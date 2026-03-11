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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestCheckGPUtype(t *testing.T) {
	tests := []struct {
		name     string
		annos    map[string]string
		cardtype string
		want     bool
	}{
		{
			name: "Single GPUInUse value matches",
			annos: map[string]string{
				GPUInUse: "NVIDIA",
			},
			cardtype: "NVIDIA",
			want:     true,
		},
		{
			name: "GPUInUse mixed case matches",
			annos: map[string]string{
				GPUInUse: "nvidia",
			},
			cardtype: "NVIDIA",
			want:     true,
		},
		{
			name: "Multiple GPUInUse values, one matches",
			annos: map[string]string{
				GPUInUse: "AMD,NVIDIA,INTEL",
			},
			cardtype: "NVIDIA",
			want:     true,
		},
		{
			name: "Multiple GPUInUse values, none matches",
			annos: map[string]string{
				GPUInUse: "AMD,INTEL,MLU",
			},
			cardtype: "NVIDIA",
			want:     false,
		},
		{
			name: "Single GPUNoUse value matches",
			annos: map[string]string{
				GPUNoUse: "NVIDIA",
			},
			cardtype: "NVIDIA",
			want:     false,
		},
		{
			name: "GPUNoUse mixed case matches",
			annos: map[string]string{
				GPUNoUse: "nvidia",
			},
			cardtype: "NVIDIA",
			want:     false,
		},
		{
			name: "Multiple GPUNoUse values, one matches",
			annos: map[string]string{
				GPUNoUse: "AMD,NVIDIA,INTEL",
			},
			cardtype: "NVIDIA",
			want:     false,
		},
		{
			name: "Multiple GPUNoUse values, none matches",
			annos: map[string]string{
				GPUNoUse: "AMD,INTEL,MLU",
			},
			cardtype: "NVIDIA",
			want:     true,
		},
		{
			name:     "Empty annotations map",
			annos:    map[string]string{},
			cardtype: "NVIDIA",
			want:     true,
		},
		{
			name:     "Nil annotations map",
			annos:    nil,
			cardtype: "NVIDIA",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkGPUtype(tt.annos, tt.cardtype); got != tt.want {
				t.Errorf("checkGPUtype() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPodGroupKey(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want string
	}{
		{
			name: "nil pod",
			pod:  nil,
			want: "",
		},
		{
			name: "nil annotations",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pod"},
			},
			want: "",
		},
		{
			name: "scheduling.k8s.io/group-name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Annotations: map[string]string{
						v1beta1.KubeGroupNameAnnotationKey: "my-pg",
					},
				},
			},
			want: "default/my-pg",
		},
		{
			name: "scheduling.volcano.sh/group-name (Volcano API)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Annotations: map[string]string{
						v1beta1.VolcanoGroupNameAnnotationKey: "job-pg",
					},
				},
			},
			want: "ns1/job-pg",
		},
		{
			name: "k8s annotation takes precedence over volcano",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns",
					Annotations: map[string]string{
						v1beta1.KubeGroupNameAnnotationKey:    "k8s-pg",
						v1beta1.VolcanoGroupNameAnnotationKey: "volcano-pg",
					},
				},
			},
			want: "ns/k8s-pg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getPodGroupKey(tt.pod); got != tt.want {
				t.Errorf("getPodGroupKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceHasPodFromSameGroup(t *testing.T) {
	tests := []struct {
		name       string
		gd         *GPUDevice
		currentKey string
		want       bool
	}{
		{
			name:       "empty currentKey",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": &GPUUsage{PodGroupKey: "ns/pg", UsedMem: 1000}}},
			currentKey: "",
			want:       false,
		},
		{
			name:       "no pod from same group",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": &GPUUsage{PodGroupKey: "ns/other", UsedMem: 1000}}},
			currentKey: "ns/my-pg",
			want:       false,
		},
		{
			name:       "same group with non-zero usage",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": &GPUUsage{PodGroupKey: "ns/my-pg", UsedMem: 1000, UsedCore: 50}}},
			currentKey: "ns/my-pg",
			want:       true,
		},
		{
			name:       "same group but zero usage (released)",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": &GPUUsage{PodGroupKey: "ns/my-pg", UsedMem: 0, UsedCore: 0}}},
			currentKey: "ns/my-pg",
			want:       false,
		},
		{
			name:       "empty PodMap",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{}},
			currentKey: "ns/pg",
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceHasPodFromSameGroup(tt.gd, tt.currentKey); got != tt.want {
				t.Errorf("deviceHasPodFromSameGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}
