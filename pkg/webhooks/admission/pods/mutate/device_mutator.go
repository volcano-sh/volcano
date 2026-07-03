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

	hami "volcano.sh/volcano/pkg/scheduler/api/devices/ascend/hami"
	devconfig "volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

const (
	ascendHAMiMutatorName = "ascend"
	jsonPatchOpAdd        = "add"
)

type DeviceMutator interface {
	Name() string
	MutateAdmission(pod *v1.Pod) []patchOperation
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

func (m *AscendMutator) MutateAdmission(pod *v1.Pod) []patchOperation {
	if pod == nil || pod.Annotations == nil {
		return nil
	}
	if vnpuMode, ok := pod.Annotations[hami.VNPUModeAnnotation]; !ok || vnpuMode != hami.VNPUModeHamiCore {
		return nil
	}
	klog.V(5).Infof("the hami-core annotation exists in pod %s/%s", pod.Namespace, pod.Name)
	config := devconfig.GetConfig()
	if config == nil {
		klog.V(5).Infof("device configuration is empty.")
		return nil
	}
	var patch []patchOperation
	for idx, container := range pod.Spec.Containers {
		if container.Lifecycle != nil && container.Lifecycle.PostStart != nil {
			klog.V(5).Infof("container %s already has a postStart lifecycle handler, skipping hami-vnpu-core injection", container.Name)
			continue
		}
		hasAscendResource := false
		for _, vnpuConfig := range config.VNPUs.Configs {
			if checkResource(container.Resources, vnpuConfig.ResourceName) {
				resourceCoreName := fmt.Sprintf("%s-core", vnpuConfig.ResourceName)
				if checkResource(container.Resources, vnpuConfig.ResourceMemoryName) && checkResource(container.Resources, resourceCoreName) {
					hasAscendResource = true
				}
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
					"/bin/sh",
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

func checkResource(req v1.ResourceRequirements, resourceName string) bool {
	if val, ok := req.Limits[v1.ResourceName(resourceName)]; ok && !val.IsZero() {
		return true
	}
	if val, ok := req.Requests[v1.ResourceName(resourceName)]; ok && !val.IsZero() {
		return true
	}
	return false
}
