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

package vgpu4pd

import (
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// GPUDevice include gpu id, memory and the pods that are sharing it.
type GPUDevice struct {
	// GPU ID
	ID int
	// GPU Unique ID
	UUID string
	// The pods that are sharing this GPU
	PodMap map[string]*v1.Pod
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
}

type GPUDevices struct {
	Name string

	Device map[int]*GPUDevice
}

// NewGPUDevice creates a device
func NewGPUDevice(id int, mem uint) *GPUDevice {
	return &GPUDevice{
		ID:       id,
		Memory:   mem,
		PodMap:   map[string]*v1.Pod{},
		UsedNum:  0,
		UsedMem:  0,
		UsedCore: 0,
	}
}

func NewGPUDevices(name string, node *v1.Node) *GPUDevices {
	klog.Infoln("into devices")
	if node == nil {
		return nil
	}
	annos, ok := node.Annotations[VolcanoVGPURegister]
	if !ok {
		return nil
	}
	handshake := node.Annotations[VolcanoVGPUHandshake]
	if !ok {
		return nil
	}
	nodedevices := DecodeNodeDevices(name, annos)
	if len(nodedevices.Device) == 0 {
		return nil
	}
	for _, val := range nodedevices.Device {
		klog.Infoln("name=", nodedevices.Name, "val=", *val)
	}

	// We have to handshake here in order to avoid time-inconsistency between scheduler and nodes
	if strings.Contains(handshake, "Requesting") {
		formertime, _ := time.Parse("2006.01.02 15:04:05", strings.Split(handshake, "_")[1])
		if time.Now().After(formertime.Add(time.Second * 60)) {
			klog.Infof("node %v device %s leave, %v remaining devices:%v", node.Name, handshake)

			tmppat := make(map[string]string)
			tmppat[handshake] = "Deleted_" + time.Now().Format("2006.01.02 15:04:05")
			PatchNodeAnnotations(node, tmppat)
			return nil
		}
	} else if strings.Contains(handshake, "Deleted") {
		return nil
	} else {
		tmppat := make(map[string]string)
		tmppat[VolcanoVGPUHandshake] = "Requesting_" + time.Now().Format("2006.01.02 15:04:05")
		PatchNodeAnnotations(node, tmppat)
	}
	return nodedevices
}

// AddGPUResource adds the pod to GPU pool if it is assigned
func (gs *GPUDevices) AddResource(pod *v1.Pod) {
	/*
		gpuRes := getGPUMemoryOfPod(pod)
		if gpuRes > 0 {
			ids := GetGPUIndex(pod)
			for _, id := range ids {
				if dev := gs.Device[id]; dev != nil {
					dev.PodMap[string(pod.UID)] = pod
				}
			}
		}*/
}

// SubGPUResource frees the gpu hold by the pod
func (gs *GPUDevices) SubResource(pod *v1.Pod) {
	/*
		gpuRes := getGPUMemoryOfPod(pod)
		if gpuRes > 0 {
			ids := GetGPUIndex(pod)
			for _, id := range ids {
				if dev := gs.Device[id]; dev != nil {
					delete(dev.PodMap, string(pod.UID))
				}
			}
		}*/
}

func (gs *GPUDevices) HasDeviceRequest(pod *v1.Pod) bool {
	klog.Infoln("-=-=-=-=-=-=-=vgpu4pd:HasDeviceRequest=-=-=-=-=-=-=-=-=-=-=")
	if VGPUEnable && checkVGPUResourcesInPod(pod) {
		return true
	}
	return false
}

func (gs *GPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	/*ids := GetGPUIndex(pod)
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
	*/
	return nil
}

func (gs *GPUDevices) FilterNode(pod *v1.Pod) (bool, error) {

	klog.Infoln("4pdvgpuDeviceSharing:Into FitInPod", pod.Name)
	if VGPUEnable {
		fit, _, err := checkNodeGPUSharingPredicate(pod, gs)
		if err != nil {
			klog.Errorln("deviceSharing err=", err.Error())
			return fit, err
		}
	}
	klog.Infoln("4pdvgpu DeviceSharing:FitInPod successed")
	return true, nil
}

func (gs *GPUDevices) GetStatus() string {
	return ""
}

func (gs *GPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.V(4).Infoln("DeviceSharing:Into AllocateToPod", pod.Name)
	/*
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
		}*/
	return nil
}
