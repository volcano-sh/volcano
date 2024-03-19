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
	"context"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	volumescheduling "volcano.sh/volcano/pkg/scheduler/capabilities/volumebinding"
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

// BuildDynamicPVC create pv pvc and storage class
func BuildDynamicPVC(namespace, name string, req v1.ResourceList) (*v1.PersistentVolumeClaim, *v1.PersistentVolume, *storagev1.StorageClass) {
	tmp := v1.PersistentVolumeReclaimDelete
	tmp2 := storagev1.VolumeBindingWaitForFirstConsumer
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			ResourceVersion: "1",
			Name:            name,
		},
		Provisioner:       name,
		ReclaimPolicy:     &tmp,
		VolumeBindingMode: &tmp2,
	}
	tmp3 := v1.PersistentVolumeFilesystem
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			ResourceVersion: "1",
			Namespace:       namespace,
			Name:            name,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			Resources: v1.VolumeResourceRequirements{
				Requests: req,
			},
			StorageClassName: &sc.Name,
			VolumeMode:       &tmp3,
		},
	}
	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			ResourceVersion: "1",
			Name:            name,
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName: sc.Name,
			Capacity:         req,
			VolumeMode:       &tmp3,
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
		},
		Status: v1.PersistentVolumeStatus{
			Phase: v1.VolumeAvailable,
		},
	}
	return pvc, pv, sc
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

// BuildPodGroup return podgroup
func BuildPodGroupWithPrio(name, ns, queue string, minMember int32, taskMinMember map[string]int32, status schedulingv1beta1.PodGroupPhase, prioName string) *schedulingv1beta1.PodGroup {
	pg := BuildPodGroup(name, ns, queue, minMember, taskMinMember, status)
	pg.Spec.PriorityClassName = prioName
	return pg
}

///////////// function to build queue  ///////////////////

// BuildQueue return a scheduling Queue
func BuildQueue(qname string, weight int32, cap v1.ResourceList) *schedulingv1beta1.Queue {
	return &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: qname,
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:     weight,
			Capability: cap,
		},
	}
}

// BuildQueueWithAnnos return a Queue with annotations
func BuildQueueWithAnnos(qname string, weight int32, cap v1.ResourceList, annos map[string]string) *schedulingv1beta1.Queue {
	queue := BuildQueue(qname, weight, cap)
	queue.ObjectMeta.Annotations = annos
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

// FakeBinder is used as fake binder
type FakeBinder struct {
	sync.Mutex
	Binds   map[string]string
	Channel chan string
}

// Bind used by fake binder struct to bind pods
func (fb *FakeBinder) Bind(kubeClient kubernetes.Interface, tasks []*api.TaskInfo) ([]*api.TaskInfo, error) {
	fb.Lock()
	defer fb.Unlock()
	for _, p := range tasks {
		key := fmt.Sprintf("%v/%v", p.Namespace, p.Name)
		fb.Binds[key] = p.NodeName
		fb.Channel <- key // need to wait binding pod because Bind process is asynchronous
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

// UpdateQueueStatus do fake empty update
func (ftsu *FakeStatusUpdater) UpdateQueueStatus(queue *api.QueueInfo) error {
	return nil
}

// FakeVolumeBinder is used as fake volume binder
type FakeVolumeBinder struct {
	volumeBinder volumescheduling.SchedulerVolumeBinder
	Actions      map[string][]string
}

// NewFakeVolumeBinder create fake volume binder with kubeclient
func NewFakeVolumeBinder(kubeClient kubernetes.Interface) *FakeVolumeBinder {
	logger := klog.FromContext(context.TODO())
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	podInformer := informerFactory.Core().V1().Pods()
	pvcInformer := informerFactory.Core().V1().PersistentVolumeClaims()
	pvInformer := informerFactory.Core().V1().PersistentVolumes()
	scInformer := informerFactory.Storage().V1().StorageClasses()
	nodeInformer := informerFactory.Core().V1().Nodes()
	csiNodeInformer := informerFactory.Storage().V1().CSINodes()
	csiDriverInformer := informerFactory.Storage().V1().CSIDrivers()
	csiStorageCapacityInformer := informerFactory.Storage().V1beta1().CSIStorageCapacities()

	go podInformer.Informer().Run(context.TODO().Done())
	go pvcInformer.Informer().Run(context.TODO().Done())
	go pvInformer.Informer().Run(context.TODO().Done())
	go scInformer.Informer().Run(context.TODO().Done())
	go nodeInformer.Informer().Run(context.TODO().Done())
	go csiNodeInformer.Informer().Run(context.TODO().Done())
	go csiDriverInformer.Informer().Run(context.TODO().Done())
	go csiStorageCapacityInformer.Informer().Run(context.TODO().Done())

	cache.WaitForCacheSync(context.TODO().Done(), podInformer.Informer().HasSynced,
		pvcInformer.Informer().HasSynced,
		pvInformer.Informer().HasSynced,
		scInformer.Informer().HasSynced,
		nodeInformer.Informer().HasSynced,
		csiNodeInformer.Informer().HasSynced,
		csiDriverInformer.Informer().HasSynced,
		csiStorageCapacityInformer.Informer().HasSynced)

	capacityCheck := &volumescheduling.CapacityCheck{
		CSIDriverInformer:          csiDriverInformer,
		CSIStorageCapacityInformer: csiStorageCapacityInformer,
	}
	return &FakeVolumeBinder{
		volumeBinder: volumescheduling.NewVolumeBinder(
			logger,
			kubeClient,
			podInformer,
			nodeInformer,
			csiNodeInformer,
			pvcInformer,
			pvInformer,
			scInformer,
			capacityCheck,
			30*time.Second,
		),
		Actions: make(map[string][]string),
	}
}

// AllocateVolumes is a empty function
func (fvb *FakeVolumeBinder) AllocateVolumes(task *api.TaskInfo, hostname string, podVolumes *volumescheduling.PodVolumes) error {
	if fvb.volumeBinder == nil {
		return nil
	}
	logger := klog.FromContext(context.TODO())
	_, err := fvb.volumeBinder.AssumePodVolumes(logger, task.Pod, hostname, podVolumes)

	key := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
	fvb.Actions[key] = append(fvb.Actions[key], "AllocateVolumes")
	return err
}

// BindVolumes is a empty function
func (fvb *FakeVolumeBinder) BindVolumes(task *api.TaskInfo, podVolumes *volumescheduling.PodVolumes) error {
	if fvb.volumeBinder == nil {
		return nil
	}

	key := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
	if len(podVolumes.DynamicProvisions) > 0 {
		fvb.Actions[key] = append(fvb.Actions[key], "DynamicProvisions")
	}
	if len(podVolumes.StaticBindings) > 0 {
		fvb.Actions[key] = append(fvb.Actions[key], "StaticBindings")
	}
	return nil
}

// GetPodVolumes is a empty function
func (fvb *FakeVolumeBinder) GetPodVolumes(task *api.TaskInfo, node *v1.Node) (*volumescheduling.PodVolumes, error) {
	if fvb.volumeBinder == nil {
		return nil, nil
	}
	key := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
	fvb.Actions[key] = []string{"GetPodVolumes"}
	logger := klog.FromContext(context.TODO())
	podVolumeClaims, err := fvb.volumeBinder.GetPodVolumeClaims(logger, task.Pod)
	if err != nil {
		return nil, err
	}
	// if len(unboundClaimsImmediate) > 0 {
	// 	return nil, fmt.Errorf("pod has unbound immediate PersistentVolumeClaims")
	// }

	podVolumes, reasons, err := fvb.volumeBinder.FindPodVolumes(logger, task.Pod, podVolumeClaims, node)
	if err != nil {
		return nil, err
	} else if len(reasons) > 0 {
		return nil, fmt.Errorf("%v", reasons[0])
	}
	return podVolumes, err
}

// RevertVolumes is a empty function
func (fvb *FakeVolumeBinder) RevertVolumes(task *api.TaskInfo, podVolumes *volumescheduling.PodVolumes) {
	if fvb.volumeBinder == nil {
		return
	}
	key := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
	fvb.Actions[key] = append(fvb.Actions[key], "RevertVolumes")
	if podVolumes != nil {
		fvb.volumeBinder.RevertAssumedPodVolumes(podVolumes)
	}
}
