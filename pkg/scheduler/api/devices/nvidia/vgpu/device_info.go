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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
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
	// working node of this GPU
	Mode string
	// MigTemplate for this GPU
	MigTemplate []deviceconfig.Geometry
	/// MigUsage for this GPU
	MigUsage deviceconfig.MigInUse
}

type GPUDevices struct {
	Name string

	// We cache score in filter step according to schedulePolicy, to avoid recalculating in score
	Score float64

	Device map[int]*GPUDevice
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
	deviceconfig.InitDevicesConfig(config.ConfigMapName)
	nodedevices := decodeNodeDevices(name, annos)
	if (nodedevices == nil) || len(nodedevices.Device) == 0 {
		return nil
	}
	for _, val := range nodedevices.Device {
		klog.V(3).InfoS("Nvidia Device registered name", "name", nodedevices.Name, "val", *val)
		ResetDeviceMetrics(val.UUID, node.Name, float64(val.Memory))
	}

	// We have to handshake here in order to avoid time-inconsistency between scheduler and nodes
	if strings.Contains(handshake, "Requesting") {
		formertime, _ := time.Parse("2006.01.02 15:04:05", strings.Split(handshake, "_")[1])
		if time.Now().After(formertime.Add(time.Second * 60)) {
			klog.Infof("node %v device %s leave", node.Name, handshake)

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
	return nodedevices
}

func (gs *GPUDevices) ScoreNode(pod *v1.Pod, schedulePolicy string) float64 {
	/* TODO: we need a base score to be campatable with preemption, it means a node without evicting a task has
	a higher score than those needs to evict a task */

	// Use cached stored in filter state in order to avoid recalculating.
	return gs.Score
}

func (gs *GPUDevices) GetIgnoredDevices() []string {
	return []string{deviceconfig.VolcanoVGPUMemory, deviceconfig.VolcanoVGPUMemoryPercentage, deviceconfig.VolcanoVGPUCores}
}

// AddResource adds the pod to GPU pool if it is assigned
func (gs *GPUDevices) AddResource(pod *v1.Pod) {
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
				if gsdevice.UUID == deviceused.UUID {
					klog.V(4).Infoln("VGPU recording pod", pod.Name, "device", deviceused)
					gs.Device[index].UsedMem += uint(deviceused.Usedmem)
					gs.Device[index].UsedNum++
					gs.Device[index].UsedCore += uint(deviceused.Usedcores)
					_, ok := gs.Device[index].PodMap[pod.Name]
					if !ok {
						gs.Device[index].PodMap[pod.Name] = &GPUUsage{
							UsedMem:  0,
							UsedCore: 0,
						}
					}
					gs.Device[index].PodMap[pod.Name].UsedCore += uint(deviceused.Usedcores)
					gs.Device[index].PodMap[pod.Name].UsedMem += uint(deviceused.Usedmem)
					gs.AddPodMetrics(index, pod.Name)
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
				if gsdevice.UUID == deviceused.UUID {
					klog.V(4).Infoln("VGPU subsctracting pod", pod.Name, "device", deviceused)
					gs.Device[index].UsedMem -= uint(deviceused.Usedmem)
					gs.Device[index].UsedNum--
					gs.Device[index].UsedCore -= uint(deviceused.Usedcores)
					gs.Device[index].PodMap[pod.Name].UsedCore -= uint(deviceused.Usedcores)
					gs.Device[index].PodMap[pod.Name].UsedMem -= uint(deviceused.Usedmem)
					gs.SubPodMetrics(index, pod.Name)
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
	// Nothing needs to be done here
	return nil
}

func (gs *GPUDevices) FilterNode(pod *v1.Pod, schedulePolicy string) (int, string, error) {
	if VGPUEnable {
		klog.V(4).Infoln("hami-vgpu DeviceSharing starts filtering pods", pod.Name)
		fit, _, score, err := checkNodeGPUSharingPredicateAndScore(pod, gs, true, schedulePolicy)
		if err != nil || !fit {
			klog.ErrorS(err, "Failed to allocate vgpu task")
			return devices.Unschedulable, fmt.Sprintf("hami-vgpuDeviceSharing %s", err.Error()), err
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
			klog.ErrorS(err, "Failed to allocate vgpu task")
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
		err = patchPodAnnotations(kubeClient, pod, annotations)
		if err != nil {
			return err
		}
		klog.V(3).Infoln("DeviceSharing:Allocate Success")
	}
	return nil
}
