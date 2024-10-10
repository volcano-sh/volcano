/*
Copyright 2022 The Volcano Authors.

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

package jobflow

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	versionedscheme "volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	batchinformer "volcano.sh/apis/pkg/client/informers/externalversions/batch/v1alpha1"
	flowinformer "volcano.sh/apis/pkg/client/informers/externalversions/flow/v1alpha1"
	batchlister "volcano.sh/apis/pkg/client/listers/batch/v1alpha1"
	flowlister "volcano.sh/apis/pkg/client/listers/flow/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/framework"
	jobflowstate "volcano.sh/volcano/pkg/controllers/jobflow/state"
)

func init() {
	framework.RegisterController(&jobflowcontroller{})
}

// jobflowcontroller the JobFlow jobflowcontroller type.
type jobflowcontroller struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	//informer
	jobFlowInformer     flowinformer.JobFlowInformer
	jobTemplateInformer flowinformer.JobTemplateInformer
	jobInformer         batchinformer.JobInformer

	//InformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	//jobFlowLister
	jobFlowLister flowlister.JobFlowLister
	jobFlowSynced cache.InformerSynced

	//jobTemplateLister
	jobTemplateLister flowlister.JobTemplateLister
	jobTemplateSynced cache.InformerSynced

	//jobLister
	jobLister batchlister.JobLister
	jobSynced cache.InformerSynced

	// JobFlow Event recorder
	recorder record.EventRecorder

	queue          workqueue.RateLimitingInterface
	enqueueJobFlow func(req apis.FlowRequest)

	syncHandler func(req *apis.FlowRequest) error

	maxRequeueNum int
}

func (jf *jobflowcontroller) Name() string {
	return "jobflow-controller"
}

func (jf *jobflowcontroller) Initialize(opt *framework.ControllerOption) error {
	jf.kubeClient = opt.KubeClient
	jf.vcClient = opt.VolcanoClient

	factory := opt.VCSharedInformerFactory
	jf.vcInformerFactory = factory

	jf.jobFlowInformer = factory.Flow().V1alpha1().JobFlows()
	jf.jobFlowSynced = jf.jobFlowInformer.Informer().HasSynced
	jf.jobFlowLister = jf.jobFlowInformer.Lister()
	jf.jobFlowInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    jf.addJobFlow,
		UpdateFunc: jf.updateJobFlow,
	})

	jf.jobTemplateInformer = factory.Flow().V1alpha1().JobTemplates()
	jf.jobTemplateSynced = jf.jobTemplateInformer.Informer().HasSynced
	jf.jobTemplateLister = jf.jobTemplateInformer.Lister()

	jf.jobInformer = factory.Batch().V1alpha1().Jobs()
	jf.jobSynced = jf.jobInformer.Informer().HasSynced
	jf.jobLister = jf.jobInformer.Lister()
	jf.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: jf.updateJob,
	})

	jf.maxRequeueNum = opt.MaxRequeueNum
	if jf.maxRequeueNum < 0 {
		jf.maxRequeueNum = -1
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: jf.kubeClient.CoreV1().Events("")})

	jf.recorder = eventBroadcaster.NewRecorder(versionedscheme.Scheme, v1.EventSource{Component: "vc-controller-manager"})
	jf.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	jf.enqueueJobFlow = jf.enqueue

	jf.syncHandler = jf.handleJobFlow

	jobflowstate.SyncJobFlow = jf.syncJobFlow
	return nil
}

func (jf *jobflowcontroller) Run(stopCh <-chan struct{}) {
	defer jf.queue.ShutDown()

	jf.vcInformerFactory.Start(stopCh)
	for informerType, ok := range jf.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	go wait.Until(jf.worker, time.Second, stopCh)

	klog.Infof("JobFlowController is running ...... ")

	<-stopCh
}

func (jf *jobflowcontroller) worker() {
	for jf.processNextWorkItem() {
	}
}

func (jf *jobflowcontroller) processNextWorkItem() bool {
	obj, shutdown := jf.queue.Get()
	if shutdown {
		// Stop working
		return false
	}

	// We call Done here so the workqueue knows we have finished
	// processing this item. We also must remember to call Forget if we
	// do not want this work item being re-queued. For example, we do
	// not call Forget if a transient error occurs, instead the item is
	// put back on the workqueue and attempted again after a back-off
	// period.
	defer jf.queue.Done(obj)

	req, ok := obj.(apis.FlowRequest)
	if !ok {
		klog.Errorf("%v is not a valid queue request struct.", obj)
		return true
	}

	err := jf.syncHandler(&req)
	jf.handleJobFlowErr(err, obj)

	return true
}

func (jf *jobflowcontroller) handleJobFlow(req *apis.FlowRequest) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing jobflow %s (%v).", req.JobFlowName, time.Since(startTime))
	}()

	jobflow, err := jf.jobFlowLister.JobFlows(req.Namespace).Get(req.JobFlowName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).Infof("JobFlow %s has been deleted.", req.JobFlowName)
			return nil
		}

		return fmt.Errorf("get jobflow %s failed for %v", req.JobFlowName, err)
	}

	jobFlowState := jobflowstate.NewState(jobflow)
	if jobFlowState == nil {
		return fmt.Errorf("jobflow %s state %s is invalid", jobflow.Name, jobflow.Status.State)
	}

	klog.V(4).Infof("Begin execute %s action for jobflow %s", req.Action, req.JobFlowName)
	if err := jobFlowState.Execute(req.Action); err != nil {
		return fmt.Errorf("sync jobflow %s failed for %v, event is %v, action is %s",
			req.JobFlowName, err, req.Event, req.Action)
	}

	return nil
}

func (jf *jobflowcontroller) handleJobFlowErr(err error, obj interface{}) {
	if err == nil {
		jf.queue.Forget(obj)
		return
	}

	if jf.maxRequeueNum == -1 || jf.queue.NumRequeues(obj) < jf.maxRequeueNum {
		klog.V(4).Infof("Error syncing jobFlow request %v for %v.", obj, err)
		jf.queue.AddRateLimited(obj)
		return
	}

	req, _ := obj.(apis.FlowRequest)
	jf.recordEventsForJobFlow(req.Namespace, req.JobFlowName, v1.EventTypeWarning, string(req.Action),
		fmt.Sprintf("%v JobFlow failed for %v", req.Action, err))
	klog.V(4).Infof("Dropping JobFlow request %v out of the queue for %v.", obj, err)
	jf.queue.Forget(obj)
}

func (jf *jobflowcontroller) recordEventsForJobFlow(namespace, name, eventType, reason, message string) {
	jobFlow, err := jf.jobFlowLister.JobFlows(namespace).Get(name)
	if err != nil {
		klog.Errorf("Get JobFlow %s failed for %v.", name, err)
		return
	}

	jf.recorder.Event(jobFlow, eventType, reason, message)
}
