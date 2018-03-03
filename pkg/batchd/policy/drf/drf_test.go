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

package drf

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/cache"
)

func init() {
	logLevel := os.Getenv("TEST_LOG_LEVEL")
	if len(logLevel) != 0 {
		flag.Parse()
		flag.Lookup("logtostderr").Value.Set("true")
		flag.Lookup("v").Value.Set(logLevel)
	}
}

func buildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
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

func buildQueue(name string, namespace string) *arbv1.Queue {
	return &arbv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func buildPod(ns, n, nn string, p v1.PodPhase, req v1.ResourceList, owner []metav1.OwnerReference, labels map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
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

func buildPdb(n string, min int, selectorMap map[string]string) *v1beta1.PodDisruptionBudget {
	selector := &metav1.LabelSelector{
		MatchLabels: selectorMap,
	}
	minAvailable := intstr.FromInt(min)
	return &v1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: n,
		},
		Spec: v1beta1.PodDisruptionBudgetSpec{
			Selector:     selector,
			MinAvailable: &minAvailable,
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

func TestAllocate(t *testing.T) {
	owner1 := buildOwnerReference("owner1")
	owner2 := buildOwnerReference("owner2")

	tests := []struct {
		name     string
		pods     []*v1.Pod
		nodes    []*v1.Node
		queues   []*arbv1.Queue
		pdbs     []*v1beta1.PodDisruptionBudget
		expected map[string]string
	}{
		{
			name: "one queue with two Pods on one node",
			pods: []*v1.Pod{
				// pending pod with owner, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string)),

				// pending pod with owner, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string)),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2", "4Gi")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
		},
		{
			name: "two queue on one node",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string)),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod with owner, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2", "4G")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
				buildQueue("c2", "c2"),
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c2/p1": "n1",
			},
		},
		{
			name: "two queue on one node, with non-owner pods",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string)),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string)),

				// pending pod without owner, under c1
				buildPod("c1", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod without owner, under c2
				buildPod("c2", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{}, make(map[string]string)),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2", "4G")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
				buildQueue("c2", "c2"),
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c2/p1": "n1",
			},
		},
	}

	drf := New()

	for i, test := range tests {
		schedulerCache := &cache.SchedulerCache{
			Nodes:  make(map[string]*cache.NodeInfo),
			Pods:   make(map[string]*cache.PodInfo),
			Queues: make(map[string]*cache.QueueInfo),
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, queue := range test.queues {
			schedulerCache.AddQueue(queue)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		snapshot := schedulerCache.Snapshot()

		expected := drf.Allocate(snapshot.Queues, snapshot.Nodes)
		for _, queue := range expected {
			for _, ps := range queue.PodSets {
				for _, p := range ps.Assigned {
					pk := fmt.Sprintf("%v/%v", p.Namespace, p.Name)
					if p.NodeName != test.expected[pk] {
						t.Errorf("case %d (%s): %v/%v expected %s got %s",
							i, test.name, p.Namespace, p.Name, test.expected[pk], p.NodeName)
					}
				}
			}
		}
	}
}

func TestMinAvailable(t *testing.T) {
	owner1 := buildOwnerReference("owner1")
	owner2 := buildOwnerReference("owner2")

	labels1 := map[string]string{
		"minarea": "area1",
	}
	labels2 := map[string]string{
		"minarea": "area2",
	}

	tests := []struct {
		name     string
		pods     []*v1.Pod
		nodes    []*v1.Node
		queues   []*arbv1.Queue
		pdbs     []*v1beta1.PodDisruptionBudget
		expected map[string]int
	}{
		{
			name: "two queue on one node, one queue with pdb",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string)),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("5", "10G")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
				buildQueue("c2", "c2"),
			},
			pdbs: []*v1beta1.PodDisruptionBudget{
				buildPdb("pdb01", 4, labels1),
			},
			expected: map[string]int{
				"c1": 4,
				"c2": 1,
			},
		},
		{
			name: "two queue on one node, two queues with pdb, only one queue can get minAvailable",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("5", "10G")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
				buildQueue("c2", "c2"),
			},
			pdbs: []*v1beta1.PodDisruptionBudget{
				buildPdb("pdb01", 4, labels1),
				buildPdb("pdb02", 2, labels2),
			},
			expected: map[string]int{
				"c1": 5,
				"c2": 0,
			},
		},
		{
			name: "two queue on one node, two queues with pdb, only one queue can get minAvailable",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("5", "10G")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
				buildQueue("c2", "c2"),
			},
			pdbs: []*v1beta1.PodDisruptionBudget{
				buildPdb("pdb01", 4, labels1),
				buildPdb("pdb02", 2, labels2),
			},
			expected: map[string]int{
				"c1": 4,
				"c2": 0,
			},
		},
		{
			name: "two queue on one node, two queues with pdb, two queues could get minAvailable",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner1, under c1
				buildPod("c1", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, labels1),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p3", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p4", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),

				// pending pod with owner2, under c2
				buildPod("c2", "p5", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, labels2),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("5", "10G")),
			},
			queues: []*arbv1.Queue{
				buildQueue("c1", "c1"),
				buildQueue("c2", "c2"),
			},
			pdbs: []*v1beta1.PodDisruptionBudget{
				buildPdb("pdb01", 3, labels1),
				buildPdb("pdb02", 2, labels2),
			},
			expected: map[string]int{
				"c1": 3,
				"c2": 2,
			},
		},
	}

	drf := New()

	for i, test := range tests {
		schedulerCache := &cache.SchedulerCache{
			Nodes:  make(map[string]*cache.NodeInfo),
			Pods:   make(map[string]*cache.PodInfo),
			Queues: make(map[string]*cache.QueueInfo),
			Pdbs:   make(map[string]*cache.PdbInfo),
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, queue := range test.queues {
			schedulerCache.AddQueue(queue)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}
		for _, pdb := range test.pdbs {
			schedulerCache.AddPDB(pdb)
		}

		snapshot := schedulerCache.Snapshot()

		expected := drf.Allocate(snapshot.Queues, snapshot.Nodes)
		for _, queue := range expected {
			assigned := 0
			for _, ps := range queue.PodSets {
				for _, pending := range ps.Assigned {
					if len(pending.NodeName) != 0 {
						assigned++
					}
				}
			}
			if assigned != test.expected[queue.Namespace] {
				t.Errorf("case %d (%s): %s expected %d got %d",
					i, test.name, queue.Namespace, test.expected[queue.Namespace], assigned)
			}
		}
	}
}
