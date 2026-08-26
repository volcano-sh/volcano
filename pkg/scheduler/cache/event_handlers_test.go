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
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/cpuset"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
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
			OldPod: buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
			},
			OldTaskInfo: &api.TaskInfo{},
			NewTaskInfo: &api.TaskInfo{},
			Expected:    nil,
		},
		{
			Name:   "Error Case",
			OldPod: buildPod(namespace, "p1", "n1", v1.PodSucceeded, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			cache.AddOrUpdateNode(n)
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
			OldPod: buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
			},
			Expected: nil,
		},
		{
			Name:   "Error Case",
			OldPod: buildPod(namespace, "p1", "n1", v1.PodSucceeded, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			NewPod: buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "2G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			cache.AddOrUpdateNode(n)
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			cache.AddOrUpdateNode(n)
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			cache.AddOrUpdateNode(n)
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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
			Pod:  buildPod(namespace, "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string)),
			Nodes: []*v1.Node{
				buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)),
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

		cache.DeletedJobs = workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]())

		for _, n := range test.Nodes {
			cache.AddOrUpdateNode(n)
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

func TestSchedulerCache_SyncNode(t *testing.T) {
	n1 := util.BuildNode("n1", nil, map[string]string{"label-key": "label-value"})
	expectedNodeInfo := schedulingapi.NewNodeInfo(n1)
	expectedNodeInfo.State.Phase = schedulingapi.Ready

	tests := []struct {
		name          string
		cache         SchedulerCache
		nodes         []*v1.Node
		nodeName      string
		nodeSelector  map[string]sets.Empty
		expectedNodes map[string]*schedulingapi.NodeInfo
		wantErr       bool
	}{
		{
			name:          "Node not exists",
			nodeName:      "n1",
			expectedNodes: map[string]*schedulingapi.NodeInfo{},
			wantErr:       true,
		},
		{
			name: "Node added to cache",
			nodes: []*v1.Node{
				util.BuildNode("n1", nil, map[string]string{"label-key": "label-value"}),
				util.BuildNode("n2", nil, map[string]string{"label-key": "label-value"})},
			nodeName: "n1",
			nodeSelector: map[string]sets.Empty{
				"label-key:label-value": {},
			},
			expectedNodes: map[string]*schedulingapi.NodeInfo{"n1": expectedNodeInfo},
			wantErr:       false,
		},
		{
			name: "Node not added to cache",
			nodes: []*v1.Node{
				util.BuildNode("n1", nil, map[string]string{}),
				util.BuildNode("n2", nil, map[string]string{})},
			nodeName: "n1",
			nodeSelector: map[string]sets.Empty{
				"label-key:label-value": {},
			},
			expectedNodes: map[string]*schedulingapi.NodeInfo{},
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewDefaultMockSchedulerCache("volcano")
			for _, node := range tt.nodes {
				sc.nodeInformer.Informer().GetIndexer().Add(node)
			}
			sc.nodeSelectorLabels = tt.nodeSelector

			if err := sc.SyncNode(tt.nodeName); (err != nil) != tt.wantErr {
				t.Errorf("SyncNode() error = %v, wantErr %v", err, tt.wantErr)
			}

			actualNodes := make(map[string]*schedulingapi.NodeInfo)
			for n, i := range sc.Nodes {
				actualNodes[n] = i
			}
			assert.Equal(t, tt.expectedNodes, actualNodes)
		})
	}
}

func TestSchedulerCache_AddHyperNode(t *testing.T) {
	exactSelector := "exact"
	s5 := schedulingapi.BuildHyperNode("s5", 2, []schedulingapi.MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s4 := schedulingapi.BuildHyperNode("s4", 2, []schedulingapi.MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s3 := schedulingapi.BuildHyperNode("s3", 1, []schedulingapi.MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-7", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s1 := schedulingapi.BuildHyperNode("s1", 1, []schedulingapi.MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s2 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s0 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	initialHyperNodes0 := []*topologyv1alpha1.HyperNode{s5, s0, s4, s3, s1, s2}

	regexSelector := "regex"
	s00 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s10 := schedulingapi.BuildHyperNode("s1", 1, []schedulingapi.MemberConfig{
		{"node-[2-3]", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s20 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"^prefix", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	initialHyperNodes1 := []*topologyv1alpha1.HyperNode{s5, s00, s4, s3, s10, s20}
	s6 := schedulingapi.BuildHyperNode("s6", 3, []schedulingapi.MemberConfig{
		{"s4", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s5", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	tests := []struct {
		name                        string
		nodes                       []*v1.Node
		initialHyperNodes           []*topologyv1alpha1.HyperNode
		hypeNodesToAdd              []*topologyv1alpha1.HyperNode
		expectedHyperNodesSetByTier map[int]sets.Set[string]
		expectedRealNodesSet        map[string]sets.Set[string]
		expectedParentMap           map[string]string
		expectedHyperNodesInfo      map[string]string
		ready                       bool
	}{
		{
			name:              "Add hyperNodes with exact match",
			initialHyperNodes: initialHyperNodes0,
			hypeNodesToAdd:    []*topologyv1alpha1.HyperNode{s6},
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
				"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
		{
			name: "Add hyperNodes with regex match",
			nodes: []*v1.Node{
				util.BuildNode("node-0", nil, nil),
				util.BuildNode("node-1", nil, nil),
				util.BuildNode("node-2", nil, nil),
				util.BuildNode("node-3", nil, nil),
				util.BuildNode("prefix-0", nil, nil),
				util.BuildNode("prefix-1", nil, nil),
			},
			initialHyperNodes: initialHyperNodes1,
			hypeNodesToAdd:    []*topologyv1alpha1.HyperNode{s6},
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("prefix-0", "prefix-1"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-6", "node-7", "prefix-0", "prefix-1"),
				"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-6", "node-7", "prefix-0", "prefix-1"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
		{
			name:              "Add hyperNodes with two trees",
			initialHyperNodes: initialHyperNodes0,
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: ",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: ",
			},
			ready: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewDefaultMockSchedulerCache("volcano")
			// Add some initialNodes to match regex selector.
			for _, node := range tt.nodes {
				sc.nodeInformer.Informer().GetIndexer().Add(node)
			}
			for _, hyperNode := range tt.initialHyperNodes {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}
			for _, hyperNode := range tt.hypeNodesToAdd {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}
			assert.Equal(t, tt.expectedHyperNodesSetByTier, sc.HyperNodesInfo.HyperNodesSetByTier())
			assert.Equal(t, tt.expectedRealNodesSet, sc.HyperNodesInfo.RealNodesSet())
			actualHyperNodes := sc.HyperNodesInfo.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, tt.ready, sc.HyperNodesInfo.Ready())
		})
	}
}

func TestSchedulerCache_Delete_Then_AddBack(t *testing.T) {
	exactSelector := "exact"

	s5 := schedulingapi.BuildHyperNode("s5", 2, []schedulingapi.MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s4 := schedulingapi.BuildHyperNode("s4", 2, []schedulingapi.MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s3 := schedulingapi.BuildHyperNode("s3", 1, []schedulingapi.MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-7", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s1 := schedulingapi.BuildHyperNode("s1", 1, []schedulingapi.MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s2 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s0 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s6 := schedulingapi.BuildHyperNode("s6", 3, []schedulingapi.MemberConfig{
		{"s4", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s5", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s6, s5, s0, s4, s3, s1, s2}

	tests := []struct {
		name                        string
		nodes                       []*v1.Node
		initialHyperNodes           []*topologyv1alpha1.HyperNode
		hypeNodesToDelete           []*topologyv1alpha1.HyperNode
		expectedHyperNodesSetByTier map[int]sets.Set[string]
		expectedRealNodesSet        map[string]sets.Set[string]
		expectedParentMap           map[string]string
		expectedHyperNodesInfo      map[string]string
		ready                       bool
	}{
		{
			name:              "Delete hyperNode and then add it back",
			initialHyperNodes: initialHyperNodes,
			hypeNodesToDelete: []*topologyv1alpha1.HyperNode{s0},
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
				"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"),
			},
			expectedParentMap: map[string]string{
				"s0": "s4",
				"s1": "s4",
				"s2": "s5",
				"s3": "s5",
				"s4": "s6",
				"s5": "s6",
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewDefaultMockSchedulerCache("volcano")
			// Add some nodes to match regex selector.
			for _, node := range tt.nodes {
				assert.NoError(t, sc.nodeInformer.Informer().GetIndexer().Add(node))
			}
			for _, hyperNode := range tt.initialHyperNodes {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}
			for _, hyperNode := range tt.hypeNodesToDelete {
				assert.NoError(t, sc.deleteHyperNode(hyperNode.Name))
			}
			log.Println("begin add...")
			// add it back.
			for _, hyperNode := range tt.hypeNodesToDelete {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}
			assert.Equal(t, tt.expectedHyperNodesSetByTier, sc.HyperNodesInfo.HyperNodesSetByTier())
			assert.Equal(t, tt.expectedRealNodesSet, sc.HyperNodesInfo.RealNodesSet())
			actualHyperNodes := sc.HyperNodesInfo.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, tt.ready, sc.HyperNodesInfo.Ready())
		})
	}
}

func TestSchedulerCache_UpdateHyperNode(t *testing.T) {
	exactSelector := "exact"
	regexSelector := "regex"

	s0 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s1 := schedulingapi.BuildHyperNode("s1", 1, []schedulingapi.MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s2 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s3 := schedulingapi.BuildHyperNode("s3", 1, []schedulingapi.MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-7", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s4 := schedulingapi.BuildHyperNode("s4", 2, []schedulingapi.MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})

	s5 := schedulingapi.BuildHyperNode("s5", 2, []schedulingapi.MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s6 := schedulingapi.BuildHyperNode("s6", 3,
		[]schedulingapi.MemberConfig{
			{"s4", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
			{"s5", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s0, s1, s2, s3, s4, s5, s6}

	tests := []struct {
		name                        string
		nodes                       []*v1.Node
		initialHyperNodes           []*topologyv1alpha1.HyperNode
		hyperNodesToUpdated         []*topologyv1alpha1.HyperNode
		expectedHyperNodesSetByTier []map[int]sets.Set[string]
		expectedRealNodesSet        []map[string]sets.Set[string]
		expectedHyperNodesInfo      []map[string]string
		ready                       bool
	}{
		{
			name:              "Update non-leaf hyperNode's members",
			initialHyperNodes: initialHyperNodes,
			hyperNodesToUpdated: []*topologyv1alpha1.HyperNode{
				// first remove s2 from s5.
				schedulingapi.BuildHyperNode("s5", 2, []schedulingapi.MemberConfig{
					{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
				}),
				// second add s2 to s4.
				schedulingapi.BuildHyperNode("s4", 2, []schedulingapi.MemberConfig{
					{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
					{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
					{"s2", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
				}),
			},
			expectedHyperNodesSetByTier: []map[int]sets.Set[string]{
				{
					1: sets.New[string]("s0", "s1", "s2", "s3"),
					2: sets.New[string]("s4", "s5"),
					3: sets.New[string]("s6"),
				},
				{
					1: sets.New[string]("s0", "s1", "s2", "s3"),
					2: sets.New[string]("s4", "s5"),
					3: sets.New[string]("s6"),
				},
			},
			expectedRealNodesSet: []map[string]sets.Set[string]{
				{
					"s0": sets.New[string]("node-0", "node-1"),
					"s1": sets.New[string]("node-2", "node-3"),
					"s2": sets.New[string]("node-4", "node-5"),
					"s3": sets.New[string]("node-6", "node-7"),
					"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
					"s5": sets.New[string]("node-6", "node-7"),
					"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-6", "node-7"),
				},
				{
					"s0": sets.New[string]("node-0", "node-1"),
					"s1": sets.New[string]("node-2", "node-3"),
					"s2": sets.New[string]("node-4", "node-5"),
					"s3": sets.New[string]("node-6", "node-7"),
					"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5"),
					"s5": sets.New[string]("node-6", "node-7"),
					"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"),
				},
			},
			expectedHyperNodesInfo: []map[string]string{
				{
					"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
					"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
					"s2": "Name: s2, Tier: 1, TierName: , Parent: ",
					"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
					"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
					"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
					"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
				},
				{
					"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
					"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
					"s2": "Name: s2, Tier: 1, TierName: , Parent: s4",
					"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
					"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
					"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
					"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
				},
			},
			ready: true,
		},
		{
			name:              "Remove hyperNode s3's members node-6 and node-7.",
			initialHyperNodes: initialHyperNodes,
			hyperNodesToUpdated: []*topologyv1alpha1.HyperNode{
				schedulingapi.BuildHyperNode("s3", 1, []schedulingapi.MemberConfig{}),
			},
			expectedHyperNodesSetByTier: []map[int]sets.Set[string]{
				{
					1: sets.New[string]("s0", "s1", "s2", "s3"),
					2: sets.New[string]("s4", "s5"),
					3: sets.New[string]("s6"),
				},
			},
			expectedRealNodesSet: []map[string]sets.Set[string]{
				{
					"s0": sets.New[string]("node-0", "node-1"),
					"s1": sets.New[string]("node-2", "node-3"),
					"s2": sets.New[string]("node-4", "node-5"),

					"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
					"s5": sets.New[string]("node-4", "node-5"),
					"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5"),
				},
			},
			expectedHyperNodesInfo: []map[string]string{
				{
					"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
					"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
					"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
					"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
					"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
					"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
					"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
				},
			},
			ready: true,
		},
		{
			name: "Update hyperNode s2's members to regex match",
			nodes: []*v1.Node{
				util.BuildNode("4-suffix", nil, nil),
				util.BuildNode("5-suffix", nil, nil),
			},
			initialHyperNodes: initialHyperNodes,
			hyperNodesToUpdated: []*topologyv1alpha1.HyperNode{
				schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
					{"-suffix", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
				}),
			},
			expectedHyperNodesSetByTier: []map[int]sets.Set[string]{
				{
					1: sets.New[string]("s0", "s1", "s2", "s3"),
					2: sets.New[string]("s4", "s5"),
					3: sets.New[string]("s6"),
				},
			},
			expectedRealNodesSet: []map[string]sets.Set[string]{
				{
					"s0": sets.New[string]("node-0", "node-1"),
					"s1": sets.New[string]("node-2", "node-3"),
					"s2": sets.New[string]("4-suffix", "5-suffix"),
					"s3": sets.New[string]("node-6", "node-7"),
					"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
					"s5": sets.New[string]("4-suffix", "5-suffix", "node-6", "node-7"),
					"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "4-suffix", "5-suffix", "node-6", "node-7"),
				},
			},
			expectedHyperNodesInfo: []map[string]string{
				{
					"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
					"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
					"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
					"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
					"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
					"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
					"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
				},
			},
			ready: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewDefaultMockSchedulerCache("volcano")
			// Add some initialNodes to match regex selector.
			for _, node := range tt.nodes {
				sc.nodeInformer.Informer().GetIndexer().Add(node)
			}
			for _, hyperNode := range tt.initialHyperNodes {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}

			log.Println("begin update...")
			// compare the result by index as we have updated multi hyperNodes.
			for i, hyperNode := range tt.hyperNodesToUpdated {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
				assert.Equal(t, tt.expectedHyperNodesSetByTier[i], sc.HyperNodesInfo.HyperNodesSetByTier())
				assert.Equal(t, tt.expectedRealNodesSet[i], sc.HyperNodesInfo.RealNodesSet(), "RealNodesSet mismatch, index %d", i)
				actualHyperNodes := sc.HyperNodesInfo.HyperNodesInfo()
				assert.Equal(t, tt.expectedHyperNodesInfo[i], actualHyperNodes)
				assert.Equal(t, tt.ready, sc.HyperNodesInfo.Ready())
			}
		})
	}
}

func TestSchedulerCache_DeleteHyperNode(t *testing.T) {
	selector := "exact"
	s0 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s1 := schedulingapi.BuildHyperNode("s1", 1, []schedulingapi.MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s2 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s3 := schedulingapi.BuildHyperNode("s3", 1, []schedulingapi.MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-7", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s4 := schedulingapi.BuildHyperNode("s4", 2, []schedulingapi.MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s5 := schedulingapi.BuildHyperNode("s5", 2, []schedulingapi.MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s6 := schedulingapi.BuildHyperNode("s6", 3, []schedulingapi.MemberConfig{
		{"s4", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s5", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s0, s1, s2, s3, s4, s5, s6}

	tests := []struct {
		name                        string
		initialHyperNodes           []*topologyv1alpha1.HyperNode
		hyperNodesToDelete          *topologyv1alpha1.HyperNode
		expectedHyperNodesSetByTier map[int]sets.Set[string]
		expectedRealNodesSet        map[string]sets.Set[string]
		expectedHyperNodesInfo      map[string]string
		ready                       bool
	}{
		{
			name:               "Delete non-leaf hyperNode s4",
			initialHyperNodes:  initialHyperNodes,
			hyperNodesToDelete: s4,
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
				"s6": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: ",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: ",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
		{
			name:               "Delete leaf hyperNode s0",
			initialHyperNodes:  initialHyperNodes,
			hyperNodesToDelete: s0,
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
				"s6": sets.New[string]("node-2", "node-3", "node-4", "node-5", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
		{
			name:               "Delete root hyperNode s6",
			initialHyperNodes:  initialHyperNodes,
			hyperNodesToDelete: s6,
			expectedHyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: ",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: ",
			},
			ready: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewDefaultMockSchedulerCache("volcano")
			for _, hyperNode := range tt.initialHyperNodes {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}

			log.Println("begin delete...")
			assert.NoError(t, sc.deleteHyperNode(tt.hyperNodesToDelete.Name))
			assert.Equal(t, tt.expectedHyperNodesSetByTier, sc.HyperNodesInfo.HyperNodesSetByTier())
			assert.Equal(t, tt.expectedRealNodesSet, sc.HyperNodesInfo.RealNodesSet(), "RealNodesSet mismatch")
			actualHyperNodes := sc.HyperNodesInfo.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, tt.ready, sc.HyperNodesInfo.Ready())
		})
	}
}

func TestSchedulerCache_SyncHyperNode(t *testing.T) {
	exactSelector := "exact"
	regexSelector := "regex"
	s6 := schedulingapi.BuildHyperNode("s6", 3, []schedulingapi.MemberConfig{
		{"s4", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s5", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s5 := schedulingapi.BuildHyperNode("s5", 2, []schedulingapi.MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s4 := schedulingapi.BuildHyperNode("s4", 2, []schedulingapi.MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s3 := schedulingapi.BuildHyperNode("s3", 1, []schedulingapi.MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-7", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s2 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s20 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-9", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s21 := schedulingapi.BuildHyperNode("s2", 1, []schedulingapi.MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s1 := schedulingapi.BuildHyperNode("s1", 1, []schedulingapi.MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s0 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s00 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"^prefix", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s01 := schedulingapi.BuildHyperNode("s0", 1, []schedulingapi.MemberConfig{
		{"node-[01]", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	initialHyperNodes0 := []*topologyv1alpha1.HyperNode{s5, s0, s4, s3, s1, s2, s6}
	initialHyperNodes1 := []*topologyv1alpha1.HyperNode{s5, s00, s4, s3, s1, s20, s6}
	initialHyperNodes2 := []*topologyv1alpha1.HyperNode{s5, s01, s4, s3, s1, s21, s6}
	tests := []struct {
		name                         string
		initialNodes                 []*v1.Node
		nodeToAdd                    []*v1.Node
		nodeToDelete                 []*v1.Node
		initialHyperNodes            []*topologyv1alpha1.HyperNode
		expectedHyperNodesListByTier map[int]sets.Set[string]
		expectedRealNodesSet         map[string]sets.Set[string]
		expectedHyperNodesInfo       map[string]string
		ready                        bool
	}{
		{
			name: "leaf HyperNode member with exact selector, no need to update",
			initialNodes: []*v1.Node{
				buildNode("node-0", nil),
				buildNode("node-1", nil),
				buildNode("node-2", nil),
				buildNode("node-3", nil),
				buildNode("node-4", nil),
				buildNode("node-5", nil),
				buildNode("node-6", nil),
				buildNode("node-7", nil),
			},
			initialHyperNodes: initialHyperNodes0,
			nodeToAdd:         []*v1.Node{buildNode("node-8", nil)},
			expectedHyperNodesListByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7"),
				"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
		{
			name: "update hyperNode when new node added and matched",
			initialNodes: []*v1.Node{
				buildNode("node-0", nil),
				buildNode("node-1", nil),
				buildNode("node-2", nil),
				buildNode("node-3", nil),
				buildNode("node-4", nil),
				buildNode("node-5", nil),
				buildNode("node-6", nil),
				buildNode("node-7", nil),
			},
			initialHyperNodes: initialHyperNodes1,
			nodeToAdd: []*v1.Node{
				buildNode("prefix-node-8", nil),
				buildNode("node-9", nil),
			},
			expectedHyperNodesListByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1", "prefix-node-8"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4", "node-5", "node-9"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-0", "node-1", "node-2", "node-3", "prefix-node-8"),
				"s5": sets.New[string]("node-4", "node-5", "node-6", "node-7", "node-9"),
				"s6": sets.New[string]("node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7", "prefix-node-8", "node-9"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
		{
			name: "update hyperNode when new node deleted",
			initialNodes: []*v1.Node{
				buildNode("node-0", nil),
				buildNode("node-1", nil),
				buildNode("node-2", nil),
				buildNode("node-3", nil),
				buildNode("node-4", nil),
				buildNode("node-5", nil),
				buildNode("node-6", nil),
				buildNode("node-7", nil),
			},
			initialHyperNodes: initialHyperNodes2,
			nodeToDelete: []*v1.Node{
				buildNode("node-0", nil),
				buildNode("node-5", nil),
			},
			expectedHyperNodesListByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1", "s2", "s3"),
				2: sets.New[string]("s4", "s5"),
				3: sets.New[string]("s6"),
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-4"),
				"s3": sets.New[string]("node-6", "node-7"),
				"s4": sets.New[string]("node-1", "node-2", "node-3"),
				"s5": sets.New[string]("node-4", "node-6", "node-7"),
				"s6": sets.New[string]("node-1", "node-2", "node-3", "node-4", "node-6", "node-7"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, TierName: , Parent: s4",
				"s1": "Name: s1, Tier: 1, TierName: , Parent: s4",
				"s2": "Name: s2, Tier: 1, TierName: , Parent: s5",
				"s3": "Name: s3, Tier: 1, TierName: , Parent: s5",
				"s4": "Name: s4, Tier: 2, TierName: , Parent: s6",
				"s5": "Name: s5, Tier: 2, TierName: , Parent: s6",
				"s6": "Name: s6, Tier: 3, TierName: , Parent: ",
			},
			ready: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewDefaultMockSchedulerCache("volcano")
			// Add some initialNodes to match regex selector.
			for _, node := range tt.initialNodes {
				assert.NoError(t, sc.nodeInformer.Informer().GetIndexer().Add(node))
			}
			for _, hyperNode := range tt.initialHyperNodes {
				assert.NoError(t, sc.updateHyperNode(hyperNode))
			}

			for _, node := range tt.nodeToAdd {
				assert.NoError(t, sc.nodeInformer.Informer().GetIndexer().Add(node))
				name := "node/" + node.Name
				assert.NoError(t, sc.SyncHyperNode(name))
			}
			for _, node := range tt.nodeToDelete {
				assert.NoError(t, sc.nodeInformer.Informer().GetIndexer().Delete(node))
				name := "node/" + node.Name
				assert.NoError(t, sc.SyncHyperNode(name))
			}

			assert.Equal(t, tt.expectedHyperNodesListByTier, sc.HyperNodesInfo.HyperNodesSetByTier())
			assert.Equal(t, tt.expectedRealNodesSet, sc.HyperNodesInfo.RealNodesSet())
			actualHyperNodes := sc.HyperNodesInfo.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, tt.ready, sc.HyperNodesInfo.Ready())
		})
	}
}

// --- helpers shared with cache_test.go numa tests ---

func newCacheWithNodes(names ...string) *SchedulerCache {
	sc := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
		Jobs:  make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
	}
	for _, n := range names {
		sc.Nodes[n] = schedulingapi.NewNodeInfo(buildNode(n, nil))
	}
	return sc
}

func resSets(cpuSet string) schedulingapi.ResNumaSets {
	return schedulingapi.ResNumaSets{"cpu": mustParseCPUSet(cpuSet)}
}

func mustParseCPUSet(s string) cpuset.CPUSet {
	c, err := cpuset.Parse(s)
	if err != nil {
		panic(err)
	}
	return c
}

// podMetaFrom returns the PodMeta matching buildPod's UID convention so we look
// up the same key that clearUnassignedNumaTask derives from the pod.
func podMetaFrom(pod *v1.Pod) schedulingapi.PodMeta {
	return schedulingapi.PodMeta{UID: pod.UID, Name: pod.Name, Namespace: pod.Namespace}
}

func TestClearUnassignedNumaTask_RemovesEntry(t *testing.T) {
	sc := newCacheWithNodes("n1")
	node := sc.Nodes["n1"]

	pod := buildPod("ns1", "p1", "n1", v1.PodRunning, nil, nil, nil)
	node.UnassignedNumaPods = map[schedulingapi.PodMeta]schedulingapi.ResNumaSets{
		podMetaFrom(pod): resSets("0-1"),
	}
	task := schedulingapi.NewTaskInfo(pod)

	sc.clearUnassignedNumaTask(task)

	if len(node.UnassignedNumaPods) != 0 {
		t.Errorf("expected entry to be cleared, got %v", node.UnassignedNumaPods)
	}
}

func TestClearUnassignedNumaTask_NoOpWhenTaskHasNoNode(t *testing.T) {
	sc := newCacheWithNodes("n1")
	node := sc.Nodes["n1"]

	pod := buildPod("ns1", "p1", "", v1.PodRunning, nil, nil, nil)
	node.UnassignedNumaPods = map[schedulingapi.PodMeta]schedulingapi.ResNumaSets{
		podMetaFrom(pod): resSets("0-1"),
	}
	task := schedulingapi.NewTaskInfo(pod)

	sc.clearUnassignedNumaTask(task)

	if len(node.UnassignedNumaPods) != 1 {
		t.Errorf("expected entry to remain when task has no node, got %v", node.UnassignedNumaPods)
	}
}

func TestClearUnassignedNumaTask_NoOpWhenEntryMissing(t *testing.T) {
	sc := newCacheWithNodes("n1")
	node := sc.Nodes["n1"]
	otherPod := buildPod("ns1", "pother", "n1", v1.PodRunning, nil, nil, nil)
	node.UnassignedNumaPods = map[schedulingapi.PodMeta]schedulingapi.ResNumaSets{
		podMetaFrom(otherPod): resSets("0-1"),
	}

	pod := buildPod("ns1", "p1", "n1", v1.PodRunning, nil, nil, nil)
	task := schedulingapi.NewTaskInfo(pod)

	sc.clearUnassignedNumaTask(task)

	if len(node.UnassignedNumaPods) != 1 {
		t.Errorf("expected unrelated entry to remain, got %v", node.UnassignedNumaPods)
	}
}

func TestClearUnassignedNumaTask_NoOpWhenNodeMissing(t *testing.T) {
	sc := newCacheWithNodes()
	pod := buildPod("ns1", "p1", "ghost", v1.PodRunning, nil, nil, nil)
	task := schedulingapi.NewTaskInfo(pod)

	// Must not panic.
	sc.clearUnassignedNumaTask(task)
}

func TestClearUnassignedNumaPod_FallsBackToPodInfoWhenTaskNotInJob(t *testing.T) {
	sc := newCacheWithNodes("n1")
	node := sc.Nodes["n1"]

	pod := buildPod("ns1", "p1", "n1", v1.PodRunning, nil, nil, nil)
	node.UnassignedNumaPods = map[schedulingapi.PodMeta]schedulingapi.ResNumaSets{
		podMetaFrom(pod): resSets("0-1"),
	}
	// No job registered in sc.Jobs, so clearUnassignedNumaPod falls back to the
	// TaskInfo built directly from the pod.
	sc.clearUnassignedNumaPod(pod)

	if len(node.UnassignedNumaPods) != 0 {
		t.Errorf("expected entry cleared via fallback path, got %v", node.UnassignedNumaPods)
	}
}

func TestClearUnassignedNumaPod_UsesTaskFromJobWhenPresent(t *testing.T) {
	sc := newCacheWithNodes("n1")
	nodeN1 := sc.Nodes["n1"]

	sc.Nodes["n2"] = schedulingapi.NewNodeInfo(buildNode("n2", nil))
	nodeN2 := sc.Nodes["n2"]

	pod := buildPod("ns1", "p1", "n1", v1.PodRunning, nil, nil, nil)
	podMeta := podMetaFrom(pod)
	nodeN1.UnassignedNumaPods = map[schedulingapi.PodMeta]schedulingapi.ResNumaSets{podMeta: resSets("0-1")}
	nodeN2.UnassignedNumaPods = map[schedulingapi.PodMeta]schedulingapi.ResNumaSets{podMeta: resSets("2-3")}

	fallbackTask := schedulingapi.NewTaskInfo(pod)
	// Register a job whose task points at n2 (different from the pod's nn=n1 so
	// the fallback TaskInfo would clear the wrong node). clearUnassignedNumaPod
	// must prefer the job's task.
	standalone := schedulingapi.NewTaskInfo(pod)
	jobTask := *standalone
	jobTask.NodeName = "n2"
	job := &schedulingapi.JobInfo{Tasks: map[schedulingapi.TaskID]*schedulingapi.TaskInfo{fallbackTask.UID: &jobTask}}
	sc.Jobs[fallbackTask.Job] = job

	sc.clearUnassignedNumaPod(pod)

	if len(nodeN1.UnassignedNumaPods) != 1 {
		t.Errorf("n1 entry should remain since job task points to n2, got %v", nodeN1.UnassignedNumaPods)
	}
	if len(nodeN2.UnassignedNumaPods) != 0 {
		t.Errorf("n2 entry should have been cleared, got %v", nodeN2.UnassignedNumaPods)
	}
}

// TestAddPodWithUnresolvedPVCCachesTaskForResync verifies that when a Pod
// ADD races ahead of its referenced PVC arriving in the scheduler PVC
// informer, addPod does not drop the pod: it builds the TaskInfo, adds it
// to the cache (so it carries the pod reference and can be scheduled once
// the PVC syncs), and enqueues it for resync. Mirrors the DRA counterpart
// TestAddPodWithUnresolvedResourceClaimTemplateCachesTaskForResync.
func TestAddPodWithUnresolvedPVCCachesTaskForResync(t *testing.T) {
	sc := newMockSchedulerCache("volcano")

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-with-pvc",
			Namespace: "default",
			UID:       types.UID("pod-with-pvc-uid"),
			Annotations: map[string]string{
				schedulingv1.KubeGroupNameAnnotationKey: "pg-with-pvc",
			},
		},
		Spec: v1.PodSpec{
			SchedulerName: "volcano",
			Volumes: []v1.Volume{
				{
					Name: "scratch",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: "not-yet-in-informer",
						},
					},
				},
			},
		},
	}

	// The PVC informer is empty, so NewTaskInfo returns a pendingPVCError.
	// addPod must still succeed (nil error) by adding the task and
	// scheduling a resync.
	err := sc.addPod(pod)
	assert.NoError(t, err, "addPod must not fail when the PVC is not yet in the informer; it should cache the task and retry")

	// The task must exist in the cache as a proper TaskInfo carrying the
	// pod reference, so it can be scheduled once the PVC informer syncs.
	job, found := sc.Jobs[schedulingapi.JobID("default/pg-with-pvc")]
	assert.True(t, found, "job must be present in the cache")
	if !assert.NotNil(t, job) {
		return
	}
	task, found := job.Tasks[schedulingapi.TaskID("pod-with-pvc-uid")]
	assert.True(t, found, "task must be added to the cache")
	if assert.NotNil(t, task) {
		assert.NotNil(t, task.Pod, "cached task must carry its pod reference")
		assert.Equal(t, "pod-with-pvc", task.Name)
		assert.Equal(t, "default", task.Namespace)
	}

	// The task must be queued for resync so it is retried once the PVC
	// informer catches up.
	assert.Equal(t, 1, sc.errTasks.Len(), "task must be enqueued for resync")
}
