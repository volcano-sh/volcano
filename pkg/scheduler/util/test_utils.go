/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	volumescheduling "k8s.io/kubernetes/pkg/controller/volume/scheduling"

	schedulingv2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// BuildResourceList builts resource list object
func BuildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:      resource.MustParse(cpu),
		v1.ResourceMemory:   resource.MustParse(memory),
		api.GPUResourceName: resource.MustParse("0"),
	}
}

// BuildResourceListWithGPU builts resource list with GPU
func BuildResourceListWithGPU(cpu string, memory string, GPU string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:      resource.MustParse(cpu),
		v1.ResourceMemory:   resource.MustParse(memory),
		api.GPUResourceName: resource.MustParse(GPU),
	}
}

// BuildNode builts node object
func BuildNode(name string, alloc v1.ResourceList, labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

// BuildPod builts Pod object
func BuildPod(namespace, name, nodename string, p v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				schedulingv2.KubeGroupNameAnnotationKey: groupName,
			},
		},
		Status: v1.PodStatus{
			Phase: p,
		},
		Spec: v1.PodSpec{
			NodeName:     nodename,
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

// FakeBinder is used as fake binder
type FakeBinder struct {
	Binds   map[string]string
	Channel chan string
}

// Bind used by fake binder struct to bind pods
func (fb *FakeBinder) Bind(kubeClient *kubernetes.Clientset, tasks []*api.TaskInfo) (error, []*api.TaskInfo) {
	for _, p := range tasks {
		key := fmt.Sprintf("%v/%v", p.Namespace, p.Name)
		fb.Binds[key] = p.NodeName
	}

	return nil, nil
}

// FakeEvictor is used as fake evictor
type FakeEvictor struct {
	sync.Mutex
	evicts  []string
	Channel chan string
}

// Evicts returns copy of evicted pods.
func (fe *FakeEvictor) Evicts() []string {
	fe.Lock()
	defer fe.Unlock()
	return append([]string{}, fe.evicts...)
}

// Evict is used by fake evictor to evict pods
func (fe *FakeEvictor) Evict(p *v1.Pod, reason string) error {
	fe.Lock()
	defer fe.Unlock()

	fmt.Println("PodName: ", p.Name)
	key := fmt.Sprintf("%v/%v", p.Namespace, p.Name)
	fe.evicts = append(fe.evicts, key)

	fe.Channel <- key

	return nil
}

// FakeStatusUpdater is used for fake status update
type FakeStatusUpdater struct {
}

// UpdatePodCondition is a empty function
func (ftsu *FakeStatusUpdater) UpdatePodCondition(pod *v1.Pod, podCondition *v1.PodCondition) (*v1.Pod, error) {
	// do nothing here
	return nil, nil
}

// UpdatePodGroup is a empty function
func (ftsu *FakeStatusUpdater) UpdatePodGroup(pg *api.PodGroup) (*api.PodGroup, error) {
	// do nothing here
	return nil, nil
}

// FakeVolumeBinder is used as fake volume binder
type FakeVolumeBinder struct {
}

// AllocateVolumes is a empty function
func (fvb *FakeVolumeBinder) AllocateVolumes(task *api.TaskInfo, hostname string, podVolumes *volumescheduling.PodVolumes) error {
	return nil
}

// BindVolumes is a empty function
func (fvb *FakeVolumeBinder) BindVolumes(task *api.TaskInfo, podVolumes *volumescheduling.PodVolumes) error {
	return nil
}

// GetPodVolumes is a empty function
func (fvb *FakeVolumeBinder) GetPodVolumes(task *api.TaskInfo, node *v1.Node) (*volumescheduling.PodVolumes, error) {
	return nil, nil
}
