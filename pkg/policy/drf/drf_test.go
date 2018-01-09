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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
)

func init() {
	logLevel := os.Getenv("TEST_LOG_LEVEL")
	if len(logLevel) != 0 {
		flag.Parse()
		flag.Lookup("logtostderr").Value.Set("true")
		flag.Lookup("v").Value.Set(logLevel)
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

func buildConsumer(name string, namespace string) *arbv1.Consumer {
	return &arbv1.Consumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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

func TestAllocate(t *testing.T) {
	owner1 := metav1.OwnerReference{
		UID: "owner1",
	}
	owner2 := metav1.OwnerReference{
		UID: "owner2",
	}

	tests := []struct {
		name      string
		pods      []*v1.Pod
		nodes     []*v1.Node
		consumers []*arbv1.Consumer
		expected  map[string]string
	}{
		{
			name: "one consumer with two Pods on one node",
			pods: []*v1.Pod{
				// pending pod with owner, under c1
				buildPod("c1", "p1", "", v1.PodPending, v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1G"),
				}, []metav1.OwnerReference{owner1}),

				// pending pod with owner, under c1
				buildPod("c1", "p2", "", v1.PodPending, v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1G"),
				}, []metav1.OwnerReference{owner1}),
			},
			nodes: []*v1.Node{
				buildNode("n1", v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				}),
			},
			consumers: []*arbv1.Consumer{
				buildConsumer("c1", "c1"),
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
		},
		{
			name: "two consumer on one node",
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1G"),
				}, []metav1.OwnerReference{owner1}),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1G"),
				}, []metav1.OwnerReference{owner1}),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1G"),
				}, []metav1.OwnerReference{owner2}),

				// pending pod with owner, under c2
				buildPod("c2", "p2", "", v1.PodPending, v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1G"),
				}, []metav1.OwnerReference{owner2}),
			},
			nodes: []*v1.Node{
				buildNode("n1", v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				}),
			},
			consumers: []*arbv1.Consumer{
				buildConsumer("c1", "c1"),
				buildConsumer("c2", "c2"),
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
			Nodes:     make(map[string]*cache.NodeInfo),
			Pods:      make(map[string]*cache.PodInfo),
			Consumers: make(map[string]*cache.ConsumerInfo),
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, consumer := range test.consumers {
			schedulerCache.AddConsumer(consumer)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		snapshot := schedulerCache.Snapshot()

		expected := drf.Allocate(snapshot.Consumers, snapshot.Nodes)
		for _, consumer := range expected {
			for _, ps := range consumer.PodSets {
				for _, p := range ps.Pending {
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
