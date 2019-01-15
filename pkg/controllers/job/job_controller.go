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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	kbinfoext "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinfo "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	kblister "github.com/kubernetes-sigs/kube-batch/pkg/client/listers/scheduling/v1alpha1"

	vkapi "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
	"hpw.cloud/volcano/pkg/client/clientset/versioned"
	vkinfoext "hpw.cloud/volcano/pkg/client/informers/externalversions"
	vkinfo "hpw.cloud/volcano/pkg/client/informers/externalversions/batch/v1alpha1"
	vklister "hpw.cloud/volcano/pkg/client/listers/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/controllers/job/state"
)

// Controller the Job Controller type
type Controller struct {
	config      *rest.Config
	kubeClients *kubernetes.Clientset
	vkClients   *versioned.Clientset
	kbClients   *kbver.Clientset

	jobInformer vkinfo.JobInformer
	podInformer coreinformers.PodInformer
	pgInformer  kbinfo.PodGroupInformer
	svcInformer coreinformers.ServiceInformer

	// A store of jobs
	jobLister vklister.JobLister
	jobSynced func() bool

	// A store of pods, populated by the podController
	podLister corelisters.PodLister
	podSynced func() bool

	// A store of pods, populated by the podController
	pgLister kblister.PodGroupLister
	pgSynced func() bool

	svcLister corelisters.ServiceLister
	svcSynced func() bool

	// eventQueue that need to sync up
	eventQueue *cache.FIFO
}

// NewJobController create new Job Controller
func NewJobController(config *rest.Config) *Controller {
	cc := &Controller{
		config:      config,
		kubeClients: kubernetes.NewForConfigOrDie(config),
		vkClients:   versioned.NewForConfigOrDie(config),
		kbClients:   kbver.NewForConfigOrDie(config),
		eventQueue:  cache.NewFIFO(eventKey),
	}

	cc.jobInformer = vkinfoext.NewSharedInformerFactory(cc.vkClients, 0).Batch().V1alpha1().Jobs()
	cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addJob,
		UpdateFunc: cc.updateJob,
		DeleteFunc: cc.deleteJob,
	})
	cc.jobLister = cc.jobInformer.Lister()
	cc.jobSynced = cc.jobInformer.Informer().HasSynced

	cc.podInformer = informers.NewSharedInformerFactory(cc.kubeClients, 0).Core().V1().Pods()

	cc.podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.Pod:
				return helpers.ControlledByJob(t)
			case cache.DeletedFinalStateUnknown:
				if pod, ok := t.Obj.(*v1.Pod); ok {
					return helpers.ControlledByJob(pod)
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

	cc.svcInformer = informers.NewSharedInformerFactory(cc.kubeClients, 0).Core().V1().Services()
	cc.svcLister = cc.svcInformer.Lister()
	cc.svcSynced = cc.svcInformer.Informer().HasSynced

	cc.pgInformer = kbinfoext.NewSharedInformerFactory(cc.kbClients, 0).Scheduling().V1alpha1().PodGroups()
	cc.pgLister = cc.pgInformer.Lister()
	cc.pgSynced = cc.pgInformer.Informer().HasSynced

	// Register actions
	actionFns := map[vkapi.Action]state.ActionFn{
		vkapi.ResumeJobAction:    cc.resumeJob,
		vkapi.SyncJobAction:      cc.syncJob,
		vkapi.AbortJobAction:     cc.abortJob,
		vkapi.TerminateJobAction: cc.terminateJob,
		vkapi.RestartJobAction:   cc.restartJob,
		vkapi.RestartTaskAction:  cc.syncJob,
	}
	state.RegisterActions(actionFns)

	return cc
}

// Run start JobController
func (cc *Controller) Run(stopCh <-chan struct{}) {
	go cc.jobInformer.Informer().Run(stopCh)
	go cc.podInformer.Informer().Run(stopCh)
	go cc.pgInformer.Informer().Run(stopCh)
	go cc.svcInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.jobSynced, cc.podSynced, cc.pgSynced, cc.svcSynced)

	go wait.Until(cc.worker, 0, stopCh)

	glog.Infof("JobController is running ...... ")
}

func (cc *Controller) worker() {
	obj := cache.Pop(cc.eventQueue)
	if obj == nil {
		glog.Errorf("Fail to pop item from eventQueue")
	}

	req := obj.(*state.Request)
	if req.Job == nil {
		if req.Pod == nil {
			glog.Errorf("Empty data for request %v", req)
			return
		}
		jobs, err := cc.jobLister.List(labels.Everything())
		if err != nil {
			glog.Errorf("Failed to list Jobs for Pod %v/%v", req.Pod.Namespace, req.Pod.Name)
		}

		// TODO(k82cn): select by UID instead of loop
		ctl := helpers.GetController(req.Pod)
		for _, j := range jobs {
			if j.UID == ctl {
				req.Job = j
				break
			}
		}
	}

	if req.Job == nil {
		glog.Errorf("No Job for request %v from pod %v/%v",
			req.Event, req.Pod.Namespace, req.Pod.Name)
		return
	}

	st := state.NewState(req)
	if err := st.Execute(); err != nil {
		glog.Errorf("Failed to handle Job %s/%s: %v",
			req.Job.Namespace, req.Job.Name, err)
		// If any error, requeue it.
		// TODO(k82cn): replace with RateLimteQueue
		cc.eventQueue.Add(req)
	}
}
