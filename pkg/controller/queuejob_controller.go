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
	"sync"
	"time"

	"github.com/golang/glog"
	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	qjobclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	qjobv1informer "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/queuejob/v1"
	qjobv1lister "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/listers/queuejob/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
)

var controllerKind = qjobv1.SchemeGroupVersion.WithKind("QueueJob")

const qjobResIdxLable = "QJOBRES_INDEX"

type QueueJobController struct {
	kubeClient clientset.Interface
	podControl controller.PodControlInterface

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

	// A store of pods, populated by the podController
	podStore    corelisters.PodLister
	podInformer corev1informer.PodInformer

	// QueueJobs that need to be updated
	queue workqueue.RateLimitingInterface

	recorder record.EventRecorder
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
		kubeClient: kubeClient,
		qjobClient: qjobClient,
		podControl: controller.RealPodControl{
			KubeClient: kubeClient,
			Recorder:   eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "queuejob-controller"}),
		},
		expectations: controller.NewControllerExpectations(),
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "queuejob"),
		recorder:     eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "queuejob-controller"}),
	}

	// create informer for pod information
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	qjm.podInformer = informerFactory.Core().V1().Pods()
	qjm.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					glog.V(4).Infof("filter pod name(%s) namespace(%s) status(%s)\n", t.Name, t.Namespace, t.Status.Phase)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qjm.addPod,
				UpdateFunc: qjm.updatePod,
				DeleteFunc: qjm.deletePod,
			},
		})

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

	return qjm
}

// Run the main goroutine responsible for watching and syncing jobs.
func (qjm *QueueJobController) Run(workers int, stopCh <-chan struct{}) {

	go qjm.queueJobInformer.Informer().Run(stopCh)
	go qjm.podInformer.Informer().Run(stopCh)

	defer utilruntime.HandleCrash()
	defer qjm.queue.ShutDown()

	glog.Infof("Starting queuejob controller")
	defer glog.Infof("Shutting down queuejob controller")

	for i := 0; i < workers; i++ {
		go wait.Until(qjm.worker, time.Second, stopCh)
	}
	<-stopCh
}

func (qjm *QueueJobController) addPod(obj interface{}) {

	return
}

func (qjm *QueueJobController) updatePod(old, cur interface{}) {

	return
}

func (qjm *QueueJobController) deletePod(obj interface{}) {

	return
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

func (qjm *QueueJobController) scaleUpQueueJobResource(idx int, diff int32, activePods []*v1.Pod, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	qjobRes.Template.Labels[qjobResIdxLable] = strconv.Itoa(idx)

	wait := sync.WaitGroup{}
	wait.Add(int(diff))
	for i := int32(0); i < diff; i++ {
		go func() {
			defer wait.Done()
			err := qjm.podControl.CreatePodsWithControllerRef(queuejob.Namespace, &qjobRes.Template, queuejob, metav1.NewControllerRef(queuejob, controllerKind))
			if err != nil && errors.IsTimeout(err) {
				return
			}
			if err != nil {
				defer utilruntime.HandleError(err)
			}
		}()
	}
	wait.Wait()

	return nil

}

func (qjm *QueueJobController) scaleDownQueueJobResource(idx int, diff int32, activePods []*v1.Pod, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	wait := sync.WaitGroup{}
	wait.Add(int(diff))
	for i := int32(0); i < diff; i++ {
		go func(ix int32) {
			defer wait.Done()
			if err := qjm.podControl.DeletePod(queuejob.Namespace, activePods[ix].Name, queuejob); err != nil {
				defer utilruntime.HandleError(err)
			}
		}(i)
	}
	wait.Wait()
	return nil

}

func (qjm *QueueJobController) deleteQueueJob(obj interface{}) {

	qjm.enqueueController(obj)

}

func (qjm *QueueJobController) deleteQueueJobPods(queuejob *qjobv1.QueueJob) error {

	job := *queuejob

	pods, err := qjm.getPodsForQueueJob(queuejob)
	if err != nil {
		return err
	}

	activePods := controller.FilterActivePods(pods)
	active := int32(len(activePods))

	wait := sync.WaitGroup{}
	wait.Add(int(active))
	for i := int32(0); i < active; i++ {
		go func(ix int32) {
			defer wait.Done()
			if err := qjm.podControl.DeletePod(queuejob.Namespace, activePods[ix].Name, queuejob); err != nil {
				defer utilruntime.HandleError(err)
				glog.V(2).Infof("Failed to delete %v, queue job %q/%q deadline exceeded", activePods[ix].Name, job.Namespace, job.Name)
			}
		}(i)
	}
	wait.Wait()

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

func (qjm *QueueJobController) syncQueueJobResource(idx int, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	if qjobRes.ObjectMeta.Labels == nil {
		qjobRes.ObjectMeta.Labels = map[string]string{}
	}
	qjobRes.ObjectMeta.Labels[qjobResIdxLable] = strconv.Itoa(idx)
	qjobRes.Template.Labels[qjobResIdxLable] = strconv.Itoa(idx)

	pods, err := qjm.getPodsForQueueJobRes(idx, queuejob)
	if err != nil {
		return err
	}

	activePods := controller.FilterActivePods(pods)
	numActivePods := int32(len(activePods))

	if qjobRes.DesiredReplicas < numActivePods {

		qjm.scaleDownQueueJobResource(idx,
			int32(numActivePods-qjobRes.DesiredReplicas),
			activePods, queuejob, qjobRes)
	} else if qjobRes.DesiredReplicas > numActivePods {

		qjm.scaleUpQueueJobResource(idx,
			int32(qjobRes.DesiredReplicas-numActivePods),
			activePods, queuejob, qjobRes)
	}

	return nil
}

func (qjm *QueueJobController) getPodsForQueueJob(j *qjobv1.QueueJob) ([]*v1.Pod, error) {
	podlist, err := qjm.kubeClient.CoreV1().Pods(j.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	pods := []*v1.Pod{}
	for i, pod := range podlist.Items {
		meta_pod, err := meta.Accessor(&pod)
		if err != nil {
			return nil, err
		}

		controllerRef := metav1.GetControllerOf(meta_pod)
		if controllerRef != nil {
			if controllerRef.UID == j.UID {
				pods = append(pods, &podlist.Items[i])
			}
		}
	}
	return pods, nil

}

func (qjm *QueueJobController) getPodsForQueueJobRes(idx int, j *qjobv1.QueueJob) ([]*v1.Pod, error) {

	pods, err := qjm.getPodsForQueueJob(j)
	if err != nil {
		return nil, err
	}

	myPods := []*v1.Pod{}
	for i, pod := range pods {
		if pod.Labels[qjobResIdxLable] == strconv.Itoa(idx) {
			myPods = append(myPods, pods[i])
		}
	}

	return myPods, nil

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
		err = qjm.deleteQueueJobPods(sharedJob)
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
		for i, ar := range job.Spec.AggrResources.Items {
			qjm.syncQueueJobResource(i, sharedJob, &ar)
		}
	}

	return nil
}

func (qjm *QueueJobController) updateJobStatus(queuejob *qjobv1.QueueJob) error {
	return nil
}
