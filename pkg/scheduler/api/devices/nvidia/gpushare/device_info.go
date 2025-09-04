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

package gpushare

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/nodelock"
)

// GPUDevice include gpu id, memory and the pods that are sharing it.
type GPUDevice struct {
	// GPU ID
	ID int
	// The pods that are sharing this GPU
	PodMap map[string]*v1.Pod
	// memory per card
	Memory uint
}

type GPUDevices struct {
	Name string

	Device map[int]*GPUDevice
}

// NewGPUDevice creates a device
func NewGPUDevice(id int, mem uint) *GPUDevice {
	return &GPUDevice{
		ID:     id,
		Memory: mem,
		PodMap: map[string]*v1.Pod{},
	}
}

func NewGPUDevices(name string, node *v1.Node) *GPUDevices {
	if node == nil {
		return nil
	}
	memory, ok := node.Status.Capacity[VolcanoGPUResource]
	if !ok {
		return nil
	}
	totalMemory := memory.Value()

	res, ok := node.Status.Capacity[VolcanoGPUNumber]
	if !ok {
		return nil
	}
	gpuNumber := res.Value()
	if gpuNumber == 0 {
		klog.Warningf("invalid %s=%s", VolcanoGPUNumber, res.String())
		return nil
	}

	memoryPerCard := uint(totalMemory / gpuNumber)
	gpudevices := GPUDevices{}
	gpudevices.Device = make(map[int]*GPUDevice)
	gpudevices.Name = name
	for i := 0; i < int(gpuNumber); i++ {
		gpudevices.Device[i] = NewGPUDevice(i, memoryPerCard)
	}
	unhealthyGPUs := getUnhealthyGPUs(&gpudevices, node)
	for i := range unhealthyGPUs {
		klog.V(4).Infof("delete unhealthy gpu id %d from GPUDevices", unhealthyGPUs[i])
		delete(gpudevices.Device, unhealthyGPUs[i])
	}
	return &gpudevices
}

// GetIgnoredDevices return device names which wish vc-scheduler to ignore
func (gs *GPUDevices) GetIgnoredDevices() []string {
	return []string{""}
}

// AddResource adds the pod to GPU pool if it is assigned
func (gs *GPUDevices) AddResource(pod *v1.Pod) {
	gpuRes := getGPUMemoryOfPod(pod)
	gpuNumRes := getGPUNumberOfPod(pod)
	if gpuRes > 0 || gpuNumRes > 0 {
		ids := GetGPUIndex(pod)
		for _, id := range ids {
			if dev := gs.Device[id]; dev != nil {
				dev.PodMap[string(pod.UID)] = pod
			}
		}
	}
}

// SubResource frees the gpu hold by the pod
func (gs *GPUDevices) SubResource(pod *v1.Pod) {
	gpuRes := getGPUMemoryOfPod(pod)
	gpuNumRes := getGPUNumberOfPod(pod)
	if gpuRes > 0 || gpuNumRes > 0 {
		ids := GetGPUIndex(pod)
		for _, id := range ids {
			if dev := gs.Device[id]; dev != nil {
				delete(dev.PodMap, string(pod.UID))
			}
		}
	}
}

func (gs *GPUDevices) HasDeviceRequest(pod *v1.Pod) bool {
	if GpuSharingEnable && getGPUMemoryOfPod(pod) > 0 ||
		GpuNumberEnable && getGPUNumberOfPod(pod) > 0 {
		return true
	}
	return false
}

func (gs *GPUDevices) AddQueueResource(pod *v1.Pod) map[string]float64 {
	return map[string]float64{}
}

func (gs *GPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	ids := GetGPUIndex(pod)
	patch := RemoveGPUIndexPatch()
	_, err := kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return errors.Errorf("patch pod %s failed with patch %s: %v", pod.Name, patch, err)
	}

	for _, id := range ids {
		if dev, ok := gs.Device[id]; ok {
			delete(dev.PodMap, string(pod.UID))
		}
	}

	klog.V(4).Infof("predicates with gpu sharing, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, gs.Name)
	return nil
}

func (gs *GPUDevices) FilterNode(pod *v1.Pod, schedulePolicy string) (int, string, error) {
	klog.V(4).Infoln("DeviceSharing:Into FitInPod", pod.Name)
	if GpuSharingEnable {
		fit, err := checkNodeGPUSharingPredicate(pod, gs)
		if err != nil || !fit {
			klog.Errorln("deviceSharing err=", err.Error())
			return devices.Unschedulable, fmt.Sprintf("GpuShare %s", err.Error()), err
		}
	}
	if GpuNumberEnable {
		fit, err := checkNodeGPUNumberPredicate(pod, gs)
		if err != nil || !fit {
			klog.Errorln("deviceSharing err=", err.Error())
			return devices.Unschedulable, fmt.Sprintf("GpuNumber %s", err.Error()), err
		}
	}
	klog.V(4).Infoln("DeviceSharing:FitInPod successed")
	return devices.Success, "", nil
}

func (gs *GPUDevices) GetStatus() string {
	return ""
}

func (gs *GPUDevices) ScoreNode(pod *v1.Pod, schedulePolicy string) float64 {
	return 0
}

func (gs *GPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.V(4).Infoln("DeviceSharing:Into AllocateToPod", pod.Name)
	if getGPUMemoryOfPod(pod) > 0 {
		if NodeLockEnable {
			nodelock.UseClient(kubeClient)
			err := nodelock.LockNode(gs.Name, "gpu")
			if err != nil {
				return errors.Errorf("node %s locked for lockname gpushare %s", gs.Name, err.Error())
			}
		}
		ids := predicateGPUbyMemory(pod, gs)
		if len(ids) == 0 {
			return errors.Errorf("the node %s can't place the pod %s in ns %s", pod.Spec.NodeName, pod.Name, pod.Namespace)
		}
		id := ids[0]
		patch := AddGPUIndexPatch([]int{id})
		pod, err := kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			return errors.Errorf("patch pod %s failed with patch %s: %v", pod.Name, patch, err)
		}
		dev, ok := gs.Device[id]
		if !ok {
			return errors.Errorf("failed to get GPU %d from node %s", id, gs.Name)
		}
		dev.PodMap[string(pod.UID)] = pod
		klog.V(4).Infof("predicates with gpu sharing, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, gs.Name)
	}
	if getGPUNumberOfPod(pod) > 0 {
		ids := predicateGPUbyNumber(pod, gs)
		if len(ids) == 0 {
			return errors.Errorf("the node %s can't place the pod %s in ns %s", pod.Spec.NodeName, pod.Name, pod.Namespace)
		}
		patch := AddGPUIndexPatch(ids)
		pod, err := kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			return errors.Errorf("patch pod %s failed with patch %s: %v", pod.Name, patch, err)
		}
		for _, id := range ids {
			dev, ok := gs.Device[id]
			if !ok {
				return errors.Errorf("failed to get GPU %d from node %s", id, gs.Name)
			}
			dev.PodMap[string(pod.UID)] = pod
		}
		klog.V(4).Infof("predicates with gpu number, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, gs.Name)
	}
	return nil
}
