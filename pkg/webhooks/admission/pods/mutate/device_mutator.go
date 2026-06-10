/*
Copyright 2026 The Volcano Authors.

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

package mutate

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	devconfig "volcano.sh/volcano/pkg/scheduler/api/devices/config"
	wkconfig "volcano.sh/volcano/pkg/webhooks/config"
)

const (
	ascendHAMiMutatorName = "ascend"
	jsonPatchOpAdd        = "add"
	VNPUModeAnnotation    = "huawei.com/vnpu-mode"
	VNPUModeHamiCore      = "hami-core"
)

type DeviceMutator interface {
	Name() string
	MutateAdmission(pod *v1.Pod, deviceMutatorConfig wkconfig.DeviceMutatorConfig) []patchOperation
}

var registeredDeviceMutators = []DeviceMutator{
	newAscendMutator(),
}

type AscendMutator struct{}

func newAscendMutator() DeviceMutator {
	return &AscendMutator{}
}

func (m *AscendMutator) Name() string {
	return ascendHAMiMutatorName
}

func (m *AscendMutator) MutateAdmission(pod *v1.Pod, deviceMutatorConfig wkconfig.DeviceMutatorConfig) []patchOperation {
	if pod == nil || pod.Annotations == nil {
		return nil
	}
	if vnpuMode, ok := pod.Annotations[VNPUModeAnnotation]; !ok || vnpuMode != VNPUModeHamiCore {
		return nil
	}
	klog.V(5).Infof("the hami-core annotation exists in pod %s/%s", pod.Namespace, pod.Name)
	devconfig.InitDevicesConfig(deviceMutatorConfig.KnownGeometriesCMName, deviceMutatorConfig.KnownGeometriesCMNamespace)
	config := devconfig.GetConfig()
	if config == nil {
		klog.V(5).Infof("device configuration is empty.")
		return nil
	}
	var patch []patchOperation
	for idx, container := range pod.Spec.Containers {
		if container.Lifecycle != nil && container.Lifecycle.PostStart != nil {
			continue
		}
		hasAscendResource := false
		for _, vnpuConfig := range config.VNPUs {
			if val, ok := container.Resources.Limits[v1.ResourceName(vnpuConfig.ResourceName)]; ok && !val.IsZero() {
				hasAscendResource = true
				break
			}
			if val, ok := container.Resources.Requests[v1.ResourceName(vnpuConfig.ResourceName)]; ok && !val.IsZero() {
				hasAscendResource = true
				break
			}
		}
		if !hasAscendResource {
			continue
		}
		klog.V(3).Infof("Ascend core resource detected, injecting postStart lifecycle for container %s", container.Name)
		handler := v1.LifecycleHandler{
			Exec: &v1.ExecAction{
				Command: []string{
					"bash",
					"-c",
					"export RUST_LOG=info\n/hami-vnpu-core/limiter > /tmp/limiter_manager.log 2>&1 &",
				},
			},
		}
		if container.Lifecycle == nil {
			patch = append(patch, patchOperation{
				Op:   jsonPatchOpAdd,
				Path: fmt.Sprintf("/spec/containers/%d/lifecycle", idx),
				Value: v1.Lifecycle{
					PostStart: &handler,
				},
			})
			continue
		}

		patch = append(patch, patchOperation{
			Op:    jsonPatchOpAdd,
			Path:  fmt.Sprintf("/spec/containers/%d/lifecycle/postStart", idx),
			Value: handler,
		})
	}

	return patch
}
