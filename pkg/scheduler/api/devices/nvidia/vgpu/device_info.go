/*
Copyright 2023 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vgpu

import (
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	deviceconfig "volcano.sh/volcano/pkg/scheduler/api/devices/config"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/nodelock"
)

type GPUUsage struct {
	UsedMem  uint
	UsedCore uint
}

// GPUDevice include gpu id, memory and the pods that are sharing it.
type GPUDevice struct {
	// GPU ID
	ID int
	// Node this GPU Device belongs
	Node string
	// GPU Unique ID
	UUID string
	// The resource usage by pods that are sharing this GPU
	PodMap map[string]*GPUUsage
	// memory per card
	Memory uint
	// max sharing number
	Number uint
	// type of this number
	Type string
	// Health condition of this GPU
	Health bool
	// number of allocated
	UsedNum uint
	// number of device memory allocated
	UsedMem uint
	// number of core used
	UsedCore uint
	// MigTemplate for this GPU
	MigTemplate []deviceconfig.Geometry
	/// MigUsage for this GPU
	MigUsage deviceconfig.MigInUse
}

type GPUDevices struct {
	Name string
	// Mode GPU sharing mode
	Mode string
	// We cache score in filter step according to schedulePolicy, to avoid recalculating in score
	Score float64

	Device map[int]*GPUDevice
	// Sharing sharing handler
	Sharing SharingFactory
}

// NewGPUDevice creates a device
func NewGPUDevice(id int, mem uint) *GPUDevice {
	return &GPUDevice{
		ID:       id,
		Memory:   mem,
		PodMap:   make(map[string]*GPUUsage),
		UsedNum:  0,
		UsedMem:  0,
		UsedCore: 0,
	}
}

func NewGPUDevices(name string, node *v1.Node) *GPUDevices {
	if node == nil {
		return nil
	}
	annos, ok := node.Annotations[deviceconfig.VolcanoVGPURegister]
	if !ok {
		return nil
	}
	handshake, ok := node.Annotations[deviceconfig.VolcanoVGPUHandshake]
	if !ok {
		return nil
	}
	nodedevices, sharingMode := decodeNodeDevices(name, annos)
	if (nodedevices == nil) || len(nodedevices.Device) == 0 {
		return nil
	}
	sharingHandler, _ := GetSharingHandler(sharingMode)
	klog.V(3).Infoln("GPU sharing mode: ", sharingMode)
	for _, val := range nodedevices.Device {
		klog.V(3).InfoS("Nvidia Device registered name", "name", nodedevices.Name, "val", *val)
		ResetDeviceMetrics(val.UUID, node.Name, float64(val.Memory))
	}

	// We have to handshake here in order to avoid time-inconsistency between scheduler and nodes
	if strings.Contains(handshake, "Requesting") {
		formertime, _ := time.Parse("2006.01.02 15:04:05", strings.Split(handshake, "_")[1])
		if time.Now().After(formertime.Add(time.Second * 60)) {
			klog.V(3).Infof("node %v device %s leave", node.Name, handshake)

			tmppat := make(map[string]string)
			tmppat[deviceconfig.VolcanoVGPUHandshake] = "Deleted_" + time.Now().Format("2006.01.02 15:04:05")
			patchNodeAnnotations(node, tmppat)
			return nil
		}
	} else if strings.Contains(handshake, "Deleted") {
		return nil
	} else {
		tmppat := make(map[string]string)
		tmppat[deviceconfig.VolcanoVGPUHandshake] = "Requesting_" + time.Now().Format("2006.01.02 15:04:05")
		patchNodeAnnotations(node, tmppat)
	}
	nodedevices.Sharing = sharingHandler
	return nodedevices
}

func (gs *GPUDevices) ScoreNode(pod *v1.Pod, schedulePolicy string) float64 {
	/* TODO: we need a base score to be campatable with preemption, it means a node without evicting a task has
	   a higher score than those needs to evict a task */

	// Use cached stored in filter state in order to avoid recalculating.
	return gs.Score
}

func (gs *GPUDevices) GetIgnoredDevices() []string {
	return []string{}
}

func (gs *GPUDevices) AddQueueResource(pod *v1.Pod) map[string]float64 {
	if gs == nil {
		return map[string]float64{}
	}
	klog.V(5).InfoS("AddQueueResource", "Name", pod.Name)
	res := map[string]float64{}
	ids, ok := pod.Annotations[AssignedIDsAnnotations]
	if !ok {
		klog.Errorf("pod %s has no annotation volcano.sh/devices-to-allocate", pod.Name)
		return res
	}
	podDev := decodePodDevices(ids)
	for _, val := range podDev {
		for _, deviceused := range val {
			for _, gsdevice := range gs.Device {
				if strings.Contains(deviceused.UUID, gsdevice.UUID) {
					res[getConfig().ResourceMemoryName] += float64(deviceused.Usedmem * 1000)
					res[getConfig().ResourceCoreName] += float64(deviceused.Usedcores * 1000)
				}
			}
		}
	}
	klog.V(4).InfoS("AddQueueResource", "Name=", pod.Name, "res=", res)
	return res
}

// AddResource adds the pod to GPU pool if it is assigned
func (gs *GPUDevices) AddResource(pod *v1.Pod) {
	if gs == nil {
		return
	}

	gs.addResource(pod.Annotations, pod)
}

func (gs *GPUDevices) addResource(annotations map[string]string, pod *v1.Pod) {
	ids, ok := annotations[AssignedIDsAnnotations]
	if !ok {
		klog.Errorf("pod %s has no annotation volcano.sh/devices-to-allocate", pod.Name)
		return
	}
	podDev := decodePodDevices(ids)
	for _, val := range podDev {
		for _, deviceused := range val {
			for index, gsdevice := range gs.Device {
				if strings.Contains(deviceused.UUID, gsdevice.UUID) {
					err := gs.Sharing.AddPod(gsdevice, deviceused.Usedmem, deviceused.Usedcores, string(pod.UID), deviceused.UUID)
					if err == nil {
						gs.AddPodMetrics(index, string(pod.UID), pod.Name)
					} else {
						klog.ErrorS(err, "add resource failed")
					}
					break
				}
			}
		}
	}
}

// SubResource frees the gpu hold by the pod
func (gs *GPUDevices) SubResource(pod *v1.Pod) {
	if gs == nil {
		return
	}
	ids, ok := pod.Annotations[AssignedIDsAnnotations]
	if !ok {
		return
	}
	podDev := decodePodDevices(ids)
	for _, val := range podDev {
		for _, deviceused := range val {
			for index, gsdevice := range gs.Device {
				if strings.Contains(deviceused.UUID, gsdevice.UUID) {
					err := gs.Sharing.SubPod(gsdevice, uint(deviceused.Usedmem), uint(deviceused.Usedcores), string(pod.UID), deviceused.UUID)
					if err != nil {
						klog.ErrorS(err, "sub resource failed")
					} else {
						gs.SubPodMetrics(index, string(pod.UID), pod.Name)
					}
					break
				}
			}
		}
	}
}

func (gs *GPUDevices) HasDeviceRequest(pod *v1.Pod) bool {
	if VGPUEnable && checkVGPUResourcesInPod(pod) {
		return true
	}
	return false
}

func (gs *GPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	return nil
}

func (gs *GPUDevices) FilterNode(pod *v1.Pod, schedulePolicy string) (int, string, error) {
	if VGPUEnable {
		klog.V(4).Infoln("hami-vgpu DeviceSharing starts filtering pods", pod.Name)
		fit, _, score, err := checkNodeGPUSharingPredicateAndScore(pod, gs, true, schedulePolicy)
		if err != nil || !fit {
			klog.ErrorS(err, "Failed to fitler node to vgpu task", "pod", pod.Name)
			return devices.Unschedulable, "hami-vgpuDeviceSharing error", err
		}
		gs.Score = score
		klog.V(4).Infoln("hami-vgpu DeviceSharing successfully filters pods")
	}
	return devices.Success, "", nil
}

func (gs *GPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	if VGPUEnable {
		klog.V(4).Infoln("hami-vgpu DeviceSharing:Into AllocateToPod", pod.Name)
		fit, device, _, err := checkNodeGPUSharingPredicateAndScore(pod, gs, false, "")
		if err != nil || !fit {
			klog.ErrorS(err, "Failed to allocate vgpu task", "pod", pod.Name)
			return err
		}
		if NodeLockEnable {
			nodelock.UseClient(kubeClient)
			err = nodelock.LockNode(gs.Name, DeviceName)
			if err != nil {
				return errors.Errorf("node %s locked for %s hamivgpu lockname %s", gs.Name, pod.Name, err.Error())
			}
		}

		annotations := make(map[string]string)
		annotations[AssignedNodeAnnotations] = gs.Name
		annotations[AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)
		annotations[AssignedIDsAnnotations] = encodePodDevices(device)
		annotations[AssignedIDsToAllocateAnnotations] = annotations[AssignedIDsAnnotations]

		annotations[DeviceBindPhase] = "allocating"
		annotations[BindTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)
		// To avoid that the pod allocated info updating latency, add it first
		gs.addResource(annotations, pod)
		err = patchPodAnnotations(kubeClient, pod, annotations)
		if err != nil {
			return err
		}

		klog.V(3).Infoln("DeviceSharing:Allocate Success")
	}
	return nil
}
