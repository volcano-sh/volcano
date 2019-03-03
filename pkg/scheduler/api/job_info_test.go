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

package api

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func jobInfoEqual(l, r *JobInfo) bool {
	if !reflect.DeepEqual(l, r) {
		return false
	}

	return true
}

func TestAddTaskInfo(t *testing.T) {
	// case1
	case01_uid := JobID("uid")
	case01_ns := "c1"
	case01_owner := buildOwnerReference("uid")

	case01_pod1 := buildPod(case01_ns, "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_task1 := NewTaskInfo(case01_pod1)
	case01_pod2 := buildPod(case01_ns, "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_task2 := NewTaskInfo(case01_pod2)
	case01_pod3 := buildPod(case01_ns, "p3", "n1", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_task3 := NewTaskInfo(case01_pod3)
	case01_pod4 := buildPod(case01_ns, "p4", "n1", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_task4 := NewTaskInfo(case01_pod4)

	tests := []struct {
		name     string
		uid      JobID
		pods     []*v1.Pod
		expected *JobInfo
	}{
		{
			name: "add 1 pending owner pod, 1 running owner pod",
			uid:  case01_uid,
			pods: []*v1.Pod{case01_pod1, case01_pod2, case01_pod3, case01_pod4},
			expected: &JobInfo{
				UID:          case01_uid,
				Allocated:    buildResource("4000m", "4G"),
				TotalRequest: buildResource("5000m", "5G"),
				Tasks: tasksMap{
					case01_task1.UID: case01_task1,
					case01_task2.UID: case01_task2,
					case01_task3.UID: case01_task3,
					case01_task4.UID: case01_task4,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Running: {
						case01_task2.UID: case01_task2,
					},
					Pending: {
						case01_task1.UID: case01_task1,
					},
					Bound: {
						case01_task3.UID: case01_task3,
						case01_task4.UID: case01_task4,
					},
				},
				NodeSelector:  make(map[string]string),
				NodesFitDelta: make(NodeResourceMap),
			},
		},
	}

	for i, test := range tests {
		ps := NewJobInfo(test.uid)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ps.AddTaskInfo(pi)
		}

		if !jobInfoEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected: %v, \n got: %v \n",
				i, test.expected, ps)
		}
	}
}

func TestDeleteTaskInfo(t *testing.T) {
	// case1
	case01_uid := JobID("owner1")
	case01_ns := "c1"
	case01_owner := buildOwnerReference(string(case01_uid))
	case01_pod1 := buildPod(case01_ns, "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_task1 := NewTaskInfo(case01_pod1)
	case01_pod2 := buildPod(case01_ns, "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_pod3 := buildPod(case01_ns, "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{case01_owner}, make(map[string]string))
	case01_task3 := NewTaskInfo(case01_pod3)

	// case2
	case02_uid := JobID("owner2")
	case02_ns := "c2"
	case02_owner := buildOwnerReference(string(case02_uid))
	case02_pod1 := buildPod(case02_ns, "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_task1 := NewTaskInfo(case02_pod1)
	case02_pod2 := buildPod(case02_ns, "p2", "n1", v1.PodPending, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_pod3 := buildPod(case02_ns, "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{case02_owner}, make(map[string]string))
	case02_task3 := NewTaskInfo(case02_pod3)

	tests := []struct {
		name     string
		uid      JobID
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *JobInfo
	}{
		{
			name:   "add 1 pending owner pod, 2 running owner pod, remove 1 running owner pod",
			uid:    case01_uid,
			pods:   []*v1.Pod{case01_pod1, case01_pod2, case01_pod3},
			rmPods: []*v1.Pod{case01_pod2},
			expected: &JobInfo{
				UID:          case01_uid,
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				Tasks: tasksMap{
					case01_task1.UID: case01_task1,
					case01_task3.UID: case01_task3,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Pending: {case01_task1.UID: case01_task1},
					Running: {case01_task3.UID: case01_task3},
				},
				NodeSelector:  make(map[string]string),
				NodesFitDelta: make(NodeResourceMap),
			},
		},
		{
			name:   "add 2 pending owner pod, 1 running owner pod, remove 1 pending owner pod",
			uid:    case02_uid,
			pods:   []*v1.Pod{case02_pod1, case02_pod2, case02_pod3},
			rmPods: []*v1.Pod{case02_pod2},
			expected: &JobInfo{
				UID:          case02_uid,
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				Tasks: tasksMap{
					case02_task1.UID: case02_task1,
					case02_task3.UID: case02_task3,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Pending: {
						case02_task1.UID: case02_task1,
					},
					Running: {
						case02_task3.UID: case02_task3,
					},
				},
				NodeSelector:  make(map[string]string),
				NodesFitDelta: make(NodeResourceMap),
			},
		},
	}

	for i, test := range tests {
		ps := NewJobInfo(test.uid)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ps.AddTaskInfo(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewTaskInfo(pod)
			ps.DeleteTaskInfo(pi)
		}

		if !jobInfoEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected: %v, \n got: %v \n",
				i, test.expected, ps)
		}
	}
}
