/*
Copyright 2017 The Kubernetes Authors.

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
	"strconv"

	v1 "k8s.io/api/core/v1"
)

type DeviceInfo struct {
	Id             int
	PodMap         map[string]*v1.Pod
	GPUTotalMemory uint
}

func (di *DeviceInfo) GetPods() []*v1.Pod {
	pods := []*v1.Pod{}
	for _, pod := range di.PodMap {
		pods = append(pods, pod)
	}
	return pods
}

func NewDeviceInfo(id int, mem uint) *DeviceInfo {
	return &DeviceInfo{
		Id:             id,
		GPUTotalMemory: mem,
		PodMap:         map[string]*v1.Pod{},
	}
}

func (di *DeviceInfo) GetUsedGPUMemory() uint {
	res := uint(0)
	for _, pod := range di.PodMap {
		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		} else {
			if len(pod.ObjectMeta.Annotations) > 0 {
				mem, found := pod.ObjectMeta.Annotations["volcano.sh/pod-gpu-memory"]
				if found {
					m, _ := strconv.Atoi(mem)
					res += uint(m)
				}
			}
		}
	}
	return res
}
