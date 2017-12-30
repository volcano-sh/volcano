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
	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/apis/v1"
	qjobclient "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client"
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client/informers"
	qjobv1informer "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client/informers/comboset/v1"
	qjobv1lister "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client/listers/comboset/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/controller/combosetresources"
	respod "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/controller/combosetresources/pod"
	queuev1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
	queueclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	queueInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	qclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
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

type ComboSetController struct {
	kubeClient              clientset.Interface
	qjobRegisteredResources combosetresources.RegisteredResources
	qjobResControls         map[qjobv1.ResourceType]combosetresources.Interface

	// Kubernetes restful client to operate queuejob
	qjobClient *rest.RESTClient

	// To allow injection of updateQueueJobStatus for testing.
	updateHandler func(queuejob *qjobv1.ComboSet) error
	syncHandler   func(queuejobKey string) error

	// A TTLCache of pod creates/deletes each rc expects to see
	expectations controller.ControllerExpectationsInterface

	// A store of queuejobs
	queueJobLister   qjobv1lister.ComboSetLister
	queueJobInformer qjobv1informer.ComboSetInformer

	// A store of queues
	queueInformer qclient.QueueInformer

	// QueueJobs that need to be updated
	queue workqueue.RateLimitingInterface

	// Reference manager to manage membership of queuejob resource and its members
	refManager combosetresources.RefManager

	recorder record.EventRecorder
}

func RegisterAllQueueJobResourceTypes(regs *combosetresources.RegisteredResources) {

	respod.Register(regs)

}

func NewQueueJobController(config *rest.Config, schCache cache.Cache) *ComboSetController {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)

	scheme.Scheme.AddKnownTypeWithName(qjobv1.SchemeGroupVersion.WithKind("QueueJob"), &qjobv1.ComboSet{})

	// create k8s clientset
	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Errorf("fail to create clientset")
		return nil
	}

	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.Core().RESTClient()).Events("")})

	qjobClient, _, err := qjobclient.NewQueueJobClient(config)

	qjm := &ComboSetController{
		kubeClient:   kubeClient,
		qjobClient:   qjobClient,
		expectations: controller.NewControllerExpectations(),
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "queuejob"),
		recorder:     eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "queuejob-controller"}),
	}

	// create informer for queuejob information
	qjobInformerFactory := qInformerfactory.NewSharedInformerFactory(qjobClient, 0)
	qjm.queueJobInformer = qjobInformerFactory.ComboSet().ComboSets()
	qjm.queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *qjobv1.ComboSet:
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

	// create queue informer
	queueClient, _, err := queueclient.NewClient(config)
	if err != nil {
		panic(err)
	}

	// create informer for queue information
	qInformerFactory := queueInformerfactory.NewSharedInformerFactory(queueClient, 0)
	qjm.queueInformer = qInformerFactory.Queue().Queues()
	qjm.queueInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *queuev1.Queue:
					glog.V(4).Infof("filter queue name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qjm.AddQueue,
				UpdateFunc: qjm.UpdateQueue,
				DeleteFunc: qjm.DeleteQueue,
			},
		})

	RegisterAllQueueJobResourceTypes(&qjm.qjobRegisteredResources)
	resControl, found, err := qjm.qjobRegisteredResources.InitComboSetResource(qjobv1.ResourceTypePod, config)
	if err != nil {
		glog.Errorf("fail to create queuejob resource control")
		return nil
	}
	if !found {
		glog.Errorf("queuejob resource type Pod not found")
		return nil
	}

	qjm.qjobResControls = map[qjobv1.ResourceType]combosetresources.Interface{}
	qjm.qjobResControls[qjobv1.ResourceTypePod] = resControl

	qjm.refManager = combosetresources.NewLabelRefManager()

	return qjm
}

// Run the main goroutine responsible for watching and syncing jobs.
func (qjm *ComboSetController) Run(workers int, stopCh <-chan struct{}) {

	go qjm.queueJobInformer.Informer().Run(stopCh)
	go qjm.queueInformer.Informer().Run(stopCh)
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
func (qjm *ComboSetController) enqueueController(obj interface{}) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Couldn't get key for object %+v: %v", obj, err))
		return
	}

	qjm.queue.Add(key)
}

// obj could be an *QueueJob, or a DeletionFinalStateUnknown marker item.
func (qjm *ComboSetController) addQueueJob(obj interface{}) {

	qjm.enqueueController(obj)
	return
}

func (qjm *ComboSetController) updateQueueJob(old, cur interface{}) {

	qjm.enqueueController(cur)
	return
}

func (qjm *ComboSetController) deleteQueueJob(obj interface{}) {

	qjm.enqueueController(obj)

}

//notification callback function for queue being added
func (qjm *ComboSetController) AddQueue(obj interface{}) {

	//TODO: adopt queuejobs belong to this queue

	return
}

//check 2 resources if equal
func resourcesEqual(r1, r2 *qjobv1.ResourceList) (bool, error) {

	if r1 == nil || r2 == nil {
		return false, fmt.Errorf("resources null error")
	}

	q1, found1 := r1.Resources["cpu"]
	q2, found2 := r2.Resources["cpu"]

	if !found1 || !found2 {
		return false, fmt.Errorf("cpu resource not found error")
	}

	if q1.Cmp(q2) != 0 {
		return false, nil
	}

	q1, found1 = r1.Resources["memory"]
	q2, found2 = r2.Resources["memory"]

	if !found1 || !found2 {
		return false, fmt.Errorf("memory resource not found error")
	}

	if q1.Cmp(q2) != 0 {
		return false, nil
	}

	return true, nil

}

//get all queuejobs belong to a certain queue
func (qjm *ComboSetController) getQueueJobsForQueue(j *queuev1.Queue) ([]*qjobv1.ComboSet, error) {
	qjoblist, err := qjm.queueJobLister.ComboSets(j.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	qjobs := []*qjobv1.ComboSet{}
	for i, qjob := range qjoblist {
		meta_qjob, err := meta.Accessor(qjob)
		if err != nil {
			return nil, err
		}

		qjob_queuename, found := meta_qjob.GetLabels()["queue"]
		if found && qjob_queuename == j.Name {
			qjobs = append(qjobs, qjoblist[i])
		}
	}
	return qjobs, nil

}

//Handle queue information updating
func (qjm *ComboSetController) updateQueue(oldQueue, newQueue *queuev1.Queue) error {

	equal, err := resourcesEqual(&oldQueue.Status.Allocated,
		&newQueue.Status.Allocated)
	if err != nil {
		return err
	}

	if !equal {
		qjobs, err := qjm.getQueueJobsForQueue(oldQueue)
		if err != nil {
			return err
		}

		//TODO: add re-schedule queuejob resources' quota handling here

		for _, qjob := range qjobs {
			qjm.enqueueController(qjob)
		}

	}

	return nil
}

//notification callback function for queue being updated
func (qjm *ComboSetController) UpdateQueue(oldObj, newObj interface{}) {
	oldQueue, ok := oldObj.(*queuev1.Queue)
	if !ok {
		glog.Errorf("cannot convert oldObj to *qjobv1.Queue: %v", oldObj)
		return
	}
	newQueue, ok := newObj.(*queuev1.Queue)
	if !ok {
		glog.Errorf("cannot convert newObj to *qjobv1.Queue: %v", newObj)
		return
	}

	glog.V(4).Infof("UPDATE oldQueue(%s) in cache, status(%#v), spec(%#v)\n", oldQueue.Name, oldQueue.Status, oldQueue.Spec)
	glog.V(4).Infof("UPDATE newQueue(%s) in cache, status(%#v), spec(%#v)\n", newQueue.Name, newQueue.Status, newQueue.Spec)
	err := qjm.updateQueue(oldQueue, newQueue)
	if err != nil {
		glog.Errorf("failed to update queue %s into cache: %v", oldQueue.Name, err)
		return
	}
	return
}

//notification callback function for queue being delelted
func (qjm *ComboSetController) DeleteQueue(obj interface{}) {

	//TODO: cleanup queuejobs belong to this queue

	return
}

func (qjm *ComboSetController) Cleanup(queuejob *qjobv1.ComboSet) error {

	if queuejob.Spec.AggrResources.Items != nil {
		for _, ar := range queuejob.Spec.AggrResources.Items {
			qjm.qjobResControls[ar.Type].Cleanup(queuejob, &ar)
		}
	}

	return nil
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (qjm *ComboSetController) worker() {
	for qjm.processNextWorkItem() {
	}
}

func (qjm *ComboSetController) processNextWorkItem() bool {

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

func (qjm *ComboSetController) syncQueueJob(key string) error {

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
	sharedJob, err := qjm.queueJobLister.ComboSets(ns).Get(name)
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

		var result qjobv1.ComboSet
		return qjm.qjobClient.Put().
			Namespace(ns).Resource(qjobv1.ComboSetPlural).
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
		var result qjobv1.ComboSet
		qjm.qjobClient.Put().
			Namespace(ns).Resource(qjobv1.ComboSetPlural).
			Name(name).Body(sharedJob).Do().Into(&result)

		//TODO: Add distributing resource quota among sub-resources

		for _, ar := range job.Spec.AggrResources.Items {
			qjm.qjobResControls[ar.Type].Sync(sharedJob, &ar)
		}
	}

	return nil
}

func (qjm *ComboSetController) updateJobStatus(queuejob *qjobv1.ComboSet) error {
	return nil
}
