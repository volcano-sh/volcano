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

package controller

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	qjobclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	qjobv1informer "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/queuejob/v1"
	qjobv1lister "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/listers/queuejob/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/controller/queuejobresources"
	respod "github.com/kubernetes-incubator/kube-arbitrator/pkg/controller/queuejobresources/pod"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
)

var controllerKind = qjobv1.SchemeGroupVersion.WithKind("QueueJob")

type QueueJobController struct {
	kubeClient              clientset.Interface
	qjobRegisteredResources queuejobresources.RegisteredResources
	qjobResControls         map[qjobv1.ResourceType]queuejobresources.Interface

	// Kubernetes restful client to operate queuejob
	qjobClient *rest.RESTClient

	// To allow injection of updateQueueJobStatus for testing.
	updateHandler func(queuejob *qjobv1.QueueJob) error
	syncHandler   func(queuejobKey string) error

	// A TTLCache of pod creates/deletes each rc expects to see
	expectations controller.ControllerExpectationsInterface

	// A store of queuejobs
	queueJobLister   qjobv1lister.QueueJobLister
	queueJobInformer qjobv1informer.QueueJobInformer

	// QueueJobs that need to be updated
	queue workqueue.RateLimitingInterface

	// Reference manager to manage membership of queuejob resource and its members
	refManager queuejobresources.RefManager

	recorder record.EventRecorder
}

func RegisterAllQueueJobResourceTypes(regs *queuejobresources.RegisteredResources) {

	respod.Register(regs)

}

func NewQueueJobController(config *rest.Config, schCache schedulercache.Cache) *QueueJobController {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)

	scheme.Scheme.AddKnownTypeWithName(qjobv1.SchemeGroupVersion.WithKind("QueueJob"), &qjobv1.QueueJob{})

	// create k8s clientset
	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Errorf("fail to create clientset")
		return nil
	}

	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.Core().RESTClient()).Events("")})

	qjobClient, _, err := qjobclient.NewQueueJobClient(config)

	qjm := &QueueJobController{
		kubeClient:   kubeClient,
		qjobClient:   qjobClient,
		expectations: controller.NewControllerExpectations(),
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "queuejob"),
		recorder:     eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "queuejob-controller"}),
	}

	// create informer for queuejob information
	qjobInformerFactory := qInformerfactory.NewSharedInformerFactory(qjobClient, 0)
	qjm.queueJobInformer = qjobInformerFactory.QueueJob().QueueJobs()
	qjm.queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *qjobv1.QueueJob:
					glog.V(4).Infof("filter queuejob name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qjm.addQueueJob,
				DeleteFunc: qjm.deleteQueueJob,
				UpdateFunc: qjm.updateQueueJob,
			},
		})
	qjm.queueJobLister = qjm.queueJobInformer.Lister()

	qjm.updateHandler = qjm.updateJobStatus
	qjm.syncHandler = qjm.syncQueueJob

	RegisterAllQueueJobResourceTypes(&qjm.qjobRegisteredResources)
	resControl, found, err := qjm.qjobRegisteredResources.InitQueueJobResource(qjobv1.ResourceTypePod, config)
	if err != nil {
		glog.Errorf("fail to create queuejob resource control")
		return nil
	}
	if !found {
		glog.Errorf("queuejob resource type Pod not found")
		return nil
	}

	qjm.qjobResControls = map[qjobv1.ResourceType]queuejobresources.Interface{}
	qjm.qjobResControls[qjobv1.ResourceTypePod] = resControl

	qjm.refManager = queuejobresources.NewLabelRefManager()

	return qjm
}

// Run the main goroutine responsible for watching and syncing jobs.
func (qjm *QueueJobController) Run(workers int, stopCh <-chan struct{}) {

	go qjm.queueJobInformer.Informer().Run(stopCh)
	go qjm.qjobResControls[qjobv1.ResourceTypePod].Run(stopCh)

	defer utilruntime.HandleCrash()
	defer qjm.queue.ShutDown()

	glog.Infof("Starting queuejob controller")
	defer glog.Infof("Shutting down queuejob controller")

	for i := 0; i < workers; i++ {
		go wait.Until(qjm.worker, time.Second, stopCh)
	}
	<-stopCh
}

// obj could be an *QueueJob, or a DeletionFinalStateUnknown marker item.
func (qjm *QueueJobController) enqueueController(obj interface{}) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %+v: %v", obj, err))
		return
	}

	qjm.queue.Add(key)
}

// obj could be an *QueueJob, or a DeletionFinalStateUnknown marker item.
func (qjm *QueueJobController) addQueueJob(obj interface{}) {

	qjm.enqueueController(obj)
	return
}

func (qjm *QueueJobController) updateQueueJob(old, cur interface{}) {

	qjm.enqueueController(cur)
	return
}

func (qjm *QueueJobController) deleteQueueJob(obj interface{}) {

	qjm.enqueueController(obj)

}

func (qjm *QueueJobController) Cleanup(queuejob *qjobv1.QueueJob) error {

	if queuejob.Spec.AggrResources.Items != nil {
		for _, ar := range queuejob.Spec.AggrResources.Items {
			qjm.qjobResControls[ar.Type].Cleanup(queuejob, &ar)
		}
	}

	return nil
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (qjm *QueueJobController) worker() {
	for qjm.processNextWorkItem() {
	}
}

func (qjm *QueueJobController) processNextWorkItem() bool {

	key, quit := qjm.queue.Get()
	if quit {
		return false
	}
	defer qjm.queue.Done(key)

	err := qjm.syncHandler(key.(string))
	if err == nil {
		qjm.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("Error syncing queueJob: %v", err))
	qjm.queue.AddRateLimited(key)

	return true
}

func (qjm *QueueJobController) syncQueueJob(key string) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job %q (%v)", key, time.Now().Sub(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	if len(ns) == 0 || len(name) == 0 {
		return fmt.Errorf("invalid queue job key %q: either namespace or name is missing", key)
	}
	sharedJob, err := qjm.queueJobLister.QueueJobs(ns).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(4).Infof("Job has been deleted: %v", key)
			qjm.expectations.DeleteExpectations(key)
			return nil
		}
		return err
	}
	job := *sharedJob

	if job.DeletionTimestamp != nil {
		err = qjm.Cleanup(sharedJob)
		if err != nil {
			return err
		}

		//empty finalizers and delete the queuejob again
		accessor, err := meta.Accessor(sharedJob)
		if err != nil {
			return err
		}
		accessor.SetFinalizers(nil)

		var result qjobv1.QueueJob
		return qjm.qjobClient.Put().
			Namespace(ns).Resource(qjobv1.QueueJobPlural).
			Name(name).Body(sharedJob).Do().Into(&result)

	}

	if job.Spec.AggrResources.Items != nil {
		for i := range job.Spec.AggrResources.Items {
			err := qjm.refManager.AddTag(&job.Spec.AggrResources.Items[i], func() string {
				return strconv.Itoa(i)
			})
			if err != nil {
				return err
			}

		}
		var result qjobv1.QueueJob
		qjm.qjobClient.Put().
			Namespace(ns).Resource(qjobv1.QueueJobPlural).
			Name(name).Body(sharedJob).Do().Into(&result)

		for _, ar := range job.Spec.AggrResources.Items {
			qjm.qjobResControls[ar.Type].Sync(sharedJob, &ar)
		}
	}

	return nil
}

func (qjm *QueueJobController) updateJobStatus(queuejob *qjobv1.QueueJob) error {
	return nil
}
