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
    TryAddPod(gd *GPUDevice, mem uint, core uint) (bool, string)
    AddPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error
    SubPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string)
}

var SharingRegistry = make(map[string]SharingFactory)

func RegisterFactory(mode string, factory SharingFactory) {
    SharingRegistry[mode] = factory
}

func init() {
    RegisterFactory(vGPUControllerMIG, MIGFactory{})
    RegisterFactory(vGPUControllerHAMICore, HAMICoreFactory{})
}

func GetSharingHandler(sharingMode string) (SharingFactory, bool) {
    s, ok := SharingRegistry[sharingMode]
    return s, ok
}
