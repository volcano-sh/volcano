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
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"
	lwsinformerfactory "sigs.k8s.io/lws/client-go/informers/externalversions"
	lwslister "sigs.k8s.io/lws/client-go/listers/leaderworkerset/v1"

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

	informerFactory    informers.SharedInformerFactory
	vcInformerFactory  vcinformer.SharedInformerFactory
	lwsInformerFactory lwsinformerfactory.SharedInformerFactory

	// A store of pods
	podLister corelisters.PodLister
	podSynced func() bool

	// A store of podgroups
	pgLister schedulinglister.PodGroupLister
	pgSynced func() bool

	// A store of replicaset
	rsSynced func() bool

	lwsLister lwslister.LeaderWorkerSetLister

	podQueue workqueue.TypedRateLimitingInterface[resourceRequest]
	lwsQueue workqueue.TypedRateLimitingInterface[resourceRequest]

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

	pg.podQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[resourceRequest]())

	pg.schedulerNames = make([]string, len(opt.SchedulerNames))
	copy(pg.schedulerNames, opt.SchedulerNames)
	pg.inheritOwnerAnnotations = opt.InheritOwnerAnnotations

	pg.informerFactory = opt.SharedInformerFactory
	pg.podInformer = opt.SharedInformerFactory.Core().V1().Pods()
	pg.podLister = pg.podInformer.Lister()
	pg.podSynced = pg.podInformer.Informer().HasSynced
	pg.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: pg.addPod,
	})

	factory := opt.VCSharedInformerFactory
	pg.vcInformerFactory = factory
	pg.pgInformer = factory.Scheduling().V1beta1().PodGroups()
	pg.pgLister = pg.pgInformer.Lister()
	pg.pgSynced = pg.pgInformer.Informer().HasSynced

	if utilfeature.DefaultFeatureGate.Enabled(features.WorkLoadSupport) {
		pg.rsInformer = pg.informerFactory.Apps().V1().ReplicaSets()
		pg.rsSynced = pg.rsInformer.Informer().HasSynced
		pg.rsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    pg.addReplicaSet,
			UpdateFunc: pg.updateReplicaSet,
		})
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.LeaderWorkerSetSupport) {
		pg.lwsInformerFactory = opt.LWSSharedInformerFactory
		pg.lwsQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[resourceRequest]())
		lwsInformer := pg.lwsInformerFactory.Leaderworkerset().V1().LeaderWorkerSets()
		pg.lwsLister = lwsInformer.Lister()
		lwsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    pg.addLeaderWorkerSet,
			UpdateFunc: pg.updateLeaderWorkerSet,
		})
	}

	return nil
}

// Run start NewPodgroupController.
func (pg *pgcontroller) Run(stopCh <-chan struct{}) {
	pg.informerFactory.Start(stopCh)
	pg.vcInformerFactory.Start(stopCh)

	if utilfeature.DefaultFeatureGate.Enabled(features.LeaderWorkerSetSupport) {
		pg.lwsInformerFactory.Start(stopCh)
		for informerType, ok := range pg.lwsInformerFactory.WaitForCacheSync(stopCh) {
			if !ok {
				klog.Errorf("caches failed to sync: %v", informerType)
				return
			}
		}
	}

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
		go wait.Until(pg.podWorker, 0, stopCh)

		if utilfeature.DefaultFeatureGate.Enabled(features.LeaderWorkerSetSupport) {
			go wait.Until(pg.lwsWorker, 0, stopCh)
		}
	}

	klog.Infof("PodgroupController is running ...... ")
}

func (pg *pgcontroller) podWorker() {
	for pg.processNextPod() {
	}
}

func (pg *pgcontroller) processNextPod() bool {
	req, shutdown := pg.podQueue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from podQueue")
		return false
	}

	defer pg.podQueue.Done(req)

	pod, err := pg.podLister.Pods(req.Namespace).Get(req.Name)
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

	// The pods created by the Leader/Worker statefulset do not need to enter the createNormalPodPGIfNotExist to create a podgroup,
	// but instead, the podgroup controller actively creates podgroups through list/watch the LeaderWorkerSets
	if utilfeature.DefaultFeatureGate.Enabled(features.LeaderWorkerSetSupport) && pod.Labels != nil && pod.Labels[lwsv1.SetNameLabelKey] != "" {
		if err = pg.updatePodAnnotations(pod, fmt.Sprintf("%s-%s", pod.Labels[lwsv1.SetNameLabelKey], pod.Labels[lwsv1.GroupIndexLabelKey])); err != nil {
			klog.Errorf("failed to patch podgroup annotation for LeaderWorkerSet pod %s/%s,", pod.Namespace, pod.Name)
			pg.podQueue.AddRateLimited(req)
		}
		return true
	}

	// normal pod use volcano
	klog.V(4).Infof("Try to create podgroup for pod %s/%s", pod.Namespace, pod.Name)
	if err := pg.createNormalPodPGIfNotExist(pod); err != nil {
		klog.Errorf("Failed to handle Pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		pg.podQueue.AddRateLimited(req)
		return true
	}

	// If no error, forget it.
	pg.podQueue.Forget(req)

	return true
}

func (pg *pgcontroller) lwsWorker() {
	for pg.processNextLeaderWorkerSet() {
	}
}

func (pg *pgcontroller) processNextLeaderWorkerSet() bool {
	req, shutdown := pg.lwsQueue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from lwsQueue")
		return false
	}

	defer pg.lwsQueue.Done(req)

	err := pg.syncLeaderWorkerSet(req)
	if err == nil {
		pg.lwsQueue.Forget(req)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("failed to proccess LeaderWorkerSet %s/%s: %v", req.Namespace, req.Name, err))
	pg.lwsQueue.AddRateLimited(req)

	return true
}

func (pg *pgcontroller) syncLeaderWorkerSet(req resourceRequest) error {
	lws, err := pg.lwsLister.LeaderWorkerSets(req.Namespace).Get(req.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	err = pg.syncLeaderWorkerSetPodGroups(lws)
	if err != nil {
		klog.Errorf("Failed to create/update PodGroups for LeaderWorkerSet %s/%s: %v", req.Namespace, req.Name, err)
		return err
	}

	return nil
}
