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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

func nodesEqual(l, r map[string]*NodeInfo) bool {
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

func podsEqual(l, r map[string]*PodInfo) bool {
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

func consumersEqual(l, r map[string]*ConsumerInfo) bool {
	if len(l) != len(r) {
		return false
	}

	for k, c := range l {
		if !reflect.DeepEqual(c, r[k]) {
			return false
		}
	}

	return true
}

func cacheEqual(l, r *SchedulerCache) bool {
	return nodesEqual(l.Nodes, r.Nodes) &&
		podsEqual(l.Pods, r.Pods) &&
		consumersEqual(l.Consumers, r.Consumers)
}

func buildNode(name string, alloc v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

func buildPod(ns, n, nn string, p v1.PodPhase, req v1.ResourceList, owner []metav1.OwnerReference) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            n,
			Namespace:       ns,
			OwnerReferences: owner,
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

func buildConsumer(name string, namespace string) *arbv1.Consumer {
	return &arbv1.Consumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func buildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
}

func buildResource(cpu string, memory string) *Resource {
	return NewResource(v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	})
}

func TestAddPod(t *testing.T) {

	// case 1:
	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	consumer1 := buildConsumer("c1", "c1")
	ni1 := NewNodeInfo(node1)
	pi1 := NewPodInfo(pod1)
	pi2 := NewPodInfo(pod2)
	ci1 := NewConsumerInfo(consumer1)
	ni1.AddPod(pi2)
	ci1.AddPod(pi1)
	ci1.AddPod(pi2)

	tests := []struct {
		pods      []*v1.Pod
		nodes     []*v1.Node
		consumers []*arbv1.Consumer
		expected  *SchedulerCache
	}{
		{
			pods:      []*v1.Pod{pod1, pod2},
			nodes:     []*v1.Node{node1},
			consumers: []*arbv1.Consumer{consumer1},
			expected: &SchedulerCache{
				Nodes: map[string]*NodeInfo{
					"n1": ni1,
				},
				Pods: map[string]*PodInfo{
					"c1/p1": pi1,
					"c1/p2": pi2,
				},
				Consumers: map[string]*ConsumerInfo{
					"c1": ci1,
				},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Nodes:     make(map[string]*NodeInfo),
			Pods:      make(map[string]*PodInfo),
			Consumers: make(map[string]*ConsumerInfo),
		}

		for _, n := range test.nodes {
			cache.AddNode(n)
		}

		for _, c := range test.consumers {
			cache.AddConsumer(c)
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

	// case 1
	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	ni1 := NewNodeInfo(node1)
	pi1 := NewPodInfo(pod1)
	pi2 := NewPodInfo(pod2)
	ni1.AddPod(pi2)

	tests := []struct {
		pods     []*v1.Pod
		nodes    []*v1.Node
		expected *SchedulerCache
	}{
		{
			pods:  []*v1.Pod{pod1, pod2},
			nodes: []*v1.Node{node1},
			expected: &SchedulerCache{
				Nodes: map[string]*NodeInfo{
					"n1": ni1,
				},
				Pods: map[string]*PodInfo{
					"c1/p1": pi1,
					"c1/p2": pi2,
				},
				Consumers: map[string]*ConsumerInfo{
					"c1": {
						Namespace: "c1",
						PodSets:   make(map[types.UID]*PodSet),
						Pods: map[string]*PodInfo{
							"p1": pi1,
							"p2": pi2,
						},
					},
				},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Nodes:     make(map[string]*NodeInfo),
			Pods:      make(map[string]*PodInfo),
			Consumers: make(map[string]*ConsumerInfo),
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

func TestAddConsumer(t *testing.T) {

	// case 1
	pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	consumer1 := buildConsumer("c1", "c1")
	pi1 := NewPodInfo(pod1)
	pi2 := NewPodInfo(pod2)
	ci1 := NewConsumerInfo(consumer1)
	ci1.AddPod(pi1)
	ci1.AddPod(pi2)

	tests := []struct {
		pods      []*v1.Pod
		consumers []*arbv1.Consumer
		expected  *SchedulerCache
	}{
		{
			pods:      []*v1.Pod{pod1, pod2},
			consumers: []*arbv1.Consumer{consumer1},
			expected: &SchedulerCache{
				Nodes: map[string]*NodeInfo{
					"n1": {
						Idle: EmptyResource(),
						Used: EmptyResource(),

						Allocatable: EmptyResource(),
						Capability:  EmptyResource(),

						Pods: map[string]*PodInfo{
							"c1/p2": pi2,
						},
					},
				},
				Pods: map[string]*PodInfo{
					"c1/p1": pi1,
					"c1/p2": pi2,
				},
				Consumers: map[string]*ConsumerInfo{
					"c1": ci1,
				},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Nodes:     make(map[string]*NodeInfo),
			Pods:      make(map[string]*PodInfo),
			Consumers: make(map[string]*ConsumerInfo),
		}

		for _, p := range test.pods {
			cache.AddPod(p)
		}

		for _, c := range test.consumers {
			cache.AddConsumer(c)
		}

		if !cacheEqual(cache, test.expected) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.expected, cache)
		}
	}
}
