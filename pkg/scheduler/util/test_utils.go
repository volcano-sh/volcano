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
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// NodeWrapper wraps a Kubernetes Node.
type NodeWrapper struct {
	v1.Node
}

// MakeNode creates a new NodeWrapper with the specified name.
func MakeNode(name string) *NodeWrapper {
	return &NodeWrapper{
		Node: v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Status: v1.NodeStatus{},
		},
	}
}

// Annotations sets the annotations of the node.
func (n *NodeWrapper) Annotations(annotations map[string]string) *NodeWrapper {
	n.ObjectMeta.Annotations = annotations
	return n
}

// Labels updates the labels of the node.
func (n *NodeWrapper) Labels(labels map[string]string) *NodeWrapper {
	n.ObjectMeta.Labels = labels
	return n
}

// Capacity sets the capacity of the node.
func (n *NodeWrapper) Capacity(capacity v1.ResourceList) *NodeWrapper {
	n.Status.Capacity = capacity
	return n
}

// Allocatable sets the allocatable resources of the node.
func (n *NodeWrapper) Allocatable(allocatable v1.ResourceList) *NodeWrapper {
	n.Status.Allocatable = allocatable
	return n
}

// Obj returns the inner Node.
func (n *NodeWrapper) Obj() *v1.Node {
	return &n.Node
}

// ContainerWrapper helps in building a Kubernetes container.
type ContainerWrapper struct {
	v1.Container
}

// NewContainer creates a new ContainerWrapper.
func NewContainer(name, image string) *ContainerWrapper {
	return &ContainerWrapper{
		Container: v1.Container{
			Name:  name,
			Image: image,
		},
	}
}

func BuildCSINode(name string, annotations map[string]string, drivers []storagev1.CSINodeDriver) *storagev1.CSINode {
	return &storagev1.CSINode{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: storagev1.CSINodeSpec{
			Drivers: drivers,
		},
	}
}

// Requests sets the resource requests for the container using a ResourceList.
func (c *ContainerWrapper) Requests(req v1.ResourceList) *ContainerWrapper {
	c.Resources.Requests = req
	return c
}

// Limits sets the resource limits for the container using a ResourceList.
func (c *ContainerWrapper) Limits(lim v1.ResourceList) *ContainerWrapper {
	c.Resources.Limits = lim
	return c
}

// Obj returns the inner Container.
func (c *ContainerWrapper) Obj() *v1.Container {
	return &c.Container
}

// PodWrapper wraps a Kubernetes Pod.
type PodWrapper struct {
	v1.Pod
}

// MakePod creates a new PodWrapper with the specified parameters.
func MakePod(namespace, name string) *PodWrapper {
	return &PodWrapper{
		Pod: v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:         types.UID(fmt.Sprintf("%v-%v", namespace, name)),
				Name:        name,
				Namespace:   namespace,
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Spec:   v1.PodSpec{},
			Status: v1.PodStatus{},
		},
	}
}

// Annotations sets the annotations of the pod.
func (p *PodWrapper) Annotations(annotations map[string]string) *PodWrapper {
	p.ObjectMeta.Annotations = annotations
	return p
}

// Labels updates the labels of the pod.
func (p *PodWrapper) Labels(labels map[string]string) *PodWrapper {
	p.ObjectMeta.Labels = labels
	return p
}

// NodeName sets the nodeName of the pod.
func (p *PodWrapper) NodeName(nodeName string) *PodWrapper {
	p.Spec.NodeName = nodeName
	return p
}

// GroupName sets the groupName of the pod.
func (p *PodWrapper) GroupName(groupName string) *PodWrapper {
	if p.ObjectMeta.Annotations == nil {
		p.ObjectMeta.Annotations = map[string]string{}
	}
	p.ObjectMeta.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey] = groupName
	return p
}

// NodeSelector sets the nodeSelector of the pod.
func (p *PodWrapper) NodeSelector(selector map[string]string) *PodWrapper {
	p.Spec.NodeSelector = selector
	return p
}

// Containers adds or replaces containers in the pod spec.
func (p *PodWrapper) Containers(containers []v1.Container) *PodWrapper {
	p.Spec.Containers = containers
	return p
}

// Priority adds the priority of the pod.
func (p *PodWrapper) Priority(priority *int32) *PodWrapper {
	p.Spec.Priority = priority
	return p
}

func (p *PodWrapper) PreemptionPolicy(preemptionPolicy v1.PreemptionPolicy) *PodWrapper {
	p.Spec.PreemptionPolicy = &preemptionPolicy
	return p
}

// Phase add the phase of the pod.
func (p *PodWrapper) Phase(phase v1.PodPhase) *PodWrapper {
	p.Status.Phase = phase
	return p
}

// PVC adds a PersistentVolumeClaim to the pod's volumes and mounts it in specified containers.
func (p *PodWrapper) PVC(pvc *v1.PersistentVolumeClaim, mountContainers ...string) *PodWrapper {
	if p.Spec.Volumes == nil {
		p.Spec.Volumes = make([]v1.Volume, 0)
	}

	newVolume := v1.Volume{
		Name: pvc.Name,
		VolumeSource: v1.VolumeSource{
			PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvc.Name,
			},
		},
	}

	exists := false
	for _, volume := range p.Spec.Volumes {
		if volume.Name == newVolume.Name {
			exists = true
			break
		}
	}

	if !exists {
		p.Spec.Volumes = append(p.Spec.Volumes, newVolume)
	}

	for i, container := range p.Spec.Containers {
		if len(mountContainers) == 0 || containsString(mountContainers, container.Name) {
			p.Spec.Containers[i].VolumeMounts = append(container.VolumeMounts, v1.VolumeMount{
				Name:      pvc.Name,
				MountPath: "/data",
			})
		}
	}

	return p
}

// help function to check if a string exists in a slice
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// Obj returns the inner Pod.
func (p *PodWrapper) Obj() *v1.Pod {
	return &p.Pod
}

// BuildPod builds a Burstable pod object
func BuildPod(namespace, name, nodeName string, p v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: groupName,
			},
		},
		Status: v1.PodStatus{
			Phase: p,
		},
		Spec: v1.PodSpec{
			NodeName:     nodeName,
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

// BuildPodWithPVC builts Pod object with pvc volume
func BuildPodWithPVC(namespace, name, nodename string, p v1.PodPhase, req v1.ResourceList, pvc *v1.PersistentVolumeClaim, groupName string, labels map[string]string, selector map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: groupName,
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
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      pvc.Name,
							MountPath: "/data",
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: pvc.Name,
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
						},
					},
				},
			},
		},
	}
}

// BuildPVC builds a PVC with specified storageclass and required resources
func BuildPVC(namespace, name string, req v1.ResourceList, scName string) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "1",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			Resources: v1.VolumeResourceRequirements{
				Requests: req,
			},
			StorageClassName: &scName,
		},
	}
}

// BuildPV builds a PV with specified storageclass and capacity
func BuildPV(name, scName string, capacity v1.ResourceList) *v1.PersistentVolume {
	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: "1",
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName: scName,
			Capacity:         capacity,
		},
		Status: v1.PersistentVolumeStatus{
			Phase: v1.VolumeAvailable,
		},
	}
}

// BuildStorageClass build a storageclass object with specified provisioner and volumeBindingMode
func BuildStorageClass(name, provisioner string, volumeBindingMode storagev1.VolumeBindingMode) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: "1",
		},
		Provisioner:       provisioner,
		VolumeBindingMode: &volumeBindingMode,
	}
}

// BuildPodWithPriority builds a pod object with priority
func BuildPodWithPriority(namespace, name, nodeName string, p v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string, priority *int32) *v1.Pod {
	pod := BuildPod(namespace, name, nodeName, p, req, groupName, labels, selector)
	pod.Spec.Priority = priority
	return pod
}

// BuildPodWithPreemptionPolicy builds a pod with preemptionPolicy
func BuildPodWithPreemptionPolicy(namespace, name, nodeName string, p v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string, preemptionPolicy v1.PreemptionPolicy) *v1.Pod {
	pod := BuildPod(namespace, name, nodeName, p, req, groupName, labels, selector)
	pod.Spec.PreemptionPolicy = &preemptionPolicy
	return pod
}

// PodGroupWrapper wraps a PodGroup for fluent construction.
type PodGroupWrapper struct {
	*schedulingv1beta1.PodGroup
}

// MakePodGroup creates a new PodGroupWrapper with the given name and namespace.
func MakePodGroup(name, ns string) *PodGroupWrapper {
	return &PodGroupWrapper{
		PodGroup: &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: schedulingv1beta1.PodGroupSpec{},
			Status: schedulingv1beta1.PodGroupStatus{
				Phase: schedulingv1beta1.PodGroupPending,
			},
		},
	}
}

// Queue sets the queue of the podgroup.
func (p *PodGroupWrapper) Queue(queue string) *PodGroupWrapper {
	p.Spec.Queue = queue
	return p
}

// MinMember sets the minimum number of members required in the podgroup.
func (p *PodGroupWrapper) MinMember(min int32) *PodGroupWrapper {
	p.Spec.MinMember = min
	return p
}

// MinResources sets the minimal resources required for the podgroup.
func (p *PodGroupWrapper) MinResources(res v1.ResourceList) *PodGroupWrapper {
	if res == nil {
		return p
	}
	resCopy := make(v1.ResourceList)
	for k, v := range res {
		resCopy[k] = v.DeepCopy()
	}
	p.Spec.MinResources = &resCopy
	return p
}

// PriorityClassName sets the priority class name for the podgroup.
func (p *PodGroupWrapper) PriorityClassName(name string) *PodGroupWrapper {
	p.Spec.PriorityClassName = name
	return p
}

// Annotations sets annotations on the podgroup.
func (p *PodGroupWrapper) Annotations(annos map[string]string) *PodGroupWrapper {
	if p.ObjectMeta.Annotations == nil {
		p.ObjectMeta.Annotations = make(map[string]string)
	}
	for k, v := range annos {
		p.ObjectMeta.Annotations[k] = v
	}
	return p
}

// Phase sets the phase of the podgroup status.
func (p *PodGroupWrapper) Phase(phase schedulingv1beta1.PodGroupPhase) *PodGroupWrapper {
	p.Status.Phase = phase
	return p
}

// TaskMinMember sets the task-level minimum member requirements.
func (p *PodGroupWrapper) TaskMinMember(taskMinMember map[string]int32) *PodGroupWrapper {
	if taskMinMember == nil {
		return p
	}
	p.Spec.MinTaskMember = make(map[string]int32)
	for k, v := range taskMinMember {
		p.Spec.MinTaskMember[k] = v
	}
	return p
}

// Obj returns the raw PodGroup object.
func (p *PodGroupWrapper) Obj() *schedulingv1beta1.PodGroup {
	return p.PodGroup
}

// ResourceQuotaWrapper wraps a ResourceQuota for fluent construction.
type ResourceQuotaWrapper struct {
	*v1.ResourceQuota
}

func MakeResourceQuota(name, ns string) *ResourceQuotaWrapper {
	return &ResourceQuotaWrapper{
		ResourceQuota: &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec:   v1.ResourceQuotaSpec{},
			Status: v1.ResourceQuotaStatus{},
		},
	}
}

func (r *ResourceQuotaWrapper) Hard(hard v1.ResourceList) *ResourceQuotaWrapper {
	r.Spec.Hard = hard
	r.Status.Hard = hard
	return r
}

func (r *ResourceQuotaWrapper) Obj() *v1.ResourceQuota {
	return r.ResourceQuota
}

// QueueWrapper wraps a schedulingv1beta1.Queue for fluent construction.
type QueueWrapper struct {
	*schedulingv1beta1.Queue
}

// MakeQueue creates a new QueueWrapper with the given name and default fields.
func MakeQueue(name string) *QueueWrapper {
	return &QueueWrapper{
		Queue: &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1, // default
			},
			Status: schedulingv1beta1.QueueStatus{
				State: schedulingv1beta1.QueueStateOpen,
			},
		},
	}
}

// Weight sets the weight of the queue.
func (q *QueueWrapper) Weight(w int32) *QueueWrapper {
	q.Spec.Weight = w
	return q
}

// Capability sets the resource capability of the queue.
func (q *QueueWrapper) Capability(cap v1.ResourceList) *QueueWrapper {
	if cap == nil {
		return q
	}
	copied := make(v1.ResourceList)
	for k, v := range cap {
		copied[k] = v.DeepCopy()
	}
	q.Spec.Capability = copied
	return q
}

// Deserved sets the deserving resources of the queue.
func (q *QueueWrapper) Deserved(deserved v1.ResourceList) *QueueWrapper {
	if deserved == nil {
		return q
	}
	copied := make(v1.ResourceList)
	for k, v := range deserved {
		copied[k] = v.DeepCopy()
	}
	q.Spec.Deserved = copied
	return q
}

// Priority sets the priority of the queue.
func (q *QueueWrapper) Priority(priority int32) *QueueWrapper {
	q.Spec.Priority = priority
	return q
}

func (q *QueueWrapper) Parent(parent string) *QueueWrapper {
	q.Spec.Parent = parent
	return q
}

// Annotations adds annotations to the queue object metadata.
func (q *QueueWrapper) Annotations(annos map[string]string) *QueueWrapper {
	if q.ObjectMeta.Annotations == nil {
		q.ObjectMeta.Annotations = make(map[string]string)
	}
	for k, v := range annos {
		q.ObjectMeta.Annotations[k] = v
	}
	return q
}

// State sets the state of the queue.
func (q *QueueWrapper) State(state schedulingv1beta1.QueueState) *QueueWrapper {
	q.Status.State = state
	return q
}

// Obj returns the raw Queue object.
func (q *QueueWrapper) Obj() *schedulingv1beta1.Queue {
	return q.Queue
}

// PriorityClassWrapper wraps a schedulingv1.PriorityClass for fluent construction.
type PriorityClassWrapper struct {
	priorityClass *schedulingv1.PriorityClass
}

// MakePriorityClass creates a new PriorityClassWrapper.
func MakePriorityClass(name string) *PriorityClassWrapper {
	return &PriorityClassWrapper{
		priorityClass: &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}
}

// PreemptionPolicy sets the preemption policy for the priority class.
func (p *PriorityClassWrapper) PreemptionPolicy(policy v1.PreemptionPolicy) *PriorityClassWrapper {
	p.priorityClass.PreemptionPolicy = &policy
	return p
}

func (p *PriorityClassWrapper) Value(value int32) *PriorityClassWrapper {
	p.priorityClass.Value = value
	return p
}

// Obj returns the raw PriorityClass object.
func (p *PriorityClassWrapper) Obj() *schedulingv1.PriorityClass {
	return p.priorityClass
}

// FakeBinder is used as fake binder
type FakeBinder struct {
	sync.RWMutex
	binds   map[string]string
	Channel chan string
}

// NewFakeBinder returns a instance of FakeBinder
func NewFakeBinder(buffer int) *FakeBinder {
	return &FakeBinder{
		binds:   make(map[string]string, buffer),
		Channel: make(chan string, buffer),
	}
}

// Binds returns the binding results
func (fb *FakeBinder) Binds() map[string]string {
	fb.RLock()
	defer fb.RUnlock()
	ret := make(map[string]string, len(fb.binds))
	for k, v := range fb.binds {
		ret[k] = v
	}
	return ret
}

// Length returns the number of bindings
func (fb *FakeBinder) Length() int {
	fb.RLock()
	defer fb.RUnlock()
	return len(fb.binds)
}

// Bind used by fake binder struct to bind pods
func (fb *FakeBinder) Bind(_ kubernetes.Interface, tasks []*api.TaskInfo) map[api.TaskID]string {
	fb.Lock()
	defer fb.Unlock()
	for _, p := range tasks {
		key := fmt.Sprintf("%v/%v", p.Namespace, p.Name)
		fb.binds[key] = p.NodeName
		fb.Channel <- key // need to wait binding pod because Bind process is asynchronous
	}
	return nil
}

// FakeEvictor is used as fake evictor
type FakeEvictor struct {
	sync.RWMutex
	evicts  []string
	Channel chan string
}

// NewFakeEvictor returns a new FakeEvictor instance
func NewFakeEvictor(buffer int) *FakeEvictor {
	return &FakeEvictor{
		evicts:  make([]string, 0, buffer),
		Channel: make(chan string, buffer),
	}
}

// Evicts returns copy of evicted pods.
func (fe *FakeEvictor) Evicts() []string {
	fe.RLock()
	defer fe.RUnlock()
	return append([]string{}, fe.evicts...)
}

// Length returns the number of evicts
func (fe *FakeEvictor) Length() int {
	fe.RLock()
	defer fe.RUnlock()
	return len(fe.evicts)
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

// UpdatePodStatus is an empty function
func (ftsu *FakeStatusUpdater) UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error) {
	// Directly return the pod and do nothing here
	return pod, nil
}

// UpdatePodGroup is an empty function
func (ftsu *FakeStatusUpdater) UpdatePodGroup(pg *api.PodGroup) (*api.PodGroup, error) {
	// Directly return the pg and do nothing here
	return pg, nil
}

// UpdateQueueStatus do fake empty update
func (ftsu *FakeStatusUpdater) UpdateQueueStatus(queue *api.QueueInfo) error {
	// do nothing here
	return nil
}
