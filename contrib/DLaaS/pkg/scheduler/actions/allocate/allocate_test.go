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

package allocate

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/plugins/drf"
)

func buildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:      resource.MustParse(cpu),
		v1.ResourceMemory:   resource.MustParse(memory),
		api.GPUResourceName: resource.MustParse("0"),
	}
}

func buildResourceListWithGPU(cpu string, memory string, GPU string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:      resource.MustParse(cpu),
		v1.ResourceMemory:   resource.MustParse(memory),
		api.GPUResourceName: resource.MustParse(GPU),
	}
}

func buildNode(name string, alloc v1.ResourceList, labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

func buildPod(ns, n, nn string, p v1.PodPhase, req v1.ResourceList, owner []metav1.OwnerReference, labels map[string]string, selector map[string]string) *v1.Pod {
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
			NodeName:     nn,
			NodeSelector: selector,
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

type fakeBinder struct {
	binds map[string]string
	c     chan string
}

func (fb *fakeBinder) Bind(p *v1.Pod, hostname string) error {
	key := fmt.Sprintf("%v/%v", p.Namespace, p.Name)

	fb.binds[key] = hostname

	fb.c <- key

	return nil
}

func TestAllocate(t *testing.T) {
	framework.RegisterPluginBuilder("drf", drf.New)
	defer framework.CleanupPluginBuilders()

	owner1 := buildOwnerReference("owner1")
	owner2 := buildOwnerReference("owner2")

	tests := []struct {
		name       string
		schedSpecs []*arbv1.SchedulingSpec
		pods       []*v1.Pod
		nodes      []*v1.Node
		expected   map[string]string
	}{
		{
			name: "one Job with two Pods on one node",
			schedSpecs: []*arbv1.SchedulingSpec{
				{
					ObjectMeta: metav1.ObjectMeta{
						OwnerReferences: []metav1.OwnerReference{owner1},
					},
				},
			},
			pods: []*v1.Pod{
				// pending pod with owner, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string), make(map[string]string)),

				// pending pod with owner, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2", "4Gi"), make(map[string]string)),
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
		},
		{
			name: "two Jobs on one node",
			schedSpecs: []*arbv1.SchedulingSpec{
				{
					ObjectMeta: metav1.ObjectMeta{
						OwnerReferences: []metav1.OwnerReference{owner1},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						OwnerReferences: []metav1.OwnerReference{owner2},
					},
				},
			},

			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string), make(map[string]string)),

				// pending pod with owner1, under c1
				buildPod("c1", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner1}, make(map[string]string), make(map[string]string)),

				// pending pod with owner2, under c2
				buildPod("c2", "p1", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string), make(map[string]string)),

				// pending pod with owner, under c2
				buildPod("c2", "p2", "", v1.PodPending, buildResourceList("1", "1G"), []metav1.OwnerReference{owner2}, make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2", "4G"), make(map[string]string)),
			},
			expected: map[string]string{
				"c2/p1": "n1",
				"c1/p1": "n1",
			},
		},
	}

	allocate := New()

	for i, test := range tests {
		binder := &fakeBinder{
			binds: map[string]string{},
			c:     make(chan string),
		}
		schedulerCache := &cache.SchedulerCache{
			Nodes:  make(map[string]*api.NodeInfo),
			Jobs:   make(map[api.JobID]*api.JobInfo),
			Binder: binder,
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		for _, ss := range test.schedSpecs {
			schedulerCache.AddSchedulingSpec(ss)
		}

		args := &framework.PluginArgs{
			Name:                 "drf",
			PreemptableFnEnabled: true,
			JobOrderFnEnabled:    true,
		}

		ssn := framework.OpenSession(schedulerCache, []*framework.PluginArgs{args})
		defer framework.CloseSession(ssn)

		allocate.Execute(ssn)

		for i := 0; i < len(test.expected); i++ {
			select {
			case <-binder.c:
			case <-time.After(3 * time.Second):
				t.Errorf("Failed to get binding request.")
			}
		}

		if !reflect.DeepEqual(test.expected, binder.binds) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.expected, binder.binds)
		}
	}
}
