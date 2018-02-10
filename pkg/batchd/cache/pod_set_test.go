/*
Copyright 2017 The Kubernetes Authors.

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

package cache

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func podSetEqual(l, r *PodSet) bool {
	if !reflect.DeepEqual(l, r) {
		return false
	}

	return true
}

func TestPodSet_AddPodInfo(t *testing.T) {
	// case1
	case01_uid := types.UID("uid")
	case01_owner := buildOwnerReference("uid")
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_pod3 := buildPod("c1", "p3", "n1", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))

	tests := []struct {
		name     string
		uid      types.UID
		pods     []*v1.Pod
		expected *PodSet
	}{
		{
			name: "add 1 pending owner pod, 1 running owner pod",
			uid:  case01_uid,
			pods: []*v1.Pod{case01_pod1, case01_pod2, case01_pod3},
			expected: &PodSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: string(case01_uid),
					UID:  case01_uid,
				},
				PdbName:      "",
				MinAvailable: 0,
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				Running: []*PodInfo{
					NewPodInfo(case01_pod2),
				},
				Pending: []*PodInfo{
					NewPodInfo(case01_pod1),
				},
				Assigned: []*PodInfo{
					NewPodInfo(case01_pod3),
				},
				Others: []*PodInfo{},
			},
		},
	}

	for i, test := range tests {
		ps := NewPodSet(test.uid)

		for _, pod := range test.pods {
			pi := NewPodInfo(pod)
			ps.AddPodInfo(pi)
		}

		if !podSetEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected %v, \n got %v \n",
				i, test.expected, ps)
		}
	}
}

func TestPodSet_DeletePodInfo(t *testing.T) {
	// case1
	case01_uid := types.UID("owner1")
	case01_owner := buildOwnerReference(string(case01_uid))
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))

	// case2
	case02_uid := types.UID("owner2")
	case02_owner := buildOwnerReference(string(case02_uid))
	case02_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_pod2 := buildPod("c1", "p2", "n1", v1.PodPending, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))

	tests := []struct {
		name     string
		uid      types.UID
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *PodSet
	}{
		{
			name:   "add 1 pending owner pod, 2 running owner pod, remove 1 running owner pod",
			uid:    case01_uid,
			pods:   []*v1.Pod{case01_pod1, case01_pod2, case01_pod3},
			rmPods: []*v1.Pod{case01_pod2},
			expected: &PodSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: string(case01_uid),
					UID:  case01_uid,
				},
				PdbName:      "",
				MinAvailable: 0,
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				Running: []*PodInfo{
					NewPodInfo(case01_pod3),
				},
				Assigned: []*PodInfo{},
				Pending: []*PodInfo{
					NewPodInfo(case01_pod1),
				},
				Others: []*PodInfo{},
			},
		},
		{
			name:   "add 2 pending owner pod, 1 running owner pod, remove 1 pending owner pod",
			uid:    case02_uid,
			pods:   []*v1.Pod{case02_pod1, case02_pod2, case02_pod3},
			rmPods: []*v1.Pod{case02_pod2},
			expected: &PodSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: string(case02_uid),
					UID:  case02_uid,
				},
				PdbName:      "",
				MinAvailable: 0,
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				Running: []*PodInfo{
					NewPodInfo(case02_pod3),
				},
				Assigned: []*PodInfo{},
				Pending: []*PodInfo{
					NewPodInfo(case02_pod1),
				},
				Others: []*PodInfo{},
			},
		},
	}

	for i, test := range tests {
		ps := NewPodSet(test.uid)

		for _, pod := range test.pods {
			pi := NewPodInfo(pod)
			ps.AddPodInfo(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewPodInfo(pod)
			ps.DeletePodInfo(pi)
		}

		if !podSetEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected %v, \n got %v \n",
				i, test.expected, ps)
		}
	}
}
