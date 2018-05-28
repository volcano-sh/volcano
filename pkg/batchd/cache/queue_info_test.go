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
	"k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
)

func queueInfoEqual(l, r *QueueInfo) bool {
	if !reflect.DeepEqual(l, r) {
		return false
	}

	return true
}

func TestQueueInfo_AddPod(t *testing.T) {

	// case1
	case01_queue := buildQueue("c1", "c1")
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))

	// case2
	case02_queue := buildQueue("c1", "c1")
	case02_owner := buildOwnerReference("owner1")
	case02_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))

	tests := []struct {
		name     string
		queue    *arbv1.Queue
		pods     []*v1.Pod
		expected *QueueInfo
	}{
		{
			name:  "add 1 pending non-owner pod, add 1 running non-owner pod",
			queue: case01_queue,
			pods:  []*v1.Pod{case01_pod1, case01_pod2},
			expected: &QueueInfo{
				Queue:     case01_queue,
				Name:      "c1",
				Namespace: "c1",
				PodSets:   make(map[types.UID]*JobInfo),
				Pods: map[string]*TaskInfo{
					"p1": NewTaskInfo(case01_pod1),
					"p2": NewTaskInfo(case01_pod2),
				},
			},
		},
		{
			name:  "add 1 pending owner pod, add 1 running owner pod",
			queue: case02_queue,
			pods:  []*v1.Pod{case02_pod1, case02_pod2},
			expected: &QueueInfo{
				Queue:     case02_queue,
				Name:      "c1",
				Namespace: "c1",
				PodSets: map[types.UID]*JobInfo{
					"owner1": {
						ObjectMeta: metav1.ObjectMeta{
							Name: "owner1",
							UID:  "owner1",
						},
						PdbName:      "",
						MinAvailable: 0,
						Allocated:    buildResource("1000m", "1G"),
						TotalRequest: buildResource("2000m", "2G"),
						Running: []*TaskInfo{
							NewTaskInfo(case02_pod2),
						},
						Assigned: []*TaskInfo{},
						Pending: []*TaskInfo{
							NewTaskInfo(case02_pod1),
						},
						Others:       []*TaskInfo{},
						NodeSelector: make(map[string]string),
					},
				},
				Pods: make(map[string]*TaskInfo),
			},
		},
	}

	for i, test := range tests {
		ci := NewQueueInfo(test.queue)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ci.AddPod(pi)
		}

		if !queueInfoEqual(ci, test.expected) {
			t.Errorf("queue info %d: \n expected %v, \n got %v \n",
				i, test.expected, ci)
		}
	}
}

func TestQueueInfo_RemovePod(t *testing.T) {

	// case1
	case01_queue := buildQueue("c1", "c1")
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01_pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))

	// case2
	case02_queue := buildQueue("c1", "c1")
	case02_owner := buildOwnerReference("owner1")
	case02_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))

	tests := []struct {
		name     string
		queue    *arbv1.Queue
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *QueueInfo
	}{
		{
			name:   "add 1 pending non-owner pod, add 2 running non-owner pod, remove 1 running non-owner pod",
			queue:  case01_queue,
			pods:   []*v1.Pod{case01_pod1, case01_pod2, case01_pod3},
			rmPods: []*v1.Pod{case01_pod2},
			expected: &QueueInfo{
				Queue:     case01_queue,
				Name:      "c1",
				Namespace: "c1",
				PodSets:   make(map[types.UID]*JobInfo),
				Pods: map[string]*TaskInfo{
					"p1": NewTaskInfo(case01_pod1),
					"p3": NewTaskInfo(case01_pod3),
				},
			},
		},
		{
			name:   "add 1 pending owner pod, add 2 running owner pod, remove 1 running owner pod",
			queue:  case02_queue,
			pods:   []*v1.Pod{case02_pod1, case02_pod2, case02_pod3},
			rmPods: []*v1.Pod{case02_pod2},
			expected: &QueueInfo{
				Queue:     case02_queue,
				Name:      "c1",
				Namespace: "c1",
				PodSets: map[types.UID]*JobInfo{
					"owner1": {
						ObjectMeta: metav1.ObjectMeta{
							Name: "owner1",
							UID:  "owner1",
						},
						PdbName:      "",
						MinAvailable: 0,
						Allocated:    buildResource("1000m", "1G"),
						TotalRequest: buildResource("2000m", "2G"),
						Running: []*TaskInfo{
							NewTaskInfo(case02_pod3),
						},
						Assigned: []*TaskInfo{},
						Pending: []*TaskInfo{
							NewTaskInfo(case02_pod1),
						},
						Others:       []*TaskInfo{},
						NodeSelector: make(map[string]string),
					},
				},
				Pods: make(map[string]*TaskInfo),
			},
		},
	}

	for i, test := range tests {
		ci := NewQueueInfo(test.queue)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ci.AddPod(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewTaskInfo(pod)
			ci.RemovePod(pi)
		}

		if !queueInfoEqual(ci, test.expected) {
			t.Errorf("queue info %d: \n expected %v, \n got %v \n",
				i, test.expected, ci)
		}
	}
}

func TestQueueInfo_AddPdb(t *testing.T) {

	// case1
	case01_queue := buildQueue("c1", "c1")
	case01_owner := buildOwnerReference("owner1")
	case01_labels := map[string]string{
		"app": "nginx",
	}
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, case01_labels)
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, case01_labels)
	case01_selector := map[string]string{
		"app": "nginx",
	}
	cass01_pdb1 := buildPdb("pdb1", 5, case01_selector)

	tests := []struct {
		name     string
		queue    *arbv1.Queue
		pods     []*v1.Pod
		pdbs     []*v1beta1.PodDisruptionBudget
		expected *QueueInfo
	}{
		{
			name:  "add 1 pdb",
			queue: case01_queue,
			pods:  []*v1.Pod{case01_pod1, case01_pod2},
			pdbs:  []*v1beta1.PodDisruptionBudget{cass01_pdb1},
			expected: &QueueInfo{
				Queue:     case01_queue,
				Name:      "c1",
				Namespace: "c1",
				PodSets: map[types.UID]*JobInfo{
					"owner1": {
						ObjectMeta: metav1.ObjectMeta{
							Name:   "owner1",
							UID:    "owner1",
							Labels: case01_labels,
						},
						PdbName:      "pdb1",
						MinAvailable: 5,
						Allocated:    buildResource("1000m", "1G"),
						TotalRequest: buildResource("2000m", "2G"),
						Running: []*TaskInfo{
							NewTaskInfo(case01_pod2),
						},
						Assigned: []*TaskInfo{},
						Pending: []*TaskInfo{
							NewTaskInfo(case01_pod1),
						},
						Others:       []*TaskInfo{},
						NodeSelector: make(map[string]string),
					},
				},
				Pods: make(map[string]*TaskInfo),
			},
		},
	}

	for i, test := range tests {
		ci := NewQueueInfo(test.queue)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ci.AddPod(pi)
		}

		for _, pdb := range test.pdbs {
			pi := NewPdbInfo(pdb)
			ci.AddPdb(pi)
		}

		if !queueInfoEqual(ci, test.expected) {
			t.Errorf("queue info %d: \n expected %v, \n got %v \n",
				i, test.expected, ci)
		}
	}
}

func TestQueueInfo_RemovePdb(t *testing.T) {

	// case1
	case01_queue := buildQueue("c1", "c1")
	case01_owner := buildOwnerReference("owner1")
	case01_labels := map[string]string{
		"app": "nginx",
	}
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, case01_labels)
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, case01_labels)
	case01_selector := map[string]string{
		"app": "nginx",
	}
	case01_pdb1 := buildPdb("pdb1", 5, case01_selector)

	tests := []struct {
		name     string
		queue    *arbv1.Queue
		pods     []*v1.Pod
		pdbs     []*v1beta1.PodDisruptionBudget
		rmPdbs   []*v1beta1.PodDisruptionBudget
		expected *QueueInfo
	}{
		{
			name:   "add 1 pdb, remove 1 pdb",
			queue:  case01_queue,
			pods:   []*v1.Pod{case01_pod1, case01_pod2},
			pdbs:   []*v1beta1.PodDisruptionBudget{case01_pdb1},
			rmPdbs: []*v1beta1.PodDisruptionBudget{case01_pdb1},
			expected: &QueueInfo{
				Queue:     case01_queue,
				Name:      "c1",
				Namespace: "c1",
				PodSets: map[types.UID]*JobInfo{
					"owner1": {
						ObjectMeta: metav1.ObjectMeta{
							Name:   "owner1",
							UID:    "owner1",
							Labels: case01_labels,
						},
						PdbName:      "",
						MinAvailable: 0,
						Allocated:    buildResource("1000m", "1G"),
						TotalRequest: buildResource("2000m", "2G"),
						Running: []*TaskInfo{
							NewTaskInfo(case01_pod2),
						},
						Assigned: []*TaskInfo{},
						Pending: []*TaskInfo{
							NewTaskInfo(case01_pod1),
						},
						Others:       []*TaskInfo{},
						NodeSelector: make(map[string]string),
					},
				},
				Pods: make(map[string]*TaskInfo),
			},
		},
	}

	for i, test := range tests {
		ci := NewQueueInfo(test.queue)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ci.AddPod(pi)
		}

		for _, pdb := range test.pdbs {
			pi := NewPdbInfo(pdb)
			ci.AddPdb(pi)
		}

		for _, pdb := range test.rmPdbs {
			pi := NewPdbInfo(pdb)
			ci.RemovePdb(pi)
		}

		if !queueInfoEqual(ci, test.expected) {
			t.Errorf("queue info %d: \n expected %v, \n got %v \n",
				i, test.expected, ci)
		}
	}
}
