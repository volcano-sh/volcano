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

package reservation

import (
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	batchinformer "volcano.sh/apis/pkg/client/informers/externalversions/batch/v1alpha1"
	schedulinginformers "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	batchlister "volcano.sh/apis/pkg/client/listers/batch/v1alpha1"
	schedulinglisters "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"

	"k8s.io/client-go/kubernetes"

	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func init() {
	framework.RegisterController(&reservationcontroller{})
}

type reservationcontroller struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	reservationInformer batchinformer.ReservationInformer
	queueInformer       schedulinginformers.QueueInformer
	pgInformer          schedulinginformers.PodGroupInformer

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	// A store of podgroups
	pgLister schedulinglisters.PodGroupLister
	pgSynced func() bool

	reservationLister batchlister.ReservationLister
	reservationSynced func() bool

	queueLister schedulinglisters.QueueLister
	queueSynced func() bool

	workers       uint32
	maxRequeueNum int

	// queue that need to sync up
	queueList []workqueue.TypedRateLimitingInterface[any]
}

func (rc *reservationcontroller) Name() string {
	return "reservation-controller"
}

func (rc *reservationcontroller) Initialize(opt *framework.ControllerOption) error {
	rc.kubeClient = opt.KubeClient
	rc.vcClient = opt.VolcanoClient

	sharedInformers := opt.SharedInformerFactory
	workers := opt.WorkerNum

	rc.informerFactory = sharedInformers
	rc.queueList = make([]workqueue.TypedRateLimitingInterface[any], workers)

	rc.workers = workers
	rc.maxRequeueNum = opt.MaxRequeueNum
	if rc.maxRequeueNum < 0 {
		rc.maxRequeueNum = -1
	}
	var i uint32
	for i = 0; i < workers; i++ {
		rc.queueList[i] = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	}

	factory := opt.VCSharedInformerFactory
	rc.vcInformerFactory = factory

	rc.reservationInformer = factory.Batch().V1alpha1().Reservations()
	rc.reservationLister = rc.reservationInformer.Lister()
	rc.reservationSynced = rc.reservationInformer.Informer().HasSynced

	rc.reservationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    rc.addReservation,
		UpdateFunc: rc.updateReservation,
		DeleteFunc: rc.deleteReservation,
	})

	rc.queueInformer = factory.Scheduling().V1beta1().Queues()
	rc.queueLister = rc.queueInformer.Lister()
	rc.queueSynced = rc.queueInformer.Informer().HasSynced

	rc.pgInformer = factory.Scheduling().V1beta1().PodGroups()
	rc.pgLister = rc.pgInformer.Lister()
	rc.pgSynced = rc.pgInformer.Informer().HasSynced

	return nil
}

func (rc *reservationcontroller) Run(stopCh <-chan struct{}) {
	klog.Infof("Starting reservation controller.")
	rc.informerFactory.Start(stopCh)
	rc.vcInformerFactory.Start(stopCh)

	for informerType, ok := range rc.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
		}
	}
	for informerType, ok := range rc.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	var i uint32
	for i = 0; i < rc.workers; i++ {
		go func(num uint32) {
			wait.Until(
				func() {
					rc.worker(num)
				},
				time.Second,
				stopCh)
		}(i)
	}

	klog.Infof("ReservationController is running ...... ")
}

func (rc *reservationcontroller) worker(i uint32) {
	klog.Infof("worker %d start ...... ", i)

	for rc.processNextReq(i) {
	}
}

func (rc *reservationcontroller) processNextReq(count uint32) bool {
	queue := rc.queueList[count]
	obj, shutdown := queue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from queue")
		return false
	}
	req := obj.(apis.Request)
	defer queue.Done(req)

	return true
}
