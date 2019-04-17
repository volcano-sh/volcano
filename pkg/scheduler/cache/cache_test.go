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
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

func nodesEqual(l, r map[string]*api.NodeInfo) bool {
	if len(l) != len(r) {
		return false
	}

	for k, n := range l {
		if !reflect.DeepEqual(n, r[k]) {
			return false
		}
	}

	return true
}

func jobsEqual(l, r map[api.JobID]*api.JobInfo) bool {
	if len(l) != len(r) {
		return false
	}

	for k, p := range l {
		if !reflect.DeepEqual(p, r[k]) {
			return false
		}
	}

	return true
}

func cacheEqual(l, r *SchedulerCache) bool {
	return nodesEqual(l.Nodes, r.Nodes) &&
		jobsEqual(l.Jobs, r.Jobs)
}

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

func buildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
}

func buildResource(cpu string, memory string) *api.Resource {
	return api.NewResource(v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	})
}

func buildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func TestAddPod(t *testing.T) {

	owner := buildOwnerReference("j1")

	// case 1:
	pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	pi1 := api.NewTaskInfo(pod1)
	pi1.Job = "j1" // The job name is set by cache.
	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	pi2 := api.NewTaskInfo(pod2)
	pi2.Job = "j1" // The job name is set by cache.

	j1 := api.NewJobInfo(api.JobID("j1"), pi1, pi2)
	pg := createShadowPodGroup(pod1)
	j1.SetPodGroup(pg)

	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	ni1 := api.NewNodeInfo(node1)
	ni1.AddTask(pi2)

	tests := []struct {
		pods     []*v1.Pod
		nodes    []*v1.Node
		expected *SchedulerCache
	}{
		{
			pods:  []*v1.Pod{pod1, pod2},
			nodes: []*v1.Node{node1},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": ni1,
				},
				Jobs: map[api.JobID]*api.JobInfo{
					"j1": j1,
				},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Jobs:  make(map[api.JobID]*api.JobInfo),
			Nodes: make(map[string]*api.NodeInfo),
		}

		for _, n := range test.nodes {
			cache.AddNode(n)
		}

		for _, p := range test.pods {
			cache.AddPod(p)
		}

		if !cacheEqual(cache, test.expected) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.expected, cache)
		}
	}
}

func TestAddNode(t *testing.T) {
	owner1 := buildOwnerReference("j1")
	owner2 := buildOwnerReference("j2")

	// case 1
	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner1}, make(map[string]string))
	pi1 := api.NewTaskInfo(pod1)
	pi1.Job = "j1" // The job name is set by cache.

	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pi2 := api.NewTaskInfo(pod2)
	pi2.Job = "j2" // The job name is set by cache.

	ni1 := api.NewNodeInfo(node1)
	ni1.AddTask(pi2)

	j1 := api.NewJobInfo("j1")
	pg1 := createShadowPodGroup(pod1)
	j1.SetPodGroup(pg1)

	j2 := api.NewJobInfo("j2")
	pg2 := createShadowPodGroup(pod2)
	j2.SetPodGroup(pg2)

	j1.AddTaskInfo(pi1)
	j2.AddTaskInfo(pi2)

	tests := []struct {
		pods     []*v1.Pod
		nodes    []*v1.Node
		expected *SchedulerCache
	}{
		{
			pods:  []*v1.Pod{pod1, pod2},
			nodes: []*v1.Node{node1},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": ni1,
				},
				Jobs: map[api.JobID]*api.JobInfo{
					"j1": j1,
					"j2": j2,
				},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Nodes: make(map[string]*api.NodeInfo),
			Jobs:  make(map[api.JobID]*api.JobInfo),
		}

		for _, p := range test.pods {
			cache.AddPod(p)
		}

		for _, n := range test.nodes {
			cache.AddNode(n)
		}

		if !cacheEqual(cache, test.expected) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.expected, cache)
		}
	}
}

func TestGetOrCreateJob(t *testing.T) {
	owner1 := buildOwnerReference("j1")
	owner2 := buildOwnerReference("j2")

	pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner1}, make(map[string]string))
	pi1 := api.NewTaskInfo(pod1)
	pi1.Job = "j1" // The job name is set by cache.

	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pod2.Spec.SchedulerName = "kube-batch"
	pi2 := api.NewTaskInfo(pod2)

	pod3 := buildPod("c3", "p3", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pi3 := api.NewTaskInfo(pod3)

	cache := &SchedulerCache{
		Nodes:         make(map[string]*api.NodeInfo),
		Jobs:          make(map[api.JobID]*api.JobInfo),
		schedulerName: "kube-batch",
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
			gotJob: true,
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
