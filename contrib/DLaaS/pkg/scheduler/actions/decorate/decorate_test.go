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

package decorate

import (
	"reflect"
	"sort"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"
)

func buildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:      resource.MustParse(cpu),
		v1.ResourceMemory:   resource.MustParse(memory),
		api.GPUResourceName: resource.MustParse("0"),
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

func buildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func nodesEqual(l, r []string) bool {
	if len(l) != len(r) {
		return false
	}

	sort.Sort(sort.StringSlice(l))
	sort.Sort(sort.StringSlice(r))

	return reflect.DeepEqual(l, r)
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name      string
		schedSpec *arbv1.SchedulingSpec
		nodes     []*v1.Node
		expected  []string
	}{
		{
			name: "one Job selects one node from three",
			schedSpec: &arbv1.SchedulingSpec{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						buildOwnerReference("j1"),
					},
				},
				Spec: arbv1.SchedulingSpecTemplate{
					NodeSelector: map[string]string{
						"label_1": "val_1",
					},
				},
			},
			nodes: []*v1.Node{
				buildNode("n1", buildResourceList("2", "4Gi"), map[string]string{"label_1": "val_1"}),
				buildNode("n2", buildResourceList("2", "4Gi"), map[string]string{"label_2": "val_2"}),
				buildNode("n3", buildResourceList("2", "4Gi"), map[string]string{"label_3": "val_3"}),
			},
			expected: []string{"n1"},
		},
	}

	decorate := New()

	for i, test := range tests {
		schedulerCache := &cache.SchedulerCache{
			Nodes: make(map[string]*api.NodeInfo),
			Jobs:  make(map[api.JobID]*api.JobInfo),
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}

		schedulerCache.AddSchedulingSpec(test.schedSpec)

		ssn := framework.OpenSession(schedulerCache, nil)
		defer framework.CloseSession(ssn)

		decorate.Execute(ssn)

		//
		var got []string
		for _, node := range ssn.Jobs[0].Candidates {
			got = append(got, node.Name)
		}

		if !nodesEqual(got, test.expected) {
			t.Errorf("Case %d: expected %v, got %v",
				i, test.expected, got)
		}

	}
}
