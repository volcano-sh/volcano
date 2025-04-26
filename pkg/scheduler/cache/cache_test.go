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
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildNode(name string, alloc v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			UID:  types.UID(name),
			Name: name,
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

func buildPod(ns, n, nn string,
	p v1.PodPhase, req v1.ResourceList,
	owner []metav1.OwnerReference, labels map[string]string) *v1.Pod {

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", ns, n)),
			Name:            n,
			Namespace:       ns,
			OwnerReferences: owner,
			Labels:          labels,
		},
		Status: v1.PodStatus{
			Phase: p,
		},
		Spec: v1.PodSpec{
			NodeName: nn,
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: req,
					},
				},
			},
		},
	}
}

func buildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func TestGetOrCreateJob(t *testing.T) {
	owner1 := buildOwnerReference("j1")
	owner2 := buildOwnerReference("j2")

	pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner1}, make(map[string]string))
	pi1 := api.NewTaskInfo(pod1)
	pi1.Job = "j1" // The job name is set by cache.

	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pod2.Spec.SchedulerName = "volcano"
	pi2 := api.NewTaskInfo(pod2)

	pod3 := buildPod("c3", "p3", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pi3 := api.NewTaskInfo(pod3)

	cache := &SchedulerCache{
		Nodes:          make(map[string]*api.NodeInfo),
		Jobs:           make(map[api.JobID]*api.JobInfo),
		schedulerNames: []string{"volcano"},
	}

	tests := []struct {
		task   *api.TaskInfo
		gotJob bool // whether getOrCreateJob will return job for corresponding task
	}{
		{
			task:   pi1,
			gotJob: true,
		},
		{
			task:   pi2,
			gotJob: false,
		},
		{
			task:   pi3,
			gotJob: false,
		},
	}
	for i, test := range tests {
		result := cache.getOrCreateJob(test.task) != nil
		if result != test.gotJob {
			t.Errorf("case %d: \n expected %t, \n got %t \n",
				i, test.gotJob, result)
		}
	}
}

func TestSchedulerCache_Bind_NodeWithSufficientResources(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:            make(map[api.JobID]*api.JobInfo),
		Nodes:           make(map[string]*api.NodeInfo),
		Binder:          util.NewFakeBinder(0),
		BindFlowChannel: make(chan *api.TaskInfo, 5000),
	}

	pod := buildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	cache.AddPod(pod)

	node := buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	cache.AddOrUpdateNode(node)

	task := api.NewTaskInfo(pod)
	task.Job = "j1"
	if err := cache.addTask(task); err != nil {
		t.Errorf("failed to add task %v", err)
	}
	task.NodeName = "n1"
	err := cache.AddBindTask(task)
	if err != nil {
		t.Errorf("failed to bind pod to node: %v", err)
	}
}

func TestSchedulerCache_Bind_NodeWithInsufficientResources(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:            make(map[api.JobID]*api.JobInfo),
		Nodes:           make(map[string]*api.NodeInfo),
		Binder:          util.NewFakeBinder(0),
		BindFlowChannel: make(chan *api.TaskInfo, 5000),
	}

	pod := buildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("5000m", "50G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	cache.AddPod(pod)

	node := buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	cache.AddOrUpdateNode(node)

	task := api.NewTaskInfo(pod)
	task.Job = "j1"

	if err := cache.addTask(task); err != nil {
		t.Errorf("failed to add task %v", err)
	}

	task.NodeName = "n1"
	taskBeforeBind := task.Clone()
	nodeBeforeBind := cache.Nodes["n1"].Clone()

	err := cache.AddBindTask(task)
	if err == nil {
		t.Errorf("expected bind to fail for node with insufficient resources")
	}

	_, taskAfterBind, err := cache.findJobAndTask(task)
	if err != nil {
		t.Errorf("expected to find task after failed bind")
	}
	if !equality.Semantic.DeepEqual(taskBeforeBind, taskAfterBind) {
		t.Errorf("expected task to remain the same after failed bind: \n %#v\n %#v", taskBeforeBind, taskAfterBind)
	}

	nodeAfterBind := cache.Nodes["n1"].Clone()
	if !reflect.DeepEqual(nodeBeforeBind, nodeAfterBind) {
		t.Errorf("expected node to remain the same after failed bind")
	}
}

func TestNodeOperation(t *testing.T) {
	// case 1
	node1 := buildNode("n1", api.BuildResourceList("2000m", "10G"))
	node2 := buildNode("n2", api.BuildResourceList("4000m", "16G"))
	node3 := buildNode("n3", api.BuildResourceList("3000m", "12G"))
	nodeInfo1 := api.NewNodeInfo(node1)
	nodeInfo2 := api.NewNodeInfo(node2)
	nodeInfo3 := api.NewNodeInfo(node3)
	tests := []struct {
		deletedNode *v1.Node
		nodes       []*v1.Node
		expected    *SchedulerCache
		delExpect   *SchedulerCache
	}{
		{
			deletedNode: node2,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n2", "n3"},
			},
			delExpect: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n3"},
			},
		},
		{
			deletedNode: node1,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n2", "n3"},
			},
			delExpect: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n2", "n3"},
			},
		},
		{
			deletedNode: node3,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n2", "n3"},
			},
			delExpect: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
				},
				NodeList: []string{"n1", "n2"},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Nodes:    make(map[string]*api.NodeInfo),
			NodeList: []string{},
		}

		for _, n := range test.nodes {
			cache.AddOrUpdateNode(n)
		}

		if !reflect.DeepEqual(cache, test.expected) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.expected, cache)
		}

		// delete node
		cache.RemoveNode(test.deletedNode.Name)
		if !reflect.DeepEqual(cache, test.delExpect) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.delExpect, cache)
		}
	}
}

func TestBindTasks(t *testing.T) {
	owner := buildOwnerReference("j1")
	scheduler := "fake-scheduler"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := NewDefaultMockSchedulerCache(scheduler)
	sc.Run(ctx.Done())
	kubeCli := sc.kubeClient

	pod := buildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string))
	node := buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	pod.Annotations = map[string]string{"scheduling.k8s.io/group-name": "j1"}
	pod.Spec.SchedulerName = scheduler

	// make sure pod exist when calling fake client binding
	kubeCli.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	// set node in cache directly
	kubeCli.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})

	// wait for pod synced
	time.Sleep(100 * time.Millisecond)
	task := api.NewTaskInfo(pod)
	task.NodeName = "n1"
	err := sc.AddBindTask(task)
	if err != nil {
		t.Errorf("failed to bind pod to node: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	r := sc.Recorder.(*record.FakeRecorder)
	if len(r.Events) != 1 {
		t.Fatalf("successfully binding task should have 1 event")
	}
}
