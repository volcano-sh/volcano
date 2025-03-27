/*
Copyright 2019 The Volcano Authors.

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

package podgroup

import (
	"slices"

	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	appinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	schedulinginformer "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/features"
)

func init() {
	framework.RegisterController(&pgcontroller{})
}

// pgcontroller the Podgroup pgcontroller type.
type pgcontroller struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	podInformer coreinformers.PodInformer
	pgInformer  schedulinginformer.PodGroupInformer
	rsInformer  appinformers.ReplicaSetInformer
	stsInformer appinformers.StatefulSetInformer

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	// A store of pods
	podLister corelisters.PodLister

	// A store of podgroups
	pgLister schedulinglister.PodGroupLister

	queue workqueue.TypedRateLimitingInterface[podRequest]

	schedulerNames []string
	workers        uint32

	// To determine whether inherit owner's annotations for pods when create podgroup
	inheritOwnerAnnotations bool
}

func (pg *pgcontroller) Name() string {
	return "pg-controller"
}

// Initialize create new Podgroup Controller.
func (pg *pgcontroller) Initialize(opt *framework.ControllerOption) error {
	pg.kubeClient = opt.KubeClient
	pg.vcClient = opt.VolcanoClient
	pg.workers = opt.WorkerThreadsForPG

	pg.queue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[podRequest]())

	pg.schedulerNames = make([]string, len(opt.SchedulerNames))
	copy(pg.schedulerNames, opt.SchedulerNames)
	pg.inheritOwnerAnnotations = opt.InheritOwnerAnnotations

	pg.informerFactory = opt.SharedInformerFactory
	pg.podInformer = opt.SharedInformerFactory.Core().V1().Pods()
	pg.podLister = pg.podInformer.Lister()
	pg.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: pg.addPod,
	})

	factory := opt.VCSharedInformerFactory
	pg.vcInformerFactory = factory
	pg.pgInformer = factory.Scheduling().V1beta1().PodGroups()
	pg.pgLister = pg.pgInformer.Lister()

	if utilfeature.DefaultFeatureGate.Enabled(features.WorkLoadSupport) {
		pg.rsInformer = pg.informerFactory.Apps().V1().ReplicaSets()
		pg.rsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    pg.addReplicaSet,
			UpdateFunc: pg.updateReplicaSet,
		})

		pg.stsInformer = pg.informerFactory.Apps().V1().StatefulSets()
		pg.stsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    pg.addStatefulSet,
			UpdateFunc: pg.updateStatefulSet,
		})
	}
	return nil
}

// Run start NewPodgroupController.
func (pg *pgcontroller) Run(stopCh <-chan struct{}) {
	pg.informerFactory.Start(stopCh)
	pg.vcInformerFactory.Start(stopCh)

	for informerType, ok := range pg.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
		}
	}
	for informerType, ok := range pg.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	for i := 0; i < int(pg.workers); i++ {
		go wait.Until(pg.worker, 0, stopCh)
	}

	klog.Infof("PodgroupController is running ...... ")
}

func (pg *pgcontroller) worker() {
	for pg.processNextReq() {
	}
}

func (pg *pgcontroller) processNextReq() bool {
	req, shutdown := pg.queue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from queue")
		return false
	}

	defer pg.queue.Done(req)

	pod, err := pg.podLister.Pods(req.podNamespace).Get(req.podName)
	if err != nil {
		klog.Errorf("Failed to get pod by <%v> from cache: %v", req, err)
		return true
	}

	if !slices.Contains(pg.schedulerNames, pod.Spec.SchedulerName) {
		klog.V(5).Infof("pod %v/%v field SchedulerName is not matched", pod.Namespace, pod.Name)
		return true
	}

	if pod.Annotations != nil && pod.Annotations[scheduling.KubeGroupNameAnnotationKey] != "" {
		klog.V(5).Infof("pod %v/%v has created podgroup", pod.Namespace, pod.Name)
		return true
	}

	// normal pod use volcano
	klog.V(4).Infof("Try to create podgroup for pod %s/%s", pod.Namespace, pod.Name)
	if err := pg.createNormalPodPGIfNotExist(pod); err != nil {
		klog.Errorf("Failed to handle Pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		pg.queue.AddRateLimited(req)
		return true
	}

	// If no error, forget it.
	pg.queue.Forget(req)

	return true
}
