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

package api

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/utils/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
)

func TestNumatopoInfo_DeepCopy(t *testing.T) {
	original := &NumatopoInfo{
		Namespace: "ns",
		Name:      "node1",
		Policies: map[nodeinfov1alpha1.PolicyName]string{
			"policyA": "valueA",
		},
		NumaResMap: map[string]*ResourceInfo{
			"cpu": {
				Allocatable:        cpuset.New(0, 1, 2),
				Capacity:           3,
				AllocatablePerNuma: map[int]float64{0: 1, 1: 2},
				UsedPerNuma:        map[int]float64{0: 0.5, 1: 1.5},
			},
		},
		CPUDetail: topology.CPUDetails{
			0: {NUMANodeID: 0, CoreID: 0},
			1: {NUMANodeID: 1, CoreID: 1},
		},
		ResReserved: v1.ResourceList{
			v1.ResourceCPU: resource.MustParse("100m"),
		},
	}
	cp := original.DeepCopy()

	// Basic fields
	if cp.Namespace != original.Namespace {
		t.Errorf("DeepCopy Namespace = %q; want %q", cp.Namespace, original.Namespace)
	}
	if cp.Name != original.Name {
		t.Errorf("DeepCopy Name = %q; want %q", cp.Name, original.Name)
	}

	// Maps are deep
	cp.Policies["policyA"] = "other"
	if original.Policies["policyA"] == "other" {
		t.Error("DeepCopy failed: modifying copy.Policies affected original")
	}

	// NumaResMap deep
	cp.NumaResMap["cpu"].Capacity = 99
	if original.NumaResMap["cpu"].Capacity == 99 {
		t.Error("DeepCopy failed: modifying copy.NumaResMap affected original")
	}

	// CPUDetail deep
	if len(cp.CPUDetail) != len(original.CPUDetail) {
		t.Errorf("DeepCopy CPUDetail length = %d; want %d", len(cp.CPUDetail), len(original.CPUDetail))
	}
}

func TestNumatopoInfo_Compare(t *testing.T) {
	cases := []struct {
		name     string
		oldAlloc cpuset.CPUSet
		newAlloc cpuset.CPUSet
		want     bool
	}{
		{"increased", cpuset.New(0, 1), cpuset.New(0, 1, 2), true},
		{"decreased", cpuset.New(0, 1, 2), cpuset.New(0), false},
		{"same", cpuset.New(0, 1), cpuset.New(0, 1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := &NumatopoInfo{NumaResMap: map[string]*ResourceInfo{"cpu": {Allocatable: c.oldAlloc}}}
			new := &NumatopoInfo{NumaResMap: map[string]*ResourceInfo{"cpu": {Allocatable: c.newAlloc}}}
			if got := old.Compare(new); got != c.want {
				t.Errorf("Compare = %v; want %v", got, c.want)
			}
		})
	}
}

func TestNumatopoInfo_Allocate_Release(t *testing.T) {
	info := &NumatopoInfo{NumaResMap: map[string]*ResourceInfo{"cpu": {Allocatable: cpuset.New(0, 1, 2, 3)}}}

	// Allocate {0,1} → remaining {2,3}
	info.Allocate(ResNumaSets{"cpu": cpuset.New(0, 1)})
	if !info.NumaResMap["cpu"].Allocatable.Equals(cpuset.New(2, 3)) {
		t.Errorf("Allocate gives %v; want {2,3}", info.NumaResMap["cpu"].Allocatable)
	}

	// Release {0,1} back → {0,1,2,3}
	info.Release(ResNumaSets{"cpu": cpuset.New(0, 1)})
	if !info.NumaResMap["cpu"].Allocatable.Equals(cpuset.New(0, 1, 2, 3)) {
		t.Errorf("Release gives %v; want {0,1,2,3}", info.NumaResMap["cpu"].Allocatable)
	}
}

func TestGenerateNodeResNumaSets_GenerateNumaNodes(t *testing.T) {
	nodes := map[string]*NodeInfo{
		"n1": {Name: "n1", NumaSchedulerInfo: &NumatopoInfo{
			NumaResMap: map[string]*ResourceInfo{"cpu": {Allocatable: cpuset.New(0, 1)}},
			CPUDetail:  topology.CPUDetails{0: {}, 1: {}},
		}},
		"n2": {Name: "n2"},
	}

	rn := GenerateNodeResNumaSets(nodes)
	if len(rn) != 1 || rn["n1"] == nil {
		t.Errorf("GenerateNodeResNumaSets = %v; want only n1", rn)
	}

	nn := GenerateNumaNodes(nodes)
	if len(nn["n1"]) != 1 {
		t.Errorf("GenerateNumaNodes(n1) length = %d; want 1", len(nn["n1"]))
	}
}

func TestResNumaSets_Allocate_Release_Clone(t *testing.T) {
	base := ResNumaSets{"cpu": cpuset.New(0, 1, 2)}
	task := ResNumaSets{"cpu": cpuset.New(1)}

	// Allocate
	base.Allocate(task)
	if !base["cpu"].Equals(cpuset.New(0, 2)) {
		t.Errorf("ResNumaSets.Allocate = %v; want {0,2}", base["cpu"])
	}

	// Release
	base.Release(task)
	if !base["cpu"].Equals(cpuset.New(0, 1, 2)) {
		t.Errorf("ResNumaSets.Release = %v; want {0,1,2}", base["cpu"])
	}

	// Clone
	clone := base.Clone()
	clone["cpu"] = cpuset.New(9)
	if base["cpu"].Equals(cpuset.New(9)) {
		t.Error("Clone should be independent of original")
	}
}

func TestGetPodResourceNumaInfo_Annotation(t *testing.T) {
	ann := `{"numa":{"0":{"cpu":"100m"},"1":{"cpu":"200m"}}}`
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{topologyDecisionAnnotation: ann}}}

	got := GetPodResourceNumaInfo(&TaskInfo{Pod: pod})
	want := map[int]v1.ResourceList{
		0: {v1.ResourceCPU: resource.MustParse("100m")},
		1: {v1.ResourceCPU: resource.MustParse("200m")},
	}
	if !equalResourceMaps(got, want) {
		t.Errorf("GetPodResourceNumaInfo = %v; want %v", got, want)
	}

	// malformed JSON → nil
	pod.Annotations[topologyDecisionAnnotation] = `%%%`
	if got2 := GetPodResourceNumaInfo(&TaskInfo{Pod: pod}); got2 != nil {
		t.Errorf("GetPodResourceNumaInfo(bad) = %v; want nil", got2)
	}
}

func equalResourceMaps(a, b map[int]v1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for i, ra := range a {
		rb, ok := b[i]
		if !ok || len(ra) != len(rb) {
			return false
		}
		for k, qa := range ra {
			qb := rb[k]
			if qa.Cmp(qb) != 0 {
				return false
			}
		}
	}
	return true
}
