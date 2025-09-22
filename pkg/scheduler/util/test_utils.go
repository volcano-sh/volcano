/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added additional test builder functions for enhanced testing support

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
	"maps"
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

// NodeWrapper wraps a Kubernetes Node.
type NodeWrapper struct {
	v1.Node
}

// MakeNode creates a new NodeWrapper
func MakeNode() *NodeWrapper {
	return &NodeWrapper{
		Node: v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{Type: v1.NodeReady, Status: v1.ConditionTrue},
				},
			},
		},
	}
}

func (n *NodeWrapper) Annotations(annotations map[string]string) *NodeWrapper {
	maps.Copy(n.ObjectMeta.Annotations, annotations)
	return n
}

func (n *NodeWrapper) Name(name string) *NodeWrapper {
	n.ObjectMeta.Name = name
	return n
}

func (n *NodeWrapper) Labels(labels map[string]string) *NodeWrapper {
	maps.Copy(n.ObjectMeta.Labels, labels)
	return n
}

func (n *NodeWrapper) Allocatable(allocatable v1.ResourceList) *NodeWrapper {
	n.Status.Allocatable = allocatable.DeepCopy()
	return n
}

func (n *NodeWrapper) Capacity(capacity v1.ResourceList) *NodeWrapper {
	n.Status.Capacity = capacity.DeepCopy()
	return n
}

func (n *NodeWrapper) Obj() *v1.Node {
	return &n.Node
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

type ContainerWrapper struct {
	v1.Container
}

// NewContainer creates a new ContainerWrapper.
func NewContainer() *ContainerWrapper {
	return &ContainerWrapper{
		Container: v1.Container{
			Resources: v1.ResourceRequirements{},
		},
	}
}

func (c *ContainerWrapper) SetName(name string) *ContainerWrapper {
	c.Name = name
	return c
}

func (c *ContainerWrapper) SetImage(image string) *ContainerWrapper {
	c.Image = image
	return c
}

func (c *ContainerWrapper) Requests(req v1.ResourceList) *ContainerWrapper {
	c.Resources.Requests = req
	return c
}

func (c *ContainerWrapper) Limits(lim v1.ResourceList) *ContainerWrapper {
	c.Resources.Limits = lim
	return c
}

func (c *ContainerWrapper) Obj() *v1.Container {
	return &c.Container
}

// PodWrapper wraps a Kubernetes Pod.
type PodWrapper struct {
	v1.Pod
}

func MakePod() *PodWrapper {
	return &PodWrapper{
		Pod: v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Status: v1.PodStatus{},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{},
				},
				NodeSelector: map[string]string{},
			},
		},
	}
}

func (n *PodWrapper) Labels(labels map[string]string) *PodWrapper {
	maps.Copy(n.ObjectMeta.Labels, labels)
	return n
}

func (n *PodWrapper) Name(name string) *PodWrapper {
	n.ObjectMeta.Name = name
	if n.ObjectMeta.UID == "" {
		if n.ObjectMeta.Name != "" && n.ObjectMeta.Namespace != "" {
			n.ObjectMeta.UID = types.UID(fmt.Sprintf("%v-%v", n.ObjectMeta.Namespace, n.ObjectMeta.Name))
		}
	}
	return n
}

func (n *PodWrapper) Annotations(annons map[string]string) *PodWrapper {
	maps.Copy(n.ObjectMeta.Annotations, annons)
	return n
}

func (n *PodWrapper) PodPhase(podPhase v1.PodPhase) *PodWrapper {
	n.Status.Phase = podPhase
	return n
}

func (n *PodWrapper) NodeName(nodeName string) *PodWrapper {
	n.Spec.NodeName = nodeName
	return n
}

func (n *PodWrapper) GroupName(groupName string) *PodWrapper {
	if n.ObjectMeta.Annotations == nil {
		n.ObjectMeta.Annotations = map[string]string{}
	}
	n.ObjectMeta.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey] = groupName
	return n
}

func (n *PodWrapper) NodeSelector(selector map[string]string) *PodWrapper {
	maps.Copy(n.Spec.NodeSelector, selector)
	return n
}

func (n *PodWrapper) Namespace(namespace string) *PodWrapper {
	n.ObjectMeta.Namespace = namespace
	if n.ObjectMeta.UID == "" {
		if n.ObjectMeta.Name != "" && n.ObjectMeta.Namespace != "" {
			n.ObjectMeta.UID = types.UID(fmt.Sprintf("%v-%v", n.ObjectMeta.Namespace, n.ObjectMeta.Name))
		}
	}
	return n
}

func (n *PodWrapper) ResourceClaim(resourceClaims []v1.PodResourceClaim) *PodWrapper {
	n.Spec.ResourceClaims = resourceClaims
	return n
}

func (n *PodWrapper) ContainerClaimRequests(claimReq []v1.ResourceClaim) *PodWrapper {
	n.Spec.Containers[0].Resources.Claims = claimReq
	return n
}

func (n *PodWrapper) ResourceList(req v1.ResourceList) *PodWrapper {
	n.Spec.Containers[0].Resources.Requests = req
	return n
}

func (n *PodWrapper) PreEmptionPolicy(preemptionPolicy v1.PreemptionPolicy) *PodWrapper {
	n.Spec.PreemptionPolicy = &preemptionPolicy
	return n
}

func (n *PodWrapper) Priority(priority *int32) *PodWrapper {
	n.Spec.Priority = priority
	return n
}

func (n *PodWrapper) Volumes(volumes []v1.Volume) *PodWrapper {
	n.Spec.Volumes = volumes
	return n
}

func (n *PodWrapper) PersistentVolumeClaim(pvc *v1.PersistentVolumeClaim) *PodWrapper {
	if n.Spec.Volumes == nil {
		n.Spec.Volumes = make([]v1.Volume, 0)
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
	for _, volume := range n.Spec.Volumes {
		if volume.Name == newVolume.Name {
			exists = true
			break
		}
	}

	if !exists {
		n.Spec.Volumes = append(n.Spec.Volumes, newVolume)
	}

	n.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
		{
			Name:      pvc.Name,
			MountPath: "/data",
		},
	}
	return n
}

func (n *PodWrapper) Containers(containers []v1.Container) *PodWrapper {
	if containers != nil {
		n.Spec.Containers = containers
	}
	return n
}

func (n *PodWrapper) Container(container v1.Container) *PodWrapper {
	if n.Spec.Containers == nil {
		n.Spec.Containers = make([]v1.Container, 0)
	}
	n.Spec.Containers = append(n.Spec.Containers, container)
	return n
}

func (n *PodWrapper) Obj() *v1.Pod {
	return &n.Pod
}

// PVCWrapper wraps a Kubernetes PersistentVolumeClaim
type PVCWrapper struct {
	v1.PersistentVolumeClaim
}

func MakePVC() *PVCWrapper {
	return &PVCWrapper{
		PersistentVolumeClaim: v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "1",
			},
			Spec: v1.PersistentVolumeClaimSpec{
				Resources: v1.VolumeResourceRequirements{},
			},
		},
	}
}

func (pvcWrapper *PVCWrapper) Namespace(namespace string) *PVCWrapper {
	pvcWrapper.ObjectMeta.Namespace = namespace
	return pvcWrapper
}

func (pvcWrapper *PVCWrapper) Name(name string) *PVCWrapper {
	pvcWrapper.ObjectMeta.Name = name
	return pvcWrapper
}

func (pvcWrapper *PVCWrapper) StorageClassName(storageClassName string) *PVCWrapper {
	pvcWrapper.Spec.StorageClassName = &storageClassName
	return pvcWrapper
}

func (pvcWrapper *PVCWrapper) Resources(req v1.ResourceList) *PVCWrapper {
	pvcWrapper.Spec.Resources.Requests = req
	return pvcWrapper
}

func (pvcWrapper *PVCWrapper) Obj() *v1.PersistentVolumeClaim {
	return &pvcWrapper.PersistentVolumeClaim
}

// PVWrapper wraps a Kubernetes PersistentVolume
type PVWrapper struct {
	v1.PersistentVolume
}

func MakePV() *PVWrapper {
	return &PVWrapper{
		PersistentVolume: v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "1",
			},
			Spec: v1.PersistentVolumeSpec{},
			Status: v1.PersistentVolumeStatus{
				Phase: v1.VolumeAvailable,
			},
		},
	}
}

func (pvWrapper *PVWrapper) Name(name string) *PVWrapper {
	pvWrapper.ObjectMeta.Name = name
	return pvWrapper
}

func (pvWrapper *PVWrapper) StorageClassName(scName string) *PVWrapper {
	pvWrapper.Spec.StorageClassName = scName
	return pvWrapper
}

func (pvWrapper *PVWrapper) Capacity(capacity v1.ResourceList) *PVWrapper {
	pvWrapper.Spec.Capacity = capacity
	return pvWrapper
}

func (pvWrapper *PVWrapper) Obj() *v1.PersistentVolume {
	return &pvWrapper.PersistentVolume
}

// DeviceRequestWrapper wraps a Kubernetes DeviceRequest
type DeviceRequestWrapper struct {
	resourcev1beta1.DeviceRequest
}

func MakeDeviceRequest() *DeviceRequestWrapper {
	return &DeviceRequestWrapper{
		DeviceRequest: resourcev1beta1.DeviceRequest{
			AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount,
			Count:          1,
		},
	}
}

func (wrapper *DeviceRequestWrapper) SetName(name string) *DeviceRequestWrapper {
	wrapper.Name = name
	return wrapper
}

func (wrapper *DeviceRequestWrapper) SetDeviceClassName(name string) *DeviceRequestWrapper {
	wrapper.DeviceClassName = name
	return wrapper
}

func (wrapper *DeviceRequestWrapper) SetSelectors(selectors []resourcev1beta1.DeviceSelector) *DeviceRequestWrapper {
	wrapper.Selectors = selectors
	return wrapper
}

func (wrapper *DeviceRequestWrapper) SetAllocationMode(allocationMode *resourcev1beta1.DeviceAllocationMode) *DeviceRequestWrapper {
	if allocationMode != nil {
		wrapper.AllocationMode = *allocationMode
	}
	return wrapper
}

func (wrapper *DeviceRequestWrapper) SetCount(count *int64) *DeviceRequestWrapper {
	if wrapper.AllocationMode == resourcev1beta1.DeviceAllocationModeExactCount && count != nil {
		wrapper.Count = *count
	}
	return wrapper
}

func (wrapper *DeviceRequestWrapper) Obj() *resourcev1beta1.DeviceRequest {
	return &wrapper.DeviceRequest
}

// ResourceClaimWrapper wraps a Kubernetes ResourceClaim
type ResourceClaimWrapper struct {
	resourcev1beta1.ResourceClaim
}

func MakeResourceClaim() *ResourceClaimWrapper {
	return &ResourceClaimWrapper{
		ResourceClaim: resourcev1beta1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "1",
			},
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{},
			},
		},
	}
}

func (wrapper *ResourceClaimWrapper) Name(name string) *ResourceClaimWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}

func (wrapper *ResourceClaimWrapper) Namespace(namespace string) *ResourceClaimWrapper {
	wrapper.ObjectMeta.Namespace = namespace
	return wrapper
}

func (wrapper *ResourceClaimWrapper) Constraints(constraints []resourcev1beta1.DeviceConstraint) *ResourceClaimWrapper {
	wrapper.Spec.Devices.Constraints = constraints
	return wrapper
}

func (wrapper *ResourceClaimWrapper) Config(config []resourcev1beta1.DeviceClaimConfiguration) *ResourceClaimWrapper {
	if config != nil {
		wrapper.Spec.Devices.Config = config
	}
	return wrapper
}

func (wrapper *ResourceClaimWrapper) DeviceRequests(deviceRequests []resourcev1beta1.DeviceRequest) *ResourceClaimWrapper {
	wrapper.Spec.Devices.Requests = deviceRequests
	return wrapper
}

func (wrapper *ResourceClaimWrapper) Obj() *resourcev1beta1.ResourceClaim {
	return &wrapper.ResourceClaim
}

// DeviceClassWrapper wraps a Kubernetes DeviceClass for method chaining.
type DeviceClassWrapper struct {
	resourcev1beta1.DeviceClass
}

func MakeDeviceClass() *DeviceClassWrapper {
	return &DeviceClassWrapper{
		DeviceClass: resourcev1beta1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       resourcev1beta1.DeviceClassSpec{},
		},
	}
}

func (dcw *DeviceClassWrapper) Name(name string) *DeviceClassWrapper {
	dcw.ObjectMeta.Name = name
	return dcw
}

func (dcw *DeviceClassWrapper) Selectors(selectors []resourcev1beta1.DeviceSelector) *DeviceClassWrapper {
	dcw.Spec.Selectors = selectors
	return dcw
}

func (dcw *DeviceClassWrapper) Config(config []resourcev1beta1.DeviceClassConfiguration) *DeviceClassWrapper {
	if config != nil {
		dcw.Spec.Config = config
	}
	return dcw
}

func (dcw *DeviceClassWrapper) Obj() *resourcev1beta1.DeviceClass {
	return &dcw.DeviceClass
}

// DeviceWrapper wraps a Kubernetes Device for method chaining.
type DeviceWrapper struct {
	resourcev1beta1.Device
}

func MakeDevice() *DeviceWrapper {
	return &DeviceWrapper{
		Device: resourcev1beta1.Device{
			Basic: &resourcev1beta1.BasicDevice{},
		},
	}
}

func (dw *DeviceWrapper) SetName(name string) *DeviceWrapper {
	dw.Name = name
	return dw
}

func (dw *DeviceWrapper) Capacity(capacity map[resourcev1beta1.QualifiedName]resourcev1beta1.DeviceCapacity) *DeviceWrapper {
	dw.Basic.Capacity = capacity
	return dw
}

func (dw *DeviceWrapper) Attributes(attributes map[resourcev1beta1.QualifiedName]resourcev1beta1.DeviceAttribute) *DeviceWrapper {
	dw.Basic.Attributes = attributes
	return dw
}

func (dw *DeviceWrapper) Obj() *resourcev1beta1.Device {
	return &dw.Device
}

// ResourceSliceWrapper wraps a Kubernetes ResourceSlice for method chaining.
type ResourceSliceWrapper struct {
	resourcev1beta1.ResourceSlice
}

func MakeResourceSlice() *ResourceSliceWrapper {
	return &ResourceSliceWrapper{
		ResourceSlice: resourcev1beta1.ResourceSlice{
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       resourcev1beta1.ResourceSliceSpec{},
		},
	}
}

func (wrapper *ResourceSliceWrapper) Name(name string) *ResourceSliceWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}

func (wrapper *ResourceSliceWrapper) NodeName(nodeName string) *ResourceSliceWrapper {
	wrapper.Spec.NodeName = nodeName
	return wrapper
}

func (wrapper *ResourceSliceWrapper) Driver(driver string) *ResourceSliceWrapper {
	wrapper.Spec.Driver = driver
	return wrapper
}

func (wrapper *ResourceSliceWrapper) Pool(pool resourcev1beta1.ResourcePool) *ResourceSliceWrapper {
	wrapper.Spec.Pool = pool
	return wrapper
}

func (wrapper *ResourceSliceWrapper) Devices(devices []resourcev1beta1.Device) *ResourceSliceWrapper {
	wrapper.Spec.Devices = devices
	return wrapper
}

func (wrapper *ResourceSliceWrapper) Obj() *resourcev1beta1.ResourceSlice {
	return &wrapper.ResourceSlice
}

type StorageClassWrapper struct {
	storagev1.StorageClass
}

func MakeStorageClass() *StorageClassWrapper {
	return &StorageClassWrapper{
		StorageClass: storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{},
		},
	}
}

func (wrapper *StorageClassWrapper) Name(name string) *StorageClassWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}

func (wrapper *StorageClassWrapper) SetProvisioner(provisioner string) *StorageClassWrapper {
	wrapper.Provisioner = provisioner
	return wrapper
}

func (wrapper *StorageClassWrapper) SetVolumeBindingMode(volumeBindingMode storagev1.VolumeBindingMode) *StorageClassWrapper {
	wrapper.VolumeBindingMode = &volumeBindingMode
	return wrapper
}

func (wrapper *StorageClassWrapper) Obj() *storagev1.StorageClass {
	return &wrapper.StorageClass
}

type PodGroupWrapper struct {
	schedulingv1beta1.PodGroup
}

func MakePodGroup() *PodGroupWrapper {
	return &PodGroupWrapper{
		PodGroup: schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				NetworkTopology: &schedulingv1beta1.NetworkTopologySpec{},
			},
			Status: schedulingv1beta1.PodGroupStatus{},
		},
	}
}

func (wrapper *PodGroupWrapper) Name(name string) *PodGroupWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}
func (wrapper *PodGroupWrapper) Annotations(annos map[string]string) *PodGroupWrapper {
	maps.Copy(wrapper.ObjectMeta.Annotations, annos)
	return wrapper
}

func (wrapper *PodGroupWrapper) HyperNodeName(hyperNodeName string) *PodGroupWrapper {
	if wrapper.ObjectMeta.Annotations == nil {
		wrapper.ObjectMeta.Annotations = make(map[string]string)
	}
	wrapper.ObjectMeta.Annotations[api.JobAllocatedHyperNode] = hyperNodeName
	return wrapper
}

func (wrapper *PodGroupWrapper) Namespace(namespace string) *PodGroupWrapper {
	wrapper.ObjectMeta.Namespace = namespace
	return wrapper
}

func (wrapper *PodGroupWrapper) PriorityClassName(priorityClassName string) *PodGroupWrapper {
	wrapper.Spec.PriorityClassName = priorityClassName
	return wrapper
}

func (wrapper *PodGroupWrapper) Queue(queue string) *PodGroupWrapper {
	wrapper.Spec.Queue = queue
	return wrapper
}

func (wrapper *PodGroupWrapper) Mode(mode string) *PodGroupWrapper {
	wrapper.Spec.NetworkTopology.Mode = schedulingv1beta1.NetworkTopologyMode(mode)
	return wrapper
}

func (wrapper *PodGroupWrapper) HighestTierAllowed(highestTierAllowed int) *PodGroupWrapper {
	wrapper.Spec.NetworkTopology.HighestTierAllowed = &highestTierAllowed
	return wrapper
}

func (wrapper *PodGroupWrapper) MinMember(minMember int32) *PodGroupWrapper {
	wrapper.Spec.MinMember = minMember
	return wrapper
}

func (wrapper *PodGroupWrapper) MinTaskMember(minTaskMember map[string]int32) *PodGroupWrapper {
	wrapper.Spec.MinTaskMember = minTaskMember
	return wrapper
}

func (wrapper *PodGroupWrapper) MinResources(minResources v1.ResourceList) *PodGroupWrapper {
	if minResources != nil {
		copiedResources := minResources.DeepCopy()
		wrapper.Spec.MinResources = &copiedResources
	}
	return wrapper
}

func (wrapper *PodGroupWrapper) Phase(phase schedulingv1beta1.PodGroupPhase) *PodGroupWrapper {
	wrapper.Status.Phase = phase
	return wrapper
}

func (wrapper *PodGroupWrapper) Obj() *schedulingv1beta1.PodGroup {
	return &wrapper.PodGroup
}

type ResourceQuotaWrapper struct {
	v1.ResourceQuota
}

func MakeResourceQuota() *ResourceQuotaWrapper {
	return &ResourceQuotaWrapper{
		ResourceQuota: v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       v1.ResourceQuotaSpec{},
			Status:     v1.ResourceQuotaStatus{},
		},
	}
}

func (wrapper *ResourceQuotaWrapper) Name(name string) *ResourceQuotaWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}

func (wrapper *ResourceQuotaWrapper) Namespace(namespace string) *ResourceQuotaWrapper {
	wrapper.ObjectMeta.Namespace = namespace
	return wrapper
}

func (wrapper *ResourceQuotaWrapper) HardResourceLimit(hard v1.ResourceList) *ResourceQuotaWrapper {
	if hard != nil {
		wrapper.Spec.Hard = hard
		wrapper.Status.Hard = hard
	}
	return wrapper
}

func (wrapper *ResourceQuotaWrapper) Obj() *v1.ResourceQuota {
	return &wrapper.ResourceQuota
}

type QueueWrapper struct {
	schedulingv1beta1.Queue
}

func MakeQueue() *QueueWrapper {
	return &QueueWrapper{
		Queue: schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
			Spec:   schedulingv1beta1.QueueSpec{},
			Status: schedulingv1beta1.QueueStatus{},
		},
	}
}

func (wrapper *QueueWrapper) Name(name string) *QueueWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}

func (wrapper *QueueWrapper) Annotations(annos map[string]string) *QueueWrapper {
	if wrapper.ObjectMeta.Annotations == nil {
		wrapper.ObjectMeta.Annotations = make(map[string]string)
	}
	maps.Copy(wrapper.ObjectMeta.Annotations, annos)
	return wrapper
}
func (wrapper *QueueWrapper) Weight(weight int32) *QueueWrapper {
	wrapper.Spec.Weight = weight
	return wrapper
}

func (wrapper *QueueWrapper) Capability(cap v1.ResourceList) *QueueWrapper {
	if cap == nil {
		return wrapper
	}
	wrapper.Spec.Capability = cap.DeepCopy()
	return wrapper
}

func (wrapper *QueueWrapper) Deserved(deserved v1.ResourceList) *QueueWrapper {
	wrapper.Spec.Deserved = deserved
	return wrapper
}

func (wrapper *QueueWrapper) Priority(priority int32) *QueueWrapper {
	wrapper.Spec.Priority = priority
	return wrapper
}

func (wrapper *QueueWrapper) Parent(parent string) *QueueWrapper {
	wrapper.Spec.Parent = parent
	return wrapper
}

func (wrapper *QueueWrapper) State(state schedulingv1beta1.QueueState) *QueueWrapper {
	wrapper.Status.State = state
	return wrapper
}

func (wrapper *QueueWrapper) Obj() *schedulingv1beta1.Queue {
	return &wrapper.Queue
}

type PriorityClassWrapper struct {
	schedulingv1.PriorityClass
}

func MakePriorityClass() *PriorityClassWrapper {
	return &PriorityClassWrapper{
		PriorityClass: schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{},
		},
	}
}

func (wrapper *PriorityClassWrapper) Name(name string) *PriorityClassWrapper {
	wrapper.ObjectMeta.Name = name
	return wrapper
}

func (wrapper *PriorityClassWrapper) SetValue(value int32) *PriorityClassWrapper {
	wrapper.Value = value
	return wrapper
}

func (wrapper *PriorityClassWrapper) PreEmptionPolicy(preemptionPolicy v1.PreemptionPolicy) *PriorityClassWrapper {
	wrapper.PreemptionPolicy = &preemptionPolicy
	return wrapper
}

func (wrapper *PriorityClassWrapper) Obj() *schedulingv1.PriorityClass {
	return &wrapper.PriorityClass
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
