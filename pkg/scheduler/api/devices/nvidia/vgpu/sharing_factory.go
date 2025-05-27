/*
Copyright 2025 Volcano Authors.

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

type SharingFactory interface {
	// TryAddPod try to add pod, not add the pod to the device pod map
	TryAddPod(gd *GPUDevice, mem uint, core uint) (bool, string)
	// AddPod truely add pod, add it to the device pod map
	AddPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error
	// SubPod substract the pod and remove it from the device pod map
	SubPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error
}

var sharingRegistry = make(map[string]SharingFactory)

func RegisterFactory(mode string, factory SharingFactory) {
	sharingRegistry[mode] = factory
}

func GetSharingHandler(sharingMode string) (SharingFactory, bool) {
	s, ok := sharingRegistry[sharingMode]
	return s, ok
}
