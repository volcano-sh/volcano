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
	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	kbinfoext "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinfo "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	kblister "github.com/kubernetes-sigs/kube-batch/pkg/client/listers/scheduling/v1alpha1"

	vkver "volcano.sh/volcano/pkg/client/clientset/versioned"
	vkscheme "volcano.sh/volcano/pkg/client/clientset/versioned/scheme"
	vkinfoext "volcano.sh/volcano/pkg/client/informers/externalversions"
	vkbatchinfo "volcano.sh/volcano/pkg/client/informers/externalversions/batch/v1alpha1"
	vkcoreinfo "volcano.sh/volcano/pkg/client/informers/externalversions/bus/v1alpha1"
	vkbatchlister "volcano.sh/volcano/pkg/client/listers/batch/v1alpha1"
	vkcorelister "volcano.sh/volcano/pkg/client/listers/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
	jobcache "volcano.sh/volcano/pkg/controllers/cache"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

// Controller the Job Controller type
type Controller struct {
	config      *rest.Config
	kubeClients *kubernetes.Clientset
	vkClients   *vkver.Clientset
	kbClients   *kbver.Clientset

	jobInformer     vkbatchinfo.JobInformer
	pgInformer      kbinfo.PodGroupInformer
	cmdInformer     vkcoreinfo.CommandInformer
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

	cmdLister vkcorelister.CommandLister
	cmdSynced func() bool

	// queue that need to sync up
	queue        workqueue.RateLimitingInterface
	commandQueue workqueue.RateLimitingInterface
	cache        jobcache.Cache
	//Job Event recorder
	recorder record.EventRecorder
}

// NewJobController create new Job Controller
func NewJobController(config *rest.Config) *Controller {

	kubeClients := kubernetes.NewForConfigOrDie(config)

	//Initialize event client
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClients.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(vkscheme.Scheme, v1.EventSource{Component: "vk-controller"})

	cc := &Controller{
		config:       config,
		kubeClients:  kubeClients,
		vkClients:    vkver.NewForConfigOrDie(config),
		kbClients:    kbver.NewForConfigOrDie(config),
		queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		commandQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		cache:        jobcache.New(),
		recorder:     recorder,
	}

	cc.jobInformer = vkinfoext.NewSharedInformerFactory(cc.vkClients, 0).Batch().V1alpha1().Jobs()
	cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: cc.addJob,
		// TODO: enable this until we find an appropriate way.
		// UpdateFunc: cc.updateJob,
		DeleteFunc: cc.deleteJob,
	})
	cc.jobLister = cc.jobInformer.Lister()
	cc.jobSynced = cc.jobInformer.Informer().HasSynced

	cc.cmdInformer = vkinfoext.NewSharedInformerFactory(cc.vkClients, 0).Bus().V1alpha1().Commands()
	cc.cmdInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: cc.addCommand,
	})
	cc.cmdLister = cc.cmdInformer.Lister()
	cc.cmdSynced = cc.cmdInformer.Informer().HasSynced

	cc.sharedInformers = informers.NewSharedInformerFactory(cc.kubeClients, 0)
	podInformer := cc.sharedInformers.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addPod,
		UpdateFunc: cc.updatePod,
		DeleteFunc: cc.deletePod,
	})

	cc.podLister = podInformer.Lister()
	cc.podSynced = podInformer.Informer().HasSynced

	pvcInformer := cc.sharedInformers.Core().V1().PersistentVolumeClaims()
	cc.pvcLister = pvcInformer.Lister()
	cc.pvcSynced = pvcInformer.Informer().HasSynced

	svcInformer := cc.sharedInformers.Core().V1().Services()
	cc.svcLister = svcInformer.Lister()
	cc.svcSynced = svcInformer.Informer().HasSynced

	cc.pgInformer = kbinfoext.NewSharedInformerFactory(cc.kbClients, 0).Scheduling().V1alpha1().PodGroups()
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

	job, err := cc.jobLister.Jobs(req.Namespace).Get(req.JobName)
	if err != nil {
		glog.Errorf("Failed to get job by <%v>, maybe it is deleted", req.Namespace, req.JobName)
		return true
	}

	// overwrite the job to prevent directly modify underlying cache.
	jobInfo.Job = job.DeepCopy()

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
