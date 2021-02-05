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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func jobInfoEqual(l, r *JobInfo) bool {
	return reflect.DeepEqual(l, r)
}

func TestAddTaskInfo(t *testing.T) {
	// case1
	case01UID := JobID("uid")
	case01Ns := "c1"
	case01Owner := buildOwnerReference("uid")

	case01Pod1 := buildPod(case01Ns, "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task1 := NewTaskInfo(case01Pod1)
	case01Pod2 := buildPod(case01Ns, "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task2 := NewTaskInfo(case01Pod2)
	case01Pod3 := buildPod(case01Ns, "p3", "n1", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task3 := NewTaskInfo(case01Pod3)
	case01Pod4 := buildPod(case01Ns, "p4", "n1", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task4 := NewTaskInfo(case01Pod4)

	tests := []struct {
		name     string
		uid      JobID
		pods     []*v1.Pod
		expected *JobInfo
	}{
		{
			name: "add 1 pending owner pod, 1 running owner pod",
			uid:  case01UID,
			pods: []*v1.Pod{case01Pod1, case01Pod2, case01Pod3, case01Pod4},
			expected: &JobInfo{
				UID:          case01UID,
				Allocated:    buildResource("4000m", "4G"),
				TotalRequest: buildResource("5000m", "5G"),
				Tasks: tasksMap{
					case01Task1.UID: case01Task1,
					case01Task2.UID: case01Task2,
					case01Task3.UID: case01Task3,
					case01Task4.UID: case01Task4,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Running: {
						case01Task2.UID: case01Task2,
					},
					Pending: {
						case01Task1.UID: case01Task1,
					},
					Bound: {
						case01Task3.UID: case01Task3,
						case01Task4.UID: case01Task4,
					},
				},
				NodesFitErrors: make(map[TaskID]*FitErrors),
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
	case01UID := JobID("owner1")
	case01Ns := "c1"
	case01Owner := buildOwnerReference(string(case01UID))
	case01Pod1 := buildPod(case01Ns, "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task1 := NewTaskInfo(case01Pod1)
	case01Pod2 := buildPod(case01Ns, "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Pod3 := buildPod(case01Ns, "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task3 := NewTaskInfo(case01Pod3)

	// case2
	case02UID := JobID("owner2")
	case02Ns := "c2"
	case02Owner := buildOwnerReference(string(case02UID))
	case02Pod1 := buildPod(case02Ns, "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02Owner}, make(map[string]string))
	case02Task1 := NewTaskInfo(case02Pod1)
	case02Pod2 := buildPod(case02Ns, "p2", "n1", v1.PodPending, buildResourceList("2000m", "2G"), []metav1.OwnerReference{case02Owner}, make(map[string]string))
	case02Pod3 := buildPod(case02Ns, "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{case02Owner}, make(map[string]string))
	case02Task3 := NewTaskInfo(case02Pod3)

	tests := []struct {
		name     string
		uid      JobID
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *JobInfo
	}{
		{
			name:   "add 1 pending owner pod, 2 running owner pod, remove 1 running owner pod",
			uid:    case01UID,
			pods:   []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			rmPods: []*v1.Pod{case01Pod2},
			expected: &JobInfo{
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				UID:          case01UID,
				Tasks: tasksMap{
					case01Task1.UID: case01Task1,
					case01Task3.UID: case01Task3,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Pending: {case01Task1.UID: case01Task1},
					Running: {case01Task3.UID: case01Task3},
				},
				NodesFitErrors: make(map[TaskID]*FitErrors),
			},
		},
		{
			name:   "add 2 pending owner pod, 1 running owner pod, remove 1 pending owner pod",
			uid:    case02UID,
			pods:   []*v1.Pod{case02Pod1, case02Pod2, case02Pod3},
			rmPods: []*v1.Pod{case02Pod2},
			expected: &JobInfo{
				Allocated:    buildResource("3000m", "3G"),
				TotalRequest: buildResource("4000m", "4G"),
				UID:          case02UID,
				Tasks: tasksMap{
					case02Task1.UID: case02Task1,
					case02Task3.UID: case02Task3,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Pending: {
						case02Task1.UID: case02Task1,
					},
					Running: {
						case02Task3.UID: case02Task3,
					},
				},
				NodesFitErrors: make(map[TaskID]*FitErrors),
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
