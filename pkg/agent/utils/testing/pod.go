/*
Copyright 2024 The Volcano Authors.

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

package testing

import (
	"context"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"volcano.sh/volcano/pkg/agent/apis"
)

// PodProvider is used to get pods and evicted pods, just for testing.
type PodProvider struct {
	pods        []*v1.Pod
	evictedPods []*v1.Pod
}

func NewPodProvider(pods ...*v1.Pod) *PodProvider {
	pp := &PodProvider{pods: pods}
	return pp
}

func (m *PodProvider) Evict(ctx context.Context, pod *v1.Pod, eventRecorder record.EventRecorder, gracePeriodSeconds int64, evictMsg string) bool {
	m.evictedPods = append(m.evictedPods, pod)
	idx := -1
	for i := range m.pods {
		if m.pods[i].Name == pod.Name {
			idx = i
		}
	}
	if idx == -1 {
		return false
	}
	m.pods = append(m.pods[:idx], m.pods[idx+1:]...)
	return true
}

func (m *PodProvider) GetPodsFunc() ([]*v1.Pod, error) {
	return m.pods, nil
}

func (m *PodProvider) GetEvictedPods() []*v1.Pod {
	return m.evictedPods
}

func MakePod(name string, cpuRequest, memoryRequest int64, qosLevel string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{apis.PodQosLevelKey: qosLevel},
			Name:        name,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewQuantity(cpuRequest, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(memoryRequest, resource.DecimalSI),
					},
					Requests: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewQuantity(cpuRequest, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(memoryRequest, resource.DecimalSI),
					},
				}},
			}},
	}
}

func MakePodWithExtendResources(name string, cpuRequest, memoryRequest int64, qosLevel string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{apis.PodQosLevelKey: qosLevel},
			Name:        name,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						apis.ExtendResourceCPU:    *resource.NewQuantity(cpuRequest, resource.DecimalSI),
						apis.ExtendResourceMemory: *resource.NewQuantity(memoryRequest, resource.DecimalSI),
					},
					Requests: v1.ResourceList{
						apis.ExtendResourceCPU:    *resource.NewQuantity(cpuRequest, resource.DecimalSI),
						apis.ExtendResourceMemory: *resource.NewQuantity(memoryRequest, resource.DecimalSI),
					},
				}},
			}},
	}
}
