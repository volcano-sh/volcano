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

package queuejob

import (
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/utils"
	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	clientset "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned/clients"
	arbinformers "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion"
	informersv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion/v1"
	listersv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/listers/controller/v1"
)

const (
	// QueueJobLabel label string for queuejob name
	QueueJobLabel string = "queuejob.kube-arbitrator.k8s.io"
)

// Controller the QueueJob Controller type
type Controller struct {
	config           *rest.Config
	queueJobInformer informersv1.QueueJobInformer
	podInformer      coreinformers.PodInformer
	clients          *kubernetes.Clientset
	arbclients       *clientset.Clientset

	// A store of jobs
	queueJobLister listersv1.QueueJobLister
	queueJobSynced func() bool

	// A store of pods, populated by the podController
	podListr  corelisters.PodLister
	podSynced func() bool

	// eventQueue that need to sync up
	eventQueue *cache.FIFO
}

// NewQueueJobController create new QueueJob Controller
func NewQueueJobController(config *rest.Config) *Controller {
	cc := &Controller{
		config:     config,
		clients:    kubernetes.NewForConfigOrDie(config),
		arbclients: clientset.NewForConfigOrDie(config),
		eventQueue: cache.NewFIFO(eventKey),
	}

	queueJobClient, _, err := clients.NewClient(cc.config)
	if err != nil {
		panic(err)
	}

	cc.queueJobInformer = arbinformers.NewSharedInformerFactory(queueJobClient, 0).QueueJob().QueueJobs()
	cc.queueJobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addQueueJob,
		UpdateFunc: cc.updateQueueJob,
		DeleteFunc: cc.deleteQueueJob,
	})
	cc.queueJobLister = cc.queueJobInformer.Lister()
	cc.queueJobSynced = cc.queueJobInformer.Informer().HasSynced

	cc.podInformer = informers.NewSharedInformerFactory(cc.clients, 0).Core().V1().Pods()
	cc.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addPod,
		UpdateFunc: cc.updatePod,
		DeleteFunc: cc.deletePod,
	})

	cc.podListr = cc.podInformer.Lister()
	cc.podSynced = cc.podInformer.Informer().HasSynced

	return cc
}

// Run start QueueJob Controller
func (cc *Controller) Run(stopCh chan struct{}) {
	// initialized
	createQueueJobKind(cc.config)

	go cc.queueJobInformer.Informer().Run(stopCh)
	go cc.podInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.queueJobSynced, cc.podSynced)

	go wait.Until(cc.worker, time.Second, stopCh)
}

func (cc *Controller) addQueueJob(obj interface{}) {
	qj, ok := obj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("obj is not QueueJob")
		return
	}

	cc.enqueue(qj)
}

func (cc *Controller) updateQueueJob(oldObj, newObj interface{}) {
	newQJ, ok := newObj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("newObj is not QueueJob")
		return
	}

	cc.enqueue(newQJ)
}

func (cc *Controller) deleteQueueJob(obj interface{}) {
	qj, ok := obj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("obj is not QueueJob")
		return
	}

	cc.enqueue(qj)
}

func (cc *Controller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Error("Failed to convert %v to v1.Pod", obj)
		return
	}

	cc.enqueue(pod)
}

func (cc *Controller) updatePod(oldObj, newObj interface{}) {
	pod, ok := newObj.(*v1.Pod)
	if !ok {
		glog.Error("Failed to convert %v to v1.Pod", newObj)
		return
	}

	cc.enqueue(pod)
}

func (cc *Controller) deletePod(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Pod: %v", t)
		return
	}

	queuejobs, err := cc.queueJobLister.List(labels.Everything())
	if err != nil {
		glog.Errorf("Failed to list QueueJobs for Pod %v/%v", pod.Namespace, pod.Name)
	}

	ctl := utils.GetController(pod)
	for _, qj := range queuejobs {
		if qj.UID == ctl {
			cc.enqueue(qj)
			break
		}
	}
}

func (cc *Controller) enqueue(obj interface{}) {
	err := cc.eventQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue QueueJob to updateQueue, err %#v", err)
	}
}

func (cc *Controller) worker() {
	if _, err := cc.eventQueue.Pop(func(obj interface{}) error {
		var queuejob *arbv1.QueueJob
		switch v := obj.(type) {
		case *arbv1.QueueJob:
			queuejob = v
		case *v1.Pod:
			queuejobs, err := cc.queueJobLister.List(labels.Everything())
			if err != nil {
				glog.Errorf("Failed to list QueueJobs for Pod %v/%v", v.Namespace, v.Name)
			}

			ctl := utils.GetController(v)
			for _, qj := range queuejobs {
				if qj.UID == ctl {
					queuejob = qj
					break
				}
			}

		default:
			glog.Errorf("Un-supported type of %v", obj)
			return nil
		}

		if queuejob == nil {
			if acc, err := meta.Accessor(obj); err != nil {
				glog.Warningf("Failed to get QueueJob for %v/%v", acc.GetNamespace(), acc.GetName())
			}

			return nil
		}

		// sync Pods for a QueueJob
		if err := cc.syncQueueJob(queuejob); err != nil {
			glog.Errorf("Failed to sync QueueJob %s, err %#v", queuejob.Name, err)
			// If any error, requeue it.
			return err
		}

		return nil
	}); err != nil {
		glog.Errorf("Fail to pop item from updateQueue, err %#v", err)
		return
	}
}

// filterActivePods returns pods that have not terminated.
func filterActivePods(pods []*v1.Pod) []*v1.Pod {
	var result []*v1.Pod
	for _, p := range pods {
		if isPodActive(p) {
			result = append(result, p)
		} else {
			glog.V(4).Infof("Ignoring inactive pod %v/%v in state %v, deletion time %v",
				p.Namespace, p.Name, p.Status.Phase, p.DeletionTimestamp)
		}
	}
	return result
}

func isPodActive(p *v1.Pod) bool {
	return v1.PodSucceeded != p.Status.Phase &&
		v1.PodFailed != p.Status.Phase &&
		p.DeletionTimestamp == nil
}

func (cc *Controller) syncQueueJob(qj *arbv1.QueueJob) error {
	queueJob, err := cc.queueJobLister.QueueJobs(qj.Namespace).Get(qj.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			glog.V(3).Infof("Job has been deleted: %v", qj.Name)
			return nil
		}
		return err
	}

	pods, err := cc.getPodsForQueueJob(queueJob)
	if err != nil {
		return err
	}

	return cc.manageQueueJob(queueJob, pods)
}

func (cc *Controller) getPodsForQueueJob(qj *arbv1.QueueJob) (map[string][]*v1.Pod, error) {
	pods := map[string][]*v1.Pod{}

	for _, ts := range qj.Spec.TaskSpecs {
		selector, err := metav1.LabelSelectorAsSelector(ts.Selector)
		if err != nil {
			return nil, fmt.Errorf("couldn't convert QueueJob selector: %v", err)
		}

		// List all pods under QueueJob
		ps, err := cc.podListr.Pods(qj.Namespace).List(selector)
		if err != nil {
			return nil, err
		}

		// TODO (k82cn): optimic by cache
		for _, pod := range ps {
			if !metav1.IsControlledBy(pod, qj) {
				continue
			}
			// Hash by TaskSpec.Template.Name
			pods[ts.Template.Name] = append(pods[ts.Template.Name], pod)
		}
	}

	return pods, nil
}

// manageQueueJob is the core method responsible for managing the number of running
// pods according to what is specified in the job.Spec.
func (cc *Controller) manageQueueJob(qj *arbv1.QueueJob, pods map[string][]*v1.Pod) error {
	var err error

	runningSum := int32(0)
	pendingSum := int32(0)
	succeededSum := int32(0)
	failedSum := int32(0)

	ss, err := cc.arbclients.ArbV1().SchedulingSpecs(qj.Namespace).List(metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", qj.Name),
	})

	if len(ss.Items) == 0 {
		schedSpc := createQueueJobSchedulingSpec(qj)
		_, err := cc.arbclients.ArbV1().SchedulingSpecs(qj.Namespace).Create(schedSpc)
		if err != nil {
			glog.Errorf("Failed to create SchedulingSpec for QueueJob %v/%v: %v",
				qj.Namespace, qj.Name, err)
		}
	} else {
		glog.V(3).Infof("There's %v SchedulingSpec for QueueJob %v/%v",
			len(ss.Items), qj.Namespace, qj.Name)
	}

	for _, ts := range qj.Spec.TaskSpecs {
		replicas := ts.Replicas
		name := ts.Template.Name

		running := int32(filterPods(pods[name], v1.PodRunning))
		pending := int32(filterPods(pods[name], v1.PodPending))
		succeeded := int32(filterPods(pods[name], v1.PodSucceeded))
		failed := int32(filterPods(pods[name], v1.PodFailed))

		runningSum += running
		pendingSum += pending
		succeededSum += succeeded
		failedSum += failed

		glog.V(3).Infof("There are %d pods of QueueJob %s (%s): replicas %d, pending %d, running %d, succeeded %d, failed %d",
			len(pods), qj.Name, name, replicas, pending, running, succeeded, failed)

		// Create pod if necessary
		if diff := replicas - pending - running - succeeded; diff > 0 {
			glog.V(3).Infof("Try to create %v Pods for QueueJob %v/%v", diff, qj.Namespace, qj.Name)

			var errs []error
			wait := sync.WaitGroup{}
			wait.Add(int(diff))
			for i := int32(0); i < diff; i++ {
				go func(ix int32) {
					defer wait.Done()
					newPod := createQueueJobPod(qj, &ts.Template, ix)
					_, err := cc.clients.Core().Pods(newPod.Namespace).Create(newPod)
					if err != nil {
						// Failed to create Pod, wait a moment and then create it again
						// This is to ensure all pods under the same QueueJob created
						// So gang-scheduling could schedule the QueueJob successfully
						glog.Errorf("Failed to create pod %s for QueueJob %s, err %#v",
							newPod.Name, qj.Name, err)
						errs = append(errs, err)
					}
				}(i)
			}
			wait.Wait()

			if len(errs) != 0 {
				return fmt.Errorf("failed to create %d pods of %d", len(errs), diff)
			}
		}
	}

	qj.Status = arbv1.QueueJobStatus{
		Pending:      pendingSum,
		Running:      runningSum,
		Succeeded:    succeededSum,
		Failed:       failedSum,
		MinAvailable: int32(qj.Spec.SchedSpec.MinAvailable),
	}

	// TODO(k82cn): replaced it with `UpdateStatus`
	if _, err := cc.arbclients.ArbV1().QueueJobs(qj.Namespace).Update(qj); err != nil {
		glog.Errorf("Failed to update status of QueueJob %v/%v: %v",
			qj.Namespace, qj.Name, err)
		return err
	}

	return err
}
