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
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	volumescheduling "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
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

// BuildPodWithPVC builts Pod object with pvc volume
func BuildPodWithPVC(namespace, name, nodename string, p v1.PodPhase, req v1.ResourceList, pvc *v1.PersistentVolumeClaim, groupName string, labels map[string]string, selector map[string]string) *v1.Pod {
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
			Resources: v1.ResourceRequirements{
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

// FakeBinder is used as fake binder
type FakeBinder struct {
	Binds   map[string]string
	Channel chan string
}

// Bind used by fake binder struct to bind pods
func (fb *FakeBinder) Bind(kubeClient *kubernetes.Clientset, tasks []*api.TaskInfo) ([]*api.TaskInfo, error) {
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
	volumeBinder volumescheduling.SchedulerVolumeBinder
	Actions      map[string][]string
}

// NewFakeVolumeBinder create fake volume binder with kubeclient
func NewFakeVolumeBinder(kubeClient kubernetes.Interface) *FakeVolumeBinder {
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	podInformer := informerFactory.Core().V1().Pods()
	pvcInformer := informerFactory.Core().V1().PersistentVolumeClaims()
	pvInformer := informerFactory.Core().V1().PersistentVolumes()
	scInformer := informerFactory.Storage().V1().StorageClasses()
	nodeInformer := informerFactory.Core().V1().Nodes()
	csiNodeInformer := informerFactory.Storage().V1().CSINodes()

	go podInformer.Informer().Run(context.TODO().Done())
	go pvcInformer.Informer().Run(context.TODO().Done())
	go pvInformer.Informer().Run(context.TODO().Done())
	go scInformer.Informer().Run(context.TODO().Done())
	go nodeInformer.Informer().Run(context.TODO().Done())
	go csiNodeInformer.Informer().Run(context.TODO().Done())

	cache.WaitForCacheSync(context.TODO().Done(), podInformer.Informer().HasSynced,
		pvcInformer.Informer().HasSynced,
		pvInformer.Informer().HasSynced,
		scInformer.Informer().HasSynced,
		nodeInformer.Informer().HasSynced,
		csiNodeInformer.Informer().HasSynced)
	return &FakeVolumeBinder{
		volumeBinder: volumescheduling.NewVolumeBinder(
			kubeClient,
			podInformer,
			nodeInformer,
			csiNodeInformer,
			pvcInformer,
			pvInformer,
			scInformer,
			nil,
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
	_, err := fvb.volumeBinder.AssumePodVolumes(task.Pod, hostname, podVolumes)

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
	boundClaims, claimsToBind, unboundClaimsImmediate, err := fvb.volumeBinder.GetPodVolumes(task.Pod)
	if err != nil {
		return nil, err
	}
	if len(unboundClaimsImmediate) > 0 {
		return nil, fmt.Errorf("pod has unbound immediate PersistentVolumeClaims")
	}

	podVolumes, reasons, err := fvb.volumeBinder.FindPodVolumes(task.Pod, boundClaims, claimsToBind, node)
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
