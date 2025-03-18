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

package api

import (
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
)

type Devices interface {
	//following two functions used in node_info
	//AddResource is to add the corresponding device resource of this 'pod' into current scheduler cache
	AddResource(pod *v1.Pod)
	//SubResource is to subtract the corresponding device resource of this 'pod' from current scheduler cache
	SubResource(pod *v1.Pod)

	//following four functions used in predicate
	//HasDeviceRequest checks if the 'pod' request this device
	HasDeviceRequest(pod *v1.Pod) bool
	// FilterNode checks if the 'pod' fit in current node
	// The first return value represents the filtering result, and the value range is "0, 1, 2, 3"
	// 0: Success
	// Success means that plugin ran correctly and found pod schedulable.

	// 1: Error
	// Error is used for internal plugin errors, unexpected input, etc.

	// 2: Unschedulable
	// Unschedulable is used when a plugin finds a pod unschedulable. The scheduler might attempt to
	// preempt other pods to get this pod scheduled. Use UnschedulableAndUnresolvable to make the
	// scheduler skip preemption.
	// The accompanying status message should explain why the pod is unschedulable.

	// 3: UnschedulableAndUnresolvable
	// UnschedulableAndUnresolvable is used when a plugin finds a pod unschedulable and
	// preemption would not change anything. Plugins should return Unschedulable if it is possible
	// that the pod can get scheduled with preemption.
	// The accompanying status message should explain why the pod is unschedulable.
	FilterNode(pod *v1.Pod, policy string) (int, string, error)
	// ScoreNode will be invoked when using devicescore plugin, devices api can use it to implement multiple
	// scheduling policies.
	ScoreNode(pod *v1.Pod, policy string) float64

	// Allocate action in predicate
	Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error
	// Release action in predicate
	Release(kubeClient kubernetes.Interface, pod *v1.Pod) error

	// GetIgnoredDevices notify vc-scheduler to ignore devices in return list
	GetIgnoredDevices() []string

	// GetStatus used for debug and monitor
	GetStatus() string
}

// make sure GPUDevices implements Devices interface
var _ Devices = new(gpushare.GPUDevices)
var _ Devices = new(vgpu.GPUDevices)

var RegisteredDevices = []string{
	gpushare.DeviceName,
	vgpu.DeviceName,
}

var IgnoredDevicesList = ignoredDevicesList{}

type ignoredDevicesList struct {
	sync.RWMutex
	ignoredDevices []string
}

func (l *ignoredDevicesList) Set(deviceLists ...[]string) {
	l.Lock()
	defer l.Unlock()
	l.ignoredDevices = l.ignoredDevices[:0]
	for _, devices := range deviceLists {
		l.ignoredDevices = append(l.ignoredDevices, devices...)
	}
}

func (l *ignoredDevicesList) Range(f func(i int, device string) bool) {
	l.RLock()
	defer l.RUnlock()
	for i, device := range l.ignoredDevices {
		if !f(i, device) {
			break
		}
	}
}
