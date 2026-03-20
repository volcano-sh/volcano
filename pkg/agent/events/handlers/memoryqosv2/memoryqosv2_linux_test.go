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

package memoryqosv2

import (
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"

	configv1alpha1 "volcano.sh/apis/pkg/apis/config/v1alpha1"
	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type mockCgroupManager struct {
	cgroup.CgroupManager
	memorySubsystem cgroup.MemorySubsystem
}

func (m *mockCgroupManager) Memory() cgroup.MemorySubsystem {
	return m.memorySubsystem
}

func (m *mockCgroupManager) GetPodCgroupPath(qos corev1.PodQOSClass, subsystem cgroup.CgroupSubsystem, podUID types.UID) (string, error) {
	return "/sys/fs/cgroup/kubepods.slice/pod" + string(podUID), nil
}

func (m *mockCgroupManager) BuildContainerCgroupName(containerID string) string {
	return containerID
}

type mockMemorySubsystem struct {
	cgroup.MemorySubsystem
	high *mockMemoryInterface
	low  *mockMemoryInterface
	min  *mockMemoryInterface
}

func (m *mockMemorySubsystem) High() (cgroup.MemoryInterface, bool) { return m.high, true }
func (m *mockMemorySubsystem) Low() (cgroup.MemoryInterface, bool)  { return m.low, true }
func (m *mockMemorySubsystem) Min() (cgroup.MemoryInterface, bool)  { return m.min, true }

type mockMemoryInterface struct {
	val int64
}

func (m *mockMemoryInterface) Name() string                     { return "mock" }
func (m *mockMemoryInterface) Get(path string) (int64, error)   { return m.val, nil }
func (m *mockMemoryInterface) Set(path string, val int64) error { m.val = val; return nil }

type mockPodLister struct {
	corelisters.PodLister
	pods map[string]*corev1.Pod
}

func (l *mockPodLister) Pods(ns string) corelisters.PodNamespaceLister {
	return &mockPodNamespaceLister{ns: ns, pods: l.pods}
}

type mockPodNamespaceLister struct {
	corelisters.PodNamespaceLister
	ns   string
	pods map[string]*corev1.Pod
}

func (l *mockPodNamespaceLister) Get(name string) (*corev1.Pod, error) {
	if p, ok := l.pods[name]; ok {
		return p, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("pods"), name)
}

func TestGetMemoryQuantity(t *testing.T) {
	batchMemory := apis.GetExtendResourceMemory()
	tests := []struct {
		name      string
		resources corev1.ResourceList
		qosLevel  int64
		want      resource.Quantity
	}{
		{
			name: "standard memory resource",
			resources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			qosLevel: 2, // LC
			want:     resource.MustParse("1Gi"),
		},
		{
			name: "extended memory resource with LS qos",
			resources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
				batchMemory:           resource.MustParse("2Gi"),
			},
			qosLevel: 1, // LS
			want:     resource.MustParse("2Gi"),
		},
		{
			name: "no extended resource but LS qos",
			resources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			qosLevel: 1,
			want:     resource.MustParse("1Gi"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMemoryQuantity(tt.resources, tt.qosLevel)
			if !got.Equal(tt.want) {
				t.Errorf("getMemoryQuantity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseColocationConfig(t *testing.T) {
	tests := []struct {
		name    string
		v       string
		want    *configv1alpha1.MemoryQos
		wantErr bool
	}{
		{
			name: "valid config",
			v:    `{"memoryQos": {"highRatio": 80, "lowRatio": 20, "minRatio": 10}}`,
			want: &configv1alpha1.MemoryQos{
				HighRatio: 80,
				LowRatio:  20,
				MinRatio:  10,
			},
			wantErr: false,
		},
		{
			name: "missing memoryQos",
			v:    `{}`,
			want: &configv1alpha1.MemoryQos{
				HighRatio: 100,
				LowRatio:  0,
				MinRatio:  0,
			},
			wantErr: false,
		},
		{
			name:    "invalid json",
			v:       `{invalid}`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseColocationConfig(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseColocationConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseColocationConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandle(t *testing.T) {
	podUID := types.UID("test-uid")
	podName := "test-pod"
	ns := "default"
	containerName := "test-container"
	containerID := "containerd://test-container-id"

	cfg := configv1alpha1.Configuration{
		MemoryQos: &configv1alpha1.MemoryQos{
			HighRatio: 80,
			LowRatio:  20,
			MinRatio:  10,
		},
	}
	cfgBytes, _ := json.Marshal(cfg)

	memoryLimit := resource.MustParse("1Gi")
	memoryRequest := resource.MustParse("512Mi")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			UID:       podUID,
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigKey: string(cfgBytes),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: containerName,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: memoryLimit,
						},
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: memoryRequest,
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        containerName,
					ContainerID: containerID,
				},
			},
		},
	}

	high := &mockMemoryInterface{}
	low := &mockMemoryInterface{}
	min := &mockMemoryInterface{}
	memSubsystem := &mockMemorySubsystem{high: high, low: low, min: min}
	cgroupMgr := &mockCgroupManager{memorySubsystem: memSubsystem}
	podLister := &mockPodLister{pods: map[string]*corev1.Pod{podName: pod}}
	recorder := record.NewFakeRecorder(10)

	h := &MemoryQoSV2Handle{
		cgroupMgr: cgroupMgr,
		podLister: podLister,
		recorder:  recorder,
	}

	event := framework.PodEvent{
		Pod:      pod,
		UID:      podUID,
		QoSClass: corev1.PodQOSBurstable,
		QoSLevel: 2, // LC
	}

	err := h.Handle(event)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Verify cgroup settings
	// High: 1Gi * 80% = 1073741824 * 0.8 = 858993459.2 -> 858993459
	expectedHigh := memoryLimit.Value() * 80 / 100
	if high.val != expectedHigh {
		t.Errorf("High value = %d, want %d", high.val, expectedHigh)
	}

	// Low: 512Mi * 20% = 536870912 * 0.2 = 107374182.4 -> 107374182
	expectedLow := memoryRequest.Value() * 20 / 100
	if low.val != expectedLow {
		t.Errorf("Low value = %d, want %d", low.val, expectedLow)
	}

	// Min: 512Mi * 10% = 536870912 * 0.1 = 53687091.2 -> 53687091
	expectedMin := memoryRequest.Value() * 10 / 100
	if min.val != expectedMin {
		t.Errorf("Min value = %d, want %d", min.val, expectedMin)
	}
}
