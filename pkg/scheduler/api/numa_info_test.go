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

package api

import (
	"encoding/json"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
)

func newNumatopoInfoForTest(podAllocations []nodeinfov1alpha1.PodAllocation) *NumatopoInfo {
	return &NumatopoInfo{
		Name:           "n1",
		Namespace:      "",
		Policies:       make(map[nodeinfov1alpha1.PolicyName]string),
		NumaResMap:     map[string]*ResourceInfo{"cpu": {Allocatable: cpuset.New(0, 1, 2, 3), Capacity: 4}},
		ResReserved:    make(v1.ResourceList),
		PodAllocations: podAllocations,
	}
}

func TestCheckNumaPodAssigned(t *testing.T) {
	uidPodAlloc := nodeinfov1alpha1.PodAllocation{UID: "uid-1", Name: "p1", Namespace: "ns1"}
	namePodAlloc := nodeinfov1alpha1.PodAllocation{UID: "uid-2", Name: "p2", Namespace: "ns2"}

	tests := []struct {
		name    string
		info    *NumatopoInfo
		podMeta PodMeta
		want    bool
	}{
		{
			name:    "match by UID",
			info:    newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{uidPodAlloc, namePodAlloc}),
			podMeta: PodMeta{UID: "uid-1", Name: "other", Namespace: "other"},
			want:    true,
		},
		{
			name:    "match by namespace+name only",
			info:    newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{uidPodAlloc, namePodAlloc}),
			podMeta: PodMeta{UID: "uid-unknown", Name: "p2", Namespace: "ns2"},
			want:    true,
		},
		{
			name:    "no match at all",
			info:    newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{uidPodAlloc, namePodAlloc}),
			podMeta: PodMeta{UID: "uid-x", Name: "p3", Namespace: "ns3"},
			want:    false,
		},
		{
			name:    "namespace+name partial mismatch",
			info:    newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{uidPodAlloc, namePodAlloc}),
			podMeta: PodMeta{UID: "uid-x", Name: "p2", Namespace: "ns3"},
			want:    false,
		},
		{
			name:    "empty PodAllocations lookup",
			info:    newNumatopoInfoForTest(nil),
			podMeta: PodMeta{UID: "uid-1", Name: "p1", Namespace: "ns1"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.CheckNumaPodAssigned(tt.podMeta); got != tt.want {
				t.Fatalf("CheckNumaPodAssigned(%+v) = %v, want %v", tt.podMeta, got, tt.want)
			}
		})
	}
}

func TestPodMetaMarshalText(t *testing.T) {
	original := PodMeta{UID: "uid-1", Name: "p1", Namespace: "ns1"}

	data, err := original.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText returned error: %v", err)
	}

	var decoded map[string]string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Marshaled bytes are not valid JSON object: %v", err)
	}
	if decoded["uid"] != "uid-1" || decoded["name"] != "p1" || decoded["namespace"] != "ns1" {
		t.Fatalf("Marshaled content unexpected: %v", decoded)
	}

	var restored PodMeta
	if err := restored.UnmarshalText(data); err != nil {
		t.Fatalf("UnmarshalText returned error: %v", err)
	}
	if restored != original {
		t.Fatalf("Round-trip mismatch: got %+v, want %+v", restored, original)
	}
}

func TestPodMetaMapKeySerialization(t *testing.T) {
	// Use string values (not CPUSet) so the round-trip strictly tests PodMeta as a
	// JSON map key — cpuset.CPUSet is not json-serializable on its own.
	original := map[PodMeta]string{
		{UID: "uid-1", Name: "p1", Namespace: "ns1"}: "pod-1",
		{UID: "uid-2", Name: "p2", Namespace: "ns2"}: "pod-2",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal map[PodMeta]... as JSON keys: %v", err)
	}

	var decoded map[PodMeta]string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal back: %v (raw=%s)", err, string(data))
	}

	if len(decoded) != len(original) {
		t.Fatalf("Decoded len = %d, want %d", len(decoded), len(original))
	}
	for k, v := range original {
		got, ok := decoded[k]
		if !ok {
			t.Fatalf("Key %+v lost during round-trip (raw=%s)", k, string(data))
		}
		if got != v {
			t.Fatalf("Value for key %+v mismatch: got %q, want %q", k, got, v)
		}
	}
}

func TestNumatopoInfoDeepCopyIsolation(t *testing.T) {
	info := newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{
		{UID: "uid-1", Name: "p1", Namespace: "ns1",
			ContainerAllocations: []nodeinfov1alpha1.ContainerAllocation{{Name: "c1"}}},
	})

	originalAllocatable := info.NumaResMap["cpu"].Allocatable.Clone()
	originalPodAlloc := info.PodAllocations[0]

	clone := info.DeepCopy()

	// Content equality (must NOT use reflect.DeepEqual on the whole struct because
	// NumaResMap values are *ResourceInfo pointers; DeepCopy creates fresh pointers
	// with equal content, which reflect.DeepEqual treats as not-equal).
	if !clone.NumaResMap["cpu"].Allocatable.Equals(originalAllocatable) {
		t.Errorf("DeepCopy Allocatable mismatch: got %v, want %v", clone.NumaResMap["cpu"].Allocatable, originalAllocatable)
	}
	if clone.NumaResMap["cpu"].Capacity != info.NumaResMap["cpu"].Capacity {
		t.Errorf("DeepCopy Capacity mismatch: got %d, want %d", clone.NumaResMap["cpu"].Capacity, info.NumaResMap["cpu"].Capacity)
	}
	if len(clone.PodAllocations) != 1 {
		t.Fatalf("DeepCopy PodAllocations len = %d, want 1", len(clone.PodAllocations))
	}
	if got := clone.PodAllocations[0]; got.UID != originalPodAlloc.UID ||
		got.Name != originalPodAlloc.Name || got.Namespace != originalPodAlloc.Namespace ||
		len(got.ContainerAllocations) != len(originalPodAlloc.ContainerAllocations) {
		t.Errorf("DeepCopy PodAllocations mismatch: got %+v, want %+v", got, originalPodAlloc)
	}
	if clone.Name != info.Name || clone.Namespace != info.Namespace {
		t.Errorf("DeepCopy identity fields mismatch: got %+v, want %+v", clone, info)
	}

	// Mutate clone and ensure original is untouched.
	clone.NumaResMap["cpu"].Allocatable = clone.NumaResMap["cpu"].Allocatable.Difference(cpuset.New(0))
	clone.PodAllocations[0] = nodeinfov1alpha1.PodAllocation{UID: "uid-mutated"}

	if !info.NumaResMap["cpu"].Allocatable.Equals(originalAllocatable) {
		t.Errorf("Original Allocatable mutated: got %v, want %v", info.NumaResMap["cpu"].Allocatable, originalAllocatable)
	}
	if got := info.PodAllocations[0]; got.UID != originalPodAlloc.UID ||
		got.Name != originalPodAlloc.Name || got.Namespace != originalPodAlloc.Namespace {
		t.Errorf("Original PodAllocations mutated: got %+v, want %+v", got, originalPodAlloc)
	}
}
