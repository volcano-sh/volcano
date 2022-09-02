/*
Copyright 2019 The Volcano Authors.

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
	"fmt"
	"reflect"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestSchedulerCache_updateTask(t *testing.T) {
	namespace := "test"
	owner := buildOwnerReference("j1")

	tests := []struct {
		Name        string
		OldPod      *v1.Pod
		NewPod      *v1.Pod
		Nodes       []*v1.Node
		JobInfo     *api.JobInfo
		OldTaskInfo *api.TaskInfo
		NewTaskInfo *api.TaskInfo
		Expected    error
	}{
		{
			Name:   "Success Case",
			OldPod: buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			OldTaskInfo: &api.TaskInfo{},
			NewTaskInfo: &api.TaskInfo{},
			Expected:    nil,
		},
		{
			Name:   "Error Case",
			OldPod: buildPod(namespace, "p1", "n1", v1.PodSucceeded, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			OldTaskInfo: &api.TaskInfo{},
			NewTaskInfo: &api.TaskInfo{},
			Expected:    fmt.Errorf("failed to find task <%s/%s> on host <%s>", namespace, "p1", "n1"),
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:  make(map[api.JobID]*api.JobInfo),
			Nodes: make(map[string]*api.NodeInfo),
		}

		for _, n := range test.Nodes {
			cache.AddNode(n)
		}

		cache.AddPod(test.OldPod)
		test.OldTaskInfo = api.NewTaskInfo(test.OldPod)
		test.NewTaskInfo = api.NewTaskInfo(test.NewPod)

		new := cache.updateTask(test.OldTaskInfo, test.NewTaskInfo)

		if test.Expected != nil && new != nil && !strings.Contains(new.Error(), test.Expected.Error()) {
			t.Errorf("Expected Error to be %v but got %v in case %d", test.Expected, new, i)
		}
	}
}

func TestSchedulerCache_UpdatePod(t *testing.T) {
	namespace := "test"
	owner := buildOwnerReference("j1")

	tests := []struct {
		Name     string
		OldPod   *v1.Pod
		NewPod   *v1.Pod
		Nodes    []*v1.Node
		JobInfo  *api.JobInfo
		Expected error
	}{
		{
			Name:   "Success Case",
			OldPod: buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			Expected: nil,
		},
		{
			Name:   "Error Case",
			OldPod: buildPod(namespace, "p1", "n1", v1.PodSucceeded, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			Expected: fmt.Errorf("failed to find task <%s/%s> on host <%s>", namespace, "p1", "n1"),
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:  make(map[api.JobID]*api.JobInfo),
			Nodes: make(map[string]*api.NodeInfo),
		}

		for _, n := range test.Nodes {
			cache.AddNode(n)
		}

		cache.AddPod(test.OldPod)

		new := cache.updatePod(test.OldPod, test.NewPod)

		if test.Expected != nil && new != nil && !strings.Contains(new.Error(), test.Expected.Error()) {
			t.Errorf("Expected Error to be %v but got %v in case %d", test.Expected, new, i)
		}
	}
}

func TestSchedulerCache_AddPodGroupV1beta1(t *testing.T) {
	namespace := "test"
	owner := buildOwnerReference("j1")

	tests := []struct {
		Name     string
		Pod      *v1.Pod
		Nodes    []*v1.Node
		PodGroup interface{}
		Expected *api.PodGroup
	}{
		{
			Name: "Success Case",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			PodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			Expected: &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "j1",
						Namespace: namespace,
					},
				},
			},
		},
		{
			Name: "Error Case: 1 - Wrong Type",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			PodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
		{
			Name: "Error Case: 2 - PodGroup Without Identity",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			PodGroup: &schedulingv1.PodGroup{
				Status: schedulingv1.PodGroupStatus{
					Running: int32(1),
				},
			},
			Expected: nil,
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:  make(map[api.JobID]*api.JobInfo),
			Nodes: make(map[string]*api.NodeInfo),
		}

		for _, n := range test.Nodes {
			cache.AddNode(n)
		}
		test.Pod.Annotations = map[string]string{
			"scheduling.k8s.io/group-name": "j1",
		}
		cache.AddPod(test.Pod)

		cache.AddPodGroupV1beta1(test.PodGroup)
		jobID := api.JobID("test/j1")

		job := cache.Jobs[jobID]
		pg := job.PodGroup

		if test.Expected != nil && (pg.Namespace != test.Expected.Namespace || pg.Name != test.Expected.Name) {
			t.Errorf("Expected pg to be: %v but got :%v in case %d", test.Expected, pg, i)
		}
	}
}

func TestSchedulerCache_UpdatePodGroupV1beta1(t *testing.T) {
	namespace := "test"
	owner := buildOwnerReference("j1")

	tests := []struct {
		Name        string
		Pod         *v1.Pod
		Nodes       []*v1.Node
		OldPodGroup interface{}
		NewPodGroup interface{}
		Expected    *api.PodGroup
	}{
		{
			Name: "Success Case",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			OldPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			NewPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1-updated",
					Namespace: namespace,
				},
			},
			Expected: &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "j1-updated",
						Namespace: namespace,
					},
				},
			},
		},
		{
			Name: "Error Case: 1 - Wrong Type(OldPodGroup)",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			OldPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			NewPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1-updated",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
		{
			Name: "Error Case: 2 - PodGroup Without Identity",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			NewPodGroup: &schedulingv1.PodGroup{
				Status: schedulingv1.PodGroupStatus{
					Running: int32(1),
				},
			},
			OldPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1-updated",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
		{
			Name: "Error Case: 3 - Wrong Type(NewPodGroup)",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			OldPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			NewPodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1-updated",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:  make(map[api.JobID]*api.JobInfo),
			Nodes: make(map[string]*api.NodeInfo),
		}

		for _, n := range test.Nodes {
			cache.AddNode(n)
		}
		test.Pod.Annotations = map[string]string{
			"scheduling.k8s.io/group-name": "j1",
		}
		cache.AddPod(test.Pod)

		cache.UpdatePodGroupV1beta1(test.OldPodGroup, test.NewPodGroup)
		jobID := api.JobID("test/j1")

		job := cache.Jobs[jobID]
		pg := job.PodGroup

		if test.Expected != nil && pg != nil && (pg.Namespace != test.Expected.Namespace || pg.Name != test.Expected.Name) {
			t.Errorf("Expected pg to be: %v but got :%v in case %d", test.Expected, pg, i)
		}
	}
}

func TestSchedulerCache_DeletePodGroupV1beta1(t *testing.T) {
	namespace := "test"
	owner := buildOwnerReference("j1")

	tests := []struct {
		Name     string
		Pod      *v1.Pod
		Nodes    []*v1.Node
		PodGroup interface{}
		Expected *api.PodGroup
	}{
		{
			Name: "Success Case",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			PodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
		{
			Name: "Error Case: 1 - Wrong Type",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			PodGroup: &schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "j1",
					Namespace: namespace,
				},
			},
			Expected: &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "j1",
						Namespace: namespace,
					},
				},
			},
		},
		{
			Name: "Error Case: 2 - PodGroup Without Identity",
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2000m", "10G")),
			},
			PodGroup: &schedulingv1.PodGroup{
				Status: schedulingv1.PodGroupStatus{
					Running: int32(1),
				},
			},
			Expected: &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					Status: scheduling.PodGroupStatus{
						Running: int32(1),
					},
				},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:  make(map[api.JobID]*api.JobInfo),
			Nodes: make(map[string]*api.NodeInfo),
		}

		cache.DeletedJobs = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

		for _, n := range test.Nodes {
			cache.AddNode(n)
		}
		test.Pod.Annotations = map[string]string{
			"scheduling.k8s.io/group-name": "j1",
		}
		cache.AddPod(test.Pod)

		cache.AddPodGroupV1beta1(test.PodGroup)

		cache.DeletePodGroupV1beta1(test.PodGroup)
		jobID := api.JobID("test/j1")

		job := cache.Jobs[jobID]

		if test.Expected == nil && job.PodGroup != nil {
			t.Errorf("Expected job  to be: %v but got :%v in case %d", test.Expected, job, i)
		}
	}
}

func TestSchedulerCache_AddQueueV1beta1(t *testing.T) {
	namespace := "test"

	tests := []struct {
		Name     string
		Queue    interface{}
		Expected *scheduling.Queue
	}{
		{
			Name: "Success Case",
			Queue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			Expected: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
		},
		{
			Name: "Error Case: 1 - Wrong Type",
			Queue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:   make(map[api.JobID]*api.JobInfo),
			Nodes:  make(map[string]*api.NodeInfo),
			Queues: make(map[api.QueueID]*api.QueueInfo)}

		cache.AddQueueV1beta1(test.Queue)

		queue := cache.Queues["q1"]

		if test.Expected != nil && queue != nil && queue.Queue != nil && (queue.Queue.Namespace != test.Expected.Namespace || queue.Queue.Name != test.Expected.Name) {
			t.Errorf("Expected: %v but got: %v in case %d", test.Expected, queue.Queue, i)
		}
	}
}

func TestSchedulerCache_UpdateQueueV1beta1(t *testing.T) {
	namespace := "test"

	tests := []struct {
		Name     string
		OldQueue interface{}
		NewQueue interface{}
		Expected *scheduling.Queue
	}{
		{
			Name: "Success Case",
			OldQueue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			NewQueue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1-updated",
					Namespace: namespace,
				},
			},
			Expected: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1-updated",
					Namespace: namespace,
				},
			},
		},
		{
			Name: "Error Case: 1 - Wrong Type(OldQueue)",
			OldQueue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			NewQueue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1-updated",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
		{
			Name: "Error Case: 2 - Wrong Type(NewQueue)",
			OldQueue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			NewQueue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1-updated",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:   make(map[api.JobID]*api.JobInfo),
			Nodes:  make(map[string]*api.NodeInfo),
			Queues: make(map[api.QueueID]*api.QueueInfo),
		}

		cache.UpdateQueueV1beta1(test.OldQueue, test.NewQueue)

		queue := cache.Queues["q1-updated"]

		if test.Expected != nil && queue != nil && queue.Queue != nil && (queue.Queue.Namespace != test.Expected.Namespace || queue.Queue.Name != test.Expected.Name) {
			t.Errorf("Expected: %v but got: %v in case %d", test.Expected, queue.Queue, i)
		}
	}
}

func TestSchedulerCache_DeleteQueueV1beta1(t *testing.T) {
	namespace := "test"

	tests := []struct {
		Name     string
		Queue    interface{}
		Expected *scheduling.Queue
	}{
		{
			Name: "Success Case",
			Queue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			Expected: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
		},
		{
			Name: "Error Case: 1 - Wrong Type",
			Queue: &schedulingv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "q1",
					Namespace: namespace,
				},
			},
			Expected: nil,
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:   make(map[api.JobID]*api.JobInfo),
			Nodes:  make(map[string]*api.NodeInfo),
			Queues: make(map[api.QueueID]*api.QueueInfo),
		}

		cache.AddQueueV1beta1(test.Queue)
		cache.DeleteQueueV1beta1(test.Queue)

		queue := cache.Queues["q1"]

		if test.Expected == nil && queue != nil {
			t.Errorf("Expected: %v but got: %v in case %d", test.Expected, queue, i)
		}

		if test.Expected != nil && queue != nil && queue.Queue != nil && (queue.Queue.Namespace != test.Expected.Namespace || queue.Queue.Name != test.Expected.Name) {
			t.Errorf("Expected: %v but got: %v in case %d", test.Expected, queue.Queue, i)
		}
	}
}

func TestSchedulerCache_syncNode(t *testing.T) {
	case01Node1 := buildNode("n1", buildResourceList("10", "10G"))
	case01Node2 := buildNode("n1", buildResourceList("8", "8G"))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, buildResourceList("1", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("2", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("6", "6G"), []metav1.OwnerReference{}, make(map[string]string))

	var tests = []struct {
		name     string
		node     *v1.Node
		updated  *v1.Node
		pods     []*v1.Pod
		expected *api.NodeInfo
	}{
		{
			name:    "Recover: 8 -> 10",
			node:    case01Node2,
			updated: case01Node1,
			pods:    []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			expected: &api.NodeInfo{
				Name:                     "n1",
				Node:                     case01Node1,
				Idle:                     api.NewResource(buildResourceList("1", "1G")),
				Used:                     api.NewResource(buildResourceList("9", "9G")),
				OversubscriptionResource: api.EmptyResource(),
				Releasing:                api.EmptyResource(),
				Pipelined:                api.EmptyResource(),
				Allocatable:              api.NewResource(buildResourceList("10", "10G")),
				Capability:               api.NewResource(buildResourceList("10", "10G")),
				ResourceUsage:            &api.NodeUsage{},
				State:                    api.NodeState{Phase: api.Ready}, // expected Ready
				Tasks: map[api.TaskID]*api.TaskInfo{
					"c1/p1": api.NewTaskInfo(case01Pod1),
					"c1/p2": api.NewTaskInfo(case01Pod2),
					"c1/p3": api.NewTaskInfo(case01Pod3),
				},
				GPUDevices: make(map[int]*api.GPUDevice),
			},
		},
		{
			name:    "OutOfSync: 10 -> 8",
			node:    case01Node1,
			updated: case01Node2,
			pods:    []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			expected: &api.NodeInfo{
				Name:                     "n1",
				Node:                     case01Node1,
				Idle:                     api.NewResource(buildResourceList("1", "1G")), // expected other fields are untouched
				Used:                     api.NewResource(buildResourceList("9", "9G")),
				OversubscriptionResource: api.EmptyResource(),
				Releasing:                api.EmptyResource(),
				Pipelined:                api.EmptyResource(),
				Allocatable:              api.NewResource(buildResourceList("10", "10G")),
				Capability:               api.NewResource(buildResourceList("10", "10G")),
				ResourceUsage:            &api.NodeUsage{},
				State:                    api.NodeState{Phase: api.NotReady, Reason: api.ReasonOutOfSync}, // expected OutOfSync
				Tasks: map[api.TaskID]*api.TaskInfo{
					"c1/p1": api.NewTaskInfo(case01Pod1),
					"c1/p2": api.NewTaskInfo(case01Pod2),
					"c1/p3": api.NewTaskInfo(case01Pod3),
				},
				GPUDevices: make(map[int]*api.GPUDevice),
			},
		},
	}
	for i, test := range tests {
		// build SchedulerCache
		f := informers.NewSharedInformerFactory(nil, 0)
		podInformer := f.Core().V1().Pods()
		podInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{IndexNameNodeName: IndexNodeName})
		for _, pod := range test.pods {
			podInformer.Informer().GetIndexer().Add(pod)
		}
		nodeInformer := f.Core().V1().Nodes()
		nodeInformer.Informer().GetIndexer().Add(test.updated) // actual node in informer
		sc := &SchedulerCache{
			podInformer:   podInformer,
			nodeInformer:  nodeInformer,
			Jobs:          make(map[api.JobID]*api.JobInfo),
			Nodes:         make(map[string]*api.NodeInfo),
			notReadyNodes: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		}
		sc.AddNode(test.node) // cached node
		for _, pod := range test.pods {
			sc.AddPod(pod)
		}
		ni := sc.Nodes[test.node.Name]
		sc.notReadyNodes.Add(ni.Name)
		t.Logf("case[%d]: current node info: %s", i, ni)

		// execute
		err := sc.syncNode(ni)
		if err != nil {
			t.Errorf("failed to sync node: %v", err)
		}

		// assert
		if !reflect.DeepEqual(ni, test.expected) {
			t.Errorf("case[%d]: nodeInfo: \n expected\t%v, \n got\t\t%v \n",
				i, test.expected, ni)
		} else {
			t.Logf("synchronized node: %v", ni)
		}
	}
}
