/*
Copyright 2017 The Kubernetes Authors.

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
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	v1corev1 "github.com/kubernetes-sigs/volcano/pkg/apis/bus/v1alpha1"
	"github.com/kubernetes-sigs/volcano/pkg/apis/helpers"
	vkclient "github.com/kubernetes-sigs/volcano/pkg/client/clientset/versioned"
	vkscheme "github.com/kubernetes-sigs/volcano/pkg/client/clientset/versioned/scheme"
	vkinformers "github.com/kubernetes-sigs/volcano/pkg/client/informers/externalversions"
	vkbatchinformer "github.com/kubernetes-sigs/volcano/pkg/client/informers/externalversions/batch/v1alpha1"
	vkbusinformer "github.com/kubernetes-sigs/volcano/pkg/client/informers/externalversions/bus/v1alpha1"
	kbinfo "github.com/kubernetes-sigs/volcano/pkg/client/informers/externalversions/scheduling/v1alpha1"
	vkbatchlister "github.com/kubernetes-sigs/volcano/pkg/client/listers/batch/v1alpha1"
	vkbuslister "github.com/kubernetes-sigs/volcano/pkg/client/listers/bus/v1alpha1"
	kblister "github.com/kubernetes-sigs/volcano/pkg/client/listers/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/volcano/pkg/controllers/apis"
	jobcache "github.com/kubernetes-sigs/volcano/pkg/controllers/cache"
	"github.com/kubernetes-sigs/volcano/pkg/controllers/job/state"
)

// Controller the Job Controller type
type Controller struct {
	config      *rest.Config
	kubeClients *kubernetes.Clientset
	vkClients   *vkclient.Clientset

	jobInformer     vkbatchinformer.JobInformer
	pgInformer      kbinfo.PodGroupInformer
	cmdInformer     vkbusinformer.CommandInformer
	sharedInformers informers.SharedInformerFactory

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

	cmdLister vkbuslister.CommandLister
	cmdSynced func() bool

	// queue that need to sync up
	queue        workqueue.RateLimitingInterface
	commandQueue workqueue.RateLimitingInterface
	cache        jobcache.Cache
	// Job Event recorder
	recorder record.EventRecorder
}

// NewJobController create new Job Controller
func NewJobController(config *rest.Config) *Controller {

	kubeClients := kubernetes.NewForConfigOrDie(config)

	// Initialize event client
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClients.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(vkscheme.Scheme, v1.EventSource{Component: "vk-controller"})

	cc := &Controller{
		config:       config,
		kubeClients:  kubeClients,
		vkClients:    vkclient.NewForConfigOrDie(config),
		queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		commandQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		cache:        jobcache.New(),
		recorder:     recorder,
	}

	cc.jobInformer = vkinformers.NewSharedInformerFactory(cc.vkClients, 0).Batch().V1alpha1().Jobs()
	cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: cc.addJob,
		// TODO: enable this until we find an appropriate way.
		// UpdateFunc: cc.updateJob,
		DeleteFunc: cc.deleteJob,
	})
	cc.jobLister = cc.jobInformer.Lister()
	cc.jobSynced = cc.jobInformer.Informer().HasSynced

	cc.cmdInformer = vkinformers.NewSharedInformerFactory(cc.vkClients, 0).Bus().V1alpha1().Commands()
	cc.cmdInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1corev1.Command:
				return helpers.ControlledBy(t, helpers.JobKind)
			case cache.DeletedFinalStateUnknown:
				if cmd, ok := t.Obj.(*v1corev1.Command); ok {
					return helpers.ControlledBy(cmd, helpers.JobKind)
				}
				runtime.HandleError(fmt.Errorf("unable to convert object %T to *v1.Command", obj))
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

	cc.sharedInformers = informers.NewSharedInformerFactory(cc.kubeClients, 0)
	podInformer := cc.sharedInformers.Core().V1().Pods()

	podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
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

	cc.podLister = podInformer.Lister()
	cc.podSynced = podInformer.Informer().HasSynced

	pvcInformer := cc.sharedInformers.Core().V1().PersistentVolumeClaims()
	cc.pvcLister = pvcInformer.Lister()
	cc.pvcSynced = pvcInformer.Informer().HasSynced

	svcInformer := cc.sharedInformers.Core().V1().Services()
	cc.svcLister = svcInformer.Lister()
	cc.svcSynced = svcInformer.Informer().HasSynced

	cc.pgInformer = vkinformers.NewSharedInformerFactory(cc.vkClients, 0).Scheduling().V1alpha1().PodGroups()
	cc.pgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: cc.updatePodGroup,
	})
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
	go cc.pgInformer.Informer().Run(stopCh)
	go cc.cmdInformer.Informer().Run(stopCh)
	go cc.sharedInformers.Start(stopCh)

	cache.WaitForCacheSync(stopCh, cc.jobSynced, cc.podSynced, cc.pgSynced,
		cc.svcSynced, cc.cmdSynced, cc.pvcSynced)

	go wait.Until(cc.handleCommands, 0, stopCh)
	go wait.Until(cc.worker, 0, stopCh)

	go cc.cache.Run(stopCh)

	glog.Infof("JobController is running ...... ")
}

func (cc *Controller) worker() {
	for cc.processNextReq() {
	}
}

func (cc *Controller) processNextReq() bool {
	obj, shutdown := cc.queue.Get()
	if shutdown {
		glog.Errorf("Fail to pop item from queue")
		return false
	}

	req := obj.(apis.Request)
	defer cc.queue.Done(req)

	glog.V(3).Infof("Try to handle request <%v>", req)

	jobInfo, err := cc.cache.Get(jobcache.JobKeyByReq(&req))
	if err != nil {
		// TODO(k82cn): ignore not-ready error.
		glog.Errorf("Failed to get job by <%v> from cache: %v", req, err)
		return true
	}

	st := state.NewState(jobInfo)
	if st == nil {
		glog.Errorf("Invalid state <%s> of Job <%v/%v>",
			jobInfo.Job.Status.State, jobInfo.Job.Namespace, jobInfo.Job.Name)
		return true
	}

	action := applyPolicies(jobInfo.Job, &req)
	glog.V(3).Infof("Execute <%v> on Job <%s/%s> in <%s> by <%T>.",
		action, req.Namespace, req.JobName, jobInfo.Job.Status.State.Phase, st)

	if err := st.Execute(action); err != nil {
		glog.Errorf("Failed to handle Job <%s/%s>: %v",
			jobInfo.Job.Namespace, jobInfo.Job.Name, err)
		// If any error, requeue it.
		cc.queue.AddRateLimited(req)
		return true
	}

	// If no error, forget it.
	cc.queue.Forget(req)

	return true
}
