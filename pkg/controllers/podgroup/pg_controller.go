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
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
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
)

// Controller the Podgroup Controller type
type Controller struct {
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

// NewPodgroupController create new Podgroup Controller
func NewPodgroupController(
	kubeClient kubernetes.Interface,
	vcClient vcclientset.Interface,
	sharedInformers informers.SharedInformerFactory,
	schedulerName string,
) *Controller {
	cc := &Controller{
		kubeClient: kubeClient,
		vcClient:   vcClient,

		queue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	cc.podInformer = sharedInformers.Core().V1().Pods()
	cc.podLister = cc.podInformer.Lister()
	cc.podSynced = cc.podInformer.Informer().HasSynced
	cc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch obj.(type) {
				case *v1.Pod:
					pod := obj.(*v1.Pod)
					if pod.Spec.SchedulerName == schedulerName &&
						(pod.Annotations == nil || pod.Annotations[scheduling.KubeGroupNameAnnotationKey] == "") {
						return true
					}
					return false
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: cc.addPod,
			},
		})

	cc.pgInformer = informerfactory.NewSharedInformerFactory(cc.vcClient, 0).Scheduling().V1beta1().PodGroups()
	cc.pgLister = cc.pgInformer.Lister()
	cc.pgSynced = cc.pgInformer.Informer().HasSynced

	return cc
}

// Run start NewPodgroupController
func (cc *Controller) Run(stopCh <-chan struct{}) {
	go cc.podInformer.Informer().Run(stopCh)
	go cc.pgInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.podSynced, cc.pgSynced)

	go wait.Until(cc.worker, 0, stopCh)

	klog.Infof("PodgroupController is running ...... ")
}

func (cc *Controller) worker() {
	for cc.processNextReq() {
	}
}

func (cc *Controller) processNextReq() bool {
	obj, shutdown := cc.queue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from queue")
		return false
	}

	req := obj.(podRequest)
	defer cc.queue.Done(req)

	pod, err := cc.podLister.Pods(req.podNamespace).Get(req.podName)
	if err != nil {
		klog.Errorf("Failed to get pod by <%v> from cache: %v", req, err)
		return true
	}

	// normal pod use volcano
	if err := cc.createNormalPodPGIfNotExist(pod); err != nil {
		klog.Errorf("Failed to handle Pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		cc.queue.AddRateLimited(req)
		return true
	}

	// If no error, forget it.
	cc.queue.Forget(req)

	return true
}
