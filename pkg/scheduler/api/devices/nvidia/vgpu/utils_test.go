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
	"strings"
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
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": {PodGroupKey: "ns/pg", UsedMem: 1000}}},
			currentKey: "",
			want:       false,
		},
		{
			name:       "no pod from same group",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": {PodGroupKey: "ns/other", UsedMem: 1000}}},
			currentKey: "ns/my-pg",
			want:       false,
		},
		{
			name:       "same group with non-zero usage",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": {PodGroupKey: "ns/my-pg", UsedMem: 1000, UsedCore: 50}}},
			currentKey: "ns/my-pg",
			want:       true,
		},
		{
			name:       "same group but zero usage (released)",
			gd:         &GPUDevice{PodMap: map[string]*GPUUsage{"uid1": {PodGroupKey: "ns/my-pg", UsedMem: 0, UsedCore: 0}}},
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

func TestSortedDeviceIndicesByPolicy_PicksExpectedDevice(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	testCases := []struct {
		name      string
		numGPUs   int
		seed      func(gs *GPUDevices)
		policy    string
		wantDevID int
	}{
		{
			name:    "binpack picks busier device",
			numGPUs: 2,
			seed: func(gs *GPUDevices) {
				gs.Device[0].UsedMem = 4096
				gs.Device[0].UsedNum = 1
			},
			policy:    binpackPolicy,
			wantDevID: 0,
		},
		{
			name:    "spread picks idle device over shared one",
			numGPUs: 2,
			seed: func(gs *GPUDevices) {
				gs.Device[0].UsedMem = 4096
				gs.Device[0].UsedNum = 1
			},
			policy:    spreadPolicy,
			wantDevID: 1,
		},
		{
			name:      "unset policy preserves legacy descending-index order",
			numGPUs:   3,
			seed:      func(gs *GPUDevices) {},
			policy:    "",
			wantDevID: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := makeGPUDevices("node-1", tc.numGPUs, 16384, 4)
			tc.seed(gs)
			wantUUID := gs.Device[tc.wantDevID].UUID
			pod := makeVGPUPod("worker", "default", "uid", 4096, false, "")
			fit, devs, _, err := checkNodeGPUSharingPredicateAndScore(pod, gs, true, tc.policy)
			if err != nil || !fit {
				t.Fatalf("pod should fit: fit=%v err=%v", fit, err)
			}
			got := getDeviceUUID(devs)
			if !strings.Contains(got, wantUUID) {
				t.Errorf("policy=%q: expected device %s, got %s", tc.policy, wantUUID, got)
			}
		})
	}
}

func TestGPUScore(t *testing.T) {
	idle := &GPUDevice{UsedNum: 0, UsedMem: 0, Memory: 16384}
	shared := &GPUDevice{UsedNum: 1, UsedMem: 4096, Memory: 16384}
	full := &GPUDevice{UsedNum: 4, UsedMem: 16384, Memory: 16384}

	testCases := []struct {
		name   string
		policy string
		device *GPUDevice
		want   float64
	}{
		{name: "spread rewards idle GPU", policy: spreadPolicy, device: idle, want: spreadMultiplier},
		{name: "spread does not reward shared GPU", policy: spreadPolicy, device: shared, want: 0},
		{name: "spread does not reward full GPU", policy: spreadPolicy, device: full, want: 0},
		{name: "binpack rewards full GPU most", policy: binpackPolicy, device: full, want: binpackMultiplier},
		{name: "binpack does not reward idle GPU", policy: binpackPolicy, device: idle, want: 0},
		{name: "unset policy returns zero", policy: "", device: shared, want: 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GPUScore(tc.policy, tc.device); got != tc.want {
				t.Errorf("GPUScore(%q) = %f, want %f", tc.policy, got, tc.want)
			}
		})
	}
}

func TestBinpackStacksSequentialPods(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	gs := makeGPUDevices("node-1", 3, 16384, 4)
	uuids := make([]string, 0, 3)
	for i, name := range []string{"a", "b", "c"} {
		pod := makeVGPUPod("worker-"+name, "default", name, 4096, false, "")
		fit, devs, _, err := checkNodeGPUSharingPredicateAndScore(pod, gs, false, binpackPolicy)
		if err != nil || !fit {
			t.Fatalf("pod %d should fit: fit=%v err=%v", i, fit, err)
		}
		uuids = append(uuids, getDeviceUUID(devs))
	}
	if uuids[0] != uuids[1] || uuids[1] != uuids[2] {
		t.Errorf("binpack should stack 3 sequential pods on the same GPU, got: %v", uuids)
	}
}
