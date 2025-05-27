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
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

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

// BuildPodWithResourceClaim builds a pod object with resource claim, currently the pod only contains one container
func BuildPodWithResourceClaim(ns, name, nodeName string, p v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string,
	claimReq []v1.ResourceClaim, resourceClaims []v1.PodResourceClaim) *v1.Pod {
	pod := BuildPod(ns, name, nodeName, p, req, groupName, labels, selector)
	pod.Spec.ResourceClaims = resourceClaims
	pod.Spec.Containers[0].Resources.Claims = claimReq

	return pod
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

func BuildDeviceRequest(name, deviceClassName string, selectors []resourcev1beta1.DeviceSelector,
	allocationMode *resourcev1beta1.DeviceAllocationMode, count *int64) resourcev1beta1.DeviceRequest {
	deviceRequest := resourcev1beta1.DeviceRequest{
		Name:            name,
		DeviceClassName: deviceClassName,
		AllocationMode:  resourcev1beta1.DeviceAllocationModeExactCount,
		Count:           1,
	}

	if selectors != nil {
		deviceRequest.Selectors = selectors
	}

	if allocationMode != nil {
		deviceRequest.AllocationMode = *allocationMode
	}

	if allocationMode != nil && *allocationMode == resourcev1beta1.DeviceAllocationModeExactCount && count != nil {
		deviceRequest.Count = *count
	}

	return deviceRequest
}

func BuildResourceClaim(namespace, name string, deviceRequests []resourcev1beta1.DeviceRequest,
	constraints []resourcev1beta1.DeviceConstraint, config []resourcev1beta1.DeviceClaimConfiguration) *resourcev1beta1.ResourceClaim {
	rc := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "1",
		},
		Spec: resourcev1beta1.ResourceClaimSpec{
			Devices: resourcev1beta1.DeviceClaim{
				Requests: deviceRequests,
			},
		},
	}

	if constraints != nil {
		rc.Spec.Devices.Constraints = constraints
	}

	if config != nil {
		rc.Spec.Devices.Config = config
	}

	return rc
}

func BuildDeviceClass(name string, selectors []resourcev1beta1.DeviceSelector, config []resourcev1beta1.DeviceClassConfiguration) *resourcev1beta1.DeviceClass {
	dc := &resourcev1beta1.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: resourcev1beta1.DeviceClassSpec{
			Selectors: selectors,
		},
	}

	if config != nil {
		dc.Spec.Config = config
	}

	return dc
}

func BuildDevice(name string, attributes map[resourcev1beta1.QualifiedName]resourcev1beta1.DeviceAttribute,
	capacity map[resourcev1beta1.QualifiedName]resourcev1beta1.DeviceCapacity) resourcev1beta1.Device {
	return resourcev1beta1.Device{
		Name: name,
		Basic: &resourcev1beta1.BasicDevice{
			Attributes: attributes,
			Capacity:   capacity,
		},
	}
}

func BuildResourceSlice(name, driver, nodeName string, pool resourcev1beta1.ResourcePool, devices []resourcev1beta1.Device) *resourcev1beta1.ResourceSlice {
	return &resourcev1beta1.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: resourcev1beta1.ResourceSliceSpec{
			NodeName: nodeName,
			Driver:   driver,
			Pool:     pool,
			Devices:  devices,
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

// BuildBestEffortPod builds a BestEffort pod object
func BuildBestEffortPod(namespace, name, nodeName string, p v1.PodPhase, groupName string, labels map[string]string, selector map[string]string) *v1.Pod {
	return BuildPod(namespace, name, nodeName, p, v1.ResourceList{}, groupName, labels, selector)
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

// BuildPodGroup return podgroup with base spec and phase status
func BuildPodGroup(name, ns, queue string, minMember int32, taskMinMember map[string]int32, status schedulingv1beta1.PodGroupPhase) *schedulingv1beta1.PodGroup {
	return &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: schedulingv1beta1.PodGroupSpec{
			Queue:         queue,
			MinMember:     minMember,
			MinTaskMember: taskMinMember,
		},
		Status: schedulingv1beta1.PodGroupStatus{
			Phase: status,
		},
	}
}

// BuildPodGroupWithNetWorkTopologies builds podGroup with NetWorkTopologies.
func BuildPodGroupWithNetWorkTopologies(name, ns, hyperNodeName, queue string, minMember int32, taskMinMember map[string]int32, status schedulingv1beta1.PodGroupPhase, mode string, highestTierAllowed int) *schedulingv1beta1.PodGroup {
	pg := BuildPodGroup(name, ns, queue, minMember, taskMinMember, status)
	pg.Annotations = map[string]string{api.JobAllocatedHyperNode: hyperNodeName}
	pg.Spec.NetworkTopology = &schedulingv1beta1.NetworkTopologySpec{
		Mode:               schedulingv1beta1.NetworkTopologyMode(mode),
		HighestTierAllowed: &highestTierAllowed,
	}
	return pg
}

// BuildPodGroupWithMinResources return podgroup with base spec and phase status and minResources
func BuildPodGroupWithMinResources(name, ns, queue string, minMember int32, taskMinMember map[string]int32, minResources v1.ResourceList, status schedulingv1beta1.PodGroupPhase) *schedulingv1beta1.PodGroup {
	return &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: schedulingv1beta1.PodGroupSpec{
			Queue:         queue,
			MinMember:     minMember,
			MinResources:  &minResources,
			MinTaskMember: taskMinMember,
		},
		Status: schedulingv1beta1.PodGroupStatus{
			Phase: status,
		},
	}
}

func BuildResourceQuota(name, ns string, hard v1.ResourceList) *v1.ResourceQuota {
	return &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: hard,
		},
		Status: v1.ResourceQuotaStatus{
			Hard: hard,
		},
	}
}

// BuildPodGroupWithPrio return podgroup with podgroup PriorityClassName
func BuildPodGroupWithPrio(name, ns, queue string, minMember int32, taskMinMember map[string]int32, status schedulingv1beta1.PodGroupPhase, prioName string) *schedulingv1beta1.PodGroup {
	pg := BuildPodGroup(name, ns, queue, minMember, taskMinMember, status)
	pg.Spec.PriorityClassName = prioName
	return pg
}

// BuildPodGroupWithAnno returns a podgroup object with annotations
func BuildPodGroupWithAnno(name, ns, queue string, minMember int32, taskMinMember map[string]int32, status schedulingv1beta1.PodGroupPhase, annos map[string]string) *schedulingv1beta1.PodGroup {
	pg := BuildPodGroup(name, ns, queue, minMember, taskMinMember, status)
	pg.Annotations = annos
	return pg
}

///////////// function to build queue  ///////////////////

// BuildQueue returns a new Queue object with the "Open" state.
func BuildQueue(qname string, weight int32, cap v1.ResourceList) *schedulingv1beta1.Queue {
	return &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: qname,
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:     weight,
			Capability: cap,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}
}

func BuildQueueWithState(qname string, weight int32, cap v1.ResourceList, state schedulingv1beta1.QueueState) *schedulingv1beta1.Queue {
	return &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: qname,
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:     weight,
			Capability: cap,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: state,
		},
	}
}

// BuildQueueWithAnnos return a Queue with annotations
func BuildQueueWithAnnos(qname string, weight int32, cap v1.ResourceList, annos map[string]string) *schedulingv1beta1.Queue {
	queue := BuildQueue(qname, weight, cap)
	queue.ObjectMeta.Annotations = annos
	return queue
}

// BuildQueueWithResourcesQuantity return a queue with deserved and capability resources quantity.
func BuildQueueWithResourcesQuantity(qname string, deserved, cap v1.ResourceList) *schedulingv1beta1.Queue {
	queue := BuildQueue(qname, 1, cap)
	queue.Spec.Deserved = deserved
	return queue
}

// BuildQueueWithPriorityAndResourcesQuantity return a queue with priority, deserved and capability resources quantity.
func BuildQueueWithPriorityAndResourcesQuantity(qname string, priority int32, deserved, cap v1.ResourceList) *schedulingv1beta1.Queue {
	queue := BuildQueue(qname, 1, cap)
	queue.Spec.Deserved = deserved
	queue.Spec.Priority = priority
	return queue
}

// ////// build in resource //////
// BuildPriorityClass return pc
func BuildPriorityClass(name string, value int32) *schedulingv1.PriorityClass {
	return &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Value: value,
	}
}

// BuildPriorityClassWithPreemptionPolicy return a priorityClass with value and preemptionPolicy
func BuildPriorityClassWithPreemptionPolicy(name string, value int32, preemptionPolicy v1.PreemptionPolicy) *schedulingv1.PriorityClass {
	pc := BuildPriorityClass(name, value)
	pc.PreemptionPolicy = &preemptionPolicy
	return pc
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
func (fb *FakeBinder) Bind(kubeClient kubernetes.Interface, tasks []*api.TaskInfo) map[api.TaskID]string {
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
