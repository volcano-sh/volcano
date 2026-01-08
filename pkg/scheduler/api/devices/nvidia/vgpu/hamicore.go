/*
Copyright 2025 The Volcano Authors.

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

	"k8s.io/klog/v2"
)

type HAMICoreFactory struct{}

func init() {
	RegisterFactory(vGPUControllerHAMICore, HAMICoreFactory{})
}

func (f HAMICoreFactory) TryAddPod(gd *GPUDevice, mem uint, core uint) (bool, string) {
	gd.UsedNum++
	gd.UsedMem += mem
	gd.UsedCore += core

	return true, gd.UUID
}

func (f HAMICoreFactory) AddPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error {
	_, ok := gd.PodMap[podUID]
	if !ok {
		gd.PodMap[podUID] = &GPUUsage{
			UsedMem:  0,
			UsedCore: 0,
		}
	}
	gd.UsedNum++
	gd.UsedMem += mem
	gd.UsedCore += core

	gd.PodMap[podUID].UsedMem += mem
	gd.PodMap[podUID].UsedCore += core

	klog.V(4).Infoln("add Pod: ", podUID, mem, gd.PodMap[podUID].UsedMem, gd.PodMap[podUID].UsedCore)
	return nil
}

func (f HAMICoreFactory) SubPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error {
	_, ok := gd.PodMap[podUID]
	if !ok {
		return fmt.Errorf("pod not exist in GPU pod map")
	}

	gd.UsedNum--
	gd.UsedMem -= mem
	gd.UsedCore -= core
	gd.PodMap[podUID].UsedMem -= mem
	gd.PodMap[podUID].UsedCore -= core
	klog.V(4).Infoln("sub Pod: ", podUID, mem, gd.PodMap[podUID].UsedMem, gd.PodMap[podUID].UsedCore)
	return nil
}
