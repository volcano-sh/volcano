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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	scheduling "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	vcclientset "volcano.sh/volcano/pkg/client/clientset/versioned"
	informerfactory "volcano.sh/volcano/pkg/client/informers/externalversions"
	schedulinginformer "volcano.sh/volcano/pkg/client/informers/externalversions/scheduling/v1beta1"
	schedulinglister "volcano.sh/volcano/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/framework"
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

	// A store of pods
	podLister corelisters.PodLister
	podSynced func() bool

	// A store of podgroups
	pgLister schedulinglister.PodGroupLister
	pgSynced func() bool

	queue workqueue.RateLimitingInterface
}

func (pg *pgcontroller) Name() string {
	return "pg-controller"
}

// Initialize create new Podgroup Controller.
func (pg *pgcontroller) Initialize(opt *framework.ControllerOption) error {

	pg.kubeClient = opt.KubeClient
	pg.vcClient = opt.VolcanoClient

	pg.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	pg.podInformer = opt.SharedInformerFactory.Core().V1().Pods()
	pg.podLister = pg.podInformer.Lister()
	pg.podSynced = pg.podInformer.Informer().HasSynced
	pg.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch v := obj.(type) {
				case *v1.Pod:
					if v.Spec.SchedulerName == opt.SchedulerName &&
						(v.Annotations == nil || v.Annotations[scheduling.KubeGroupNameAnnotationKey] == "") {
						return true
					}
					return false
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: pg.addPod,
			},
		})

	pg.pgInformer = informerfactory.NewSharedInformerFactory(pg.vcClient, 0).Scheduling().V1beta1().PodGroups()
	pg.pgLister = pg.pgInformer.Lister()
	pg.pgSynced = pg.pgInformer.Informer().HasSynced

	return nil
}

// Run start NewPodgroupController.
func (pg *pgcontroller) Run(stopCh <-chan struct{}) {
	go pg.podInformer.Informer().Run(stopCh)
	go pg.pgInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, pg.podSynced, pg.pgSynced)

	go wait.Until(pg.worker, 0, stopCh)

	klog.Infof("PodgroupController is running ...... ")
}

func (pg *pgcontroller) worker() {
	for pg.processNextReq() {
	}
}

func (pg *pgcontroller) processNextReq() bool {
	obj, shutdown := pg.queue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from queue")
		return false
	}

	req := obj.(podRequest)
	defer pg.queue.Done(req)

	pod, err := pg.podLister.Pods(req.podNamespace).Get(req.podName)
	if err != nil {
		klog.Errorf("Failed to get pod by <%v> from cache: %v", req, err)
		return true
	}

	// normal pod use volcano
	if err := pg.createNormalPodPGIfNotExist(pod); err != nil {
		klog.Errorf("Failed to handle Pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		pg.queue.AddRateLimited(req)
		return true
	}

	// If no error, forget it.
	pg.queue.Forget(req)

	return true
}
