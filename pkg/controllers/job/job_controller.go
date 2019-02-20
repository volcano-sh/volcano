/*
Copyright 2017 The Volcano Authors.

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

package job

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	kbinfoext "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinfo "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	kblister "github.com/kubernetes-sigs/kube-batch/pkg/client/listers/scheduling/v1alpha1"

	v1corev1 "hpw.cloud/volcano/pkg/apis/bus/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
	vkver "hpw.cloud/volcano/pkg/client/clientset/versioned"
	vkinfoext "hpw.cloud/volcano/pkg/client/informers/externalversions"
	vkbatchinfo "hpw.cloud/volcano/pkg/client/informers/externalversions/batch/v1alpha1"
	vkcoreinfo "hpw.cloud/volcano/pkg/client/informers/externalversions/bus/v1alpha1"
	vkbatchlister "hpw.cloud/volcano/pkg/client/listers/batch/v1alpha1"
	vkcorelister "hpw.cloud/volcano/pkg/client/listers/bus/v1alpha1"
	"hpw.cloud/volcano/pkg/controllers/job/apis"
	jobcache "hpw.cloud/volcano/pkg/controllers/job/cache"
	"hpw.cloud/volcano/pkg/controllers/job/state"
)

// Controller the Job Controller type
type Controller struct {
	config      *rest.Config
	kubeClients *kubernetes.Clientset
	vkClients   *vkver.Clientset
	kbClients   *kbver.Clientset

	jobInformer vkbatchinfo.JobInformer
	podInformer coreinformers.PodInformer
	pvcInformer coreinformers.PersistentVolumeClaimInformer
	pgInformer  kbinfo.PodGroupInformer
	svcInformer coreinformers.ServiceInformer
	cmdInformer vkcoreinfo.CommandInformer

	// A store of jobs
	jobLister vkbatchlister.JobLister
	jobSynced func() bool

	// A store of pods
	podLister corelisters.PodLister
	podSynced func() bool

	pvcLister corelisters.PersistentVolumeClaimLister
	pvcSynced func() bool

	// A store of podgroups
	pgLister kblister.PodGroupLister
	pgSynced func() bool

	// A store of service
	svcLister corelisters.ServiceLister
	svcSynced func() bool

	cmdLister vkcorelister.CommandLister
	cmdSynced func() bool

	// queue that need to sync up
	queue workqueue.RateLimitingInterface
	cache jobcache.Cache
}

// NewJobController create new Job Controller
func NewJobController(config *rest.Config) *Controller {
	cc := &Controller{
		config:      config,
		kubeClients: kubernetes.NewForConfigOrDie(config),
		vkClients:   vkver.NewForConfigOrDie(config),
		kbClients:   kbver.NewForConfigOrDie(config),
		queue:       workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		cache:       jobcache.New(),
	}

	cc.jobInformer = vkinfoext.NewSharedInformerFactory(cc.vkClients, 0).Batch().V1alpha1().Jobs()
	cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addJob,
		UpdateFunc: cc.updateJob,
		DeleteFunc: cc.deleteJob,
	})
	cc.jobLister = cc.jobInformer.Lister()
	cc.jobSynced = cc.jobInformer.Informer().HasSynced

	cc.cmdInformer = vkinfoext.NewSharedInformerFactory(cc.vkClients, 0).Bus().V1alpha1().Commands()
	cc.cmdInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1corev1.Command:
				return helpers.ControlledBy(t, helpers.JobKind)
			case cache.DeletedFinalStateUnknown:
				if cmd, ok := t.Obj.(*v1corev1.Command); ok {
					return helpers.ControlledBy(cmd, helpers.JobKind)
				}
				runtime.HandleError(fmt.Errorf("unable to convert object %T to *v1.Pod", obj))
				return false
			default:
				runtime.HandleError(fmt.Errorf("unable to handle object %T", obj))
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: cc.addCommand,
		},
	})
	cc.cmdLister = cc.cmdInformer.Lister()
	cc.cmdSynced = cc.cmdInformer.Informer().HasSynced

	cc.podInformer = informers.NewSharedInformerFactory(cc.kubeClients, 0).Core().V1().Pods()

	cc.podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.Pod:
				return helpers.ControlledBy(t, helpers.JobKind)
			case cache.DeletedFinalStateUnknown:
				if pod, ok := t.Obj.(*v1.Pod); ok {
					return helpers.ControlledBy(pod, helpers.JobKind)
				}
				runtime.HandleError(fmt.Errorf("unable to convert object %T to *v1.Pod", obj))
				return false
			default:
				runtime.HandleError(fmt.Errorf("unable to handle object %T", obj))
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    cc.addPod,
			UpdateFunc: cc.updatePod,
			DeleteFunc: cc.deletePod,
		},
	})

	cc.podLister = cc.podInformer.Lister()
	cc.podSynced = cc.podInformer.Informer().HasSynced

	cc.pvcInformer = informers.NewSharedInformerFactory(cc.kubeClients, 0).Core().V1().PersistentVolumeClaims()
	cc.pvcLister = cc.pvcInformer.Lister()
	cc.pvcSynced = cc.pvcInformer.Informer().HasSynced

	cc.svcInformer = informers.NewSharedInformerFactory(cc.kubeClients, 0).Core().V1().Services()
	cc.svcLister = cc.svcInformer.Lister()
	cc.svcSynced = cc.svcInformer.Informer().HasSynced

	cc.pgInformer = kbinfoext.NewSharedInformerFactory(cc.kbClients, 0).Scheduling().V1alpha1().PodGroups()
	cc.pgLister = cc.pgInformer.Lister()
	cc.pgSynced = cc.pgInformer.Informer().HasSynced

	// Register actions
	state.SyncJob = cc.syncJob
	state.KillJob = cc.killJob

	return cc
}

// Run start JobController
func (cc *Controller) Run(stopCh <-chan struct{}) {
	go cc.jobInformer.Informer().Run(stopCh)
	go cc.podInformer.Informer().Run(stopCh)
	go cc.pvcInformer.Informer().Run(stopCh)
	go cc.pgInformer.Informer().Run(stopCh)
	go cc.svcInformer.Informer().Run(stopCh)
	go cc.cmdInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.jobSynced, cc.podSynced, cc.pgSynced,
		cc.svcSynced, cc.cmdSynced, cc.pvcSynced)

	go wait.Until(cc.worker, 0, stopCh)

	go cc.cache.Run(stopCh)

	glog.Infof("JobController is running ...... ")
}

func (cc *Controller) worker() {
	obj, shutdown := cc.queue.Get()
	if shutdown {
		glog.Errorf("Fail to pop item from queue")
		return
	}

	req := obj.(apis.Request)
	defer cc.queue.Done(req)

	glog.V(3).Infof("Try to handle request <%v>", req)

	jobInfo, err := cc.cache.Get(jobcache.JobKeyByReq(&req))
	if err != nil {
		// TODO(k82cn): ignore not-ready error.
		glog.Errorf("Failed to get job by <%v> from cache: %v", req, err)
		return
	}

	st := state.NewState(jobInfo)
	if st == nil {
		glog.Errorf("Invalid state <%s> of Job <%v/%v>",
			jobInfo.Job.Status.State, jobInfo.Job.Namespace, jobInfo.Job.Name)
		return
	}

	action := applyPolicies(jobInfo.Job, &req)
	glog.V(3).Infof("Execute <%v> on Job <%s/%s> in <%s> by <%T>.",
		action, req.Namespace, req.JobName, jobInfo.Job.Status.State.Phase, st)

	if err := st.Execute(action); err != nil {
		glog.Errorf("Failed to handle Job <%s/%s>: %v",
			jobInfo.Job.Namespace, jobInfo.Job.Name, err)
		// If any error, requeue it.
		cc.queue.AddRateLimited(req)
		return
	}

	// If no error, forget it.
	cc.queue.Forget(req)
}
