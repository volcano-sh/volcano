/*
Copyright 2018 The Kubernetes Authors.

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/scheduling"
)

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

func buildPod(ns, n, nn string, p v1.PodPhase, req v1.ResourceList, owner []metav1.OwnerReference, labels map[string]string) *v1.Pod {
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

func buildResource(cpu string, memory string, scalarResources map[string]string, maxTaskNum int) *Resource {
	resourceList := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
	for key, value := range scalarResources {
		resourceList[v1.ResourceName(key)] = resource.MustParse(value)
	}
	resource := NewResource(resourceList)
	if maxTaskNum != -1 {
		resource.MaxTaskNum = maxTaskNum
	}
	return resource
}

func buildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

type ScalarResource struct {
	Name  string
	Value string
}

// BuildResourceList builds resource list object
func BuildResourceList(cpu string, memory string, scalarResources ...ScalarResource) v1.ResourceList {
	resourceList := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
	for _, scalar := range scalarResources {
		resourceList[v1.ResourceName(scalar.Name)] = resource.MustParse(scalar.Value)
	}

	return resourceList
}

// BuildResourceListWithGPU builds resource list with GPU
func BuildResourceListWithGPU(cpu string, memory string, GPU string, scalarResources ...ScalarResource) v1.ResourceList {
	resourceList := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
		GPUResourceName:   resource.MustParse(GPU),
	}
	for _, scalar := range scalarResources {
		resourceList[v1.ResourceName(scalar.Name)] = resource.MustParse(scalar.Value)
	}

	return resourceList
}

// BuildPodgroup builds podgroup
func BuildPodgroup(name, ns string, minMember int32, minResource v1.ResourceList) scheduling.PodGroup {
	return scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: scheduling.PodGroupSpec{
			MinMember:    minMember,
			MinResources: &minResource,
		},
	}
}
