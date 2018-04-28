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

	"k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/clientset"
	arbinformers "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/informers"
	informersv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/informers/v1"
	listersv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/listers/v1"
)

const (
	// QueueJobNameLabel label string for queuejob name
	QueueJobNameLabel string = "queuejob-name"

	// ControllerUIDLabel label string for queuejob controller uid
	ControllerUIDLabel string = "controller-uid"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = arbv1.SchemeGroupVersion.WithKind("QueueJob")

// Controller the QueueJob Controller type
type Controller struct {
	config           *rest.Config
	queueJobInformer informersv1.QueueJobInformer
	podInformer      coreinformers.PodInformer
	clients          *kubernetes.Clientset
	arbclients       *clientset.Clientset

	// A store of jobs
	queueJobLister listersv1.QueueJobLister

	// A store of pods, populated by the podController
	podStore corelisters.PodLister

	// QueueJobs that need to initialized
	// Add labels and selectors to QueueJob
	initQueue *cache.FIFO

	// QueueJobs that need to sync up after initialization
	updateQueue *cache.FIFO

	// A counter that store the current terminating pods no of QueueJob
	// this is used to avoid to re-create the pods of a QueueJob before
	// all the old pods are terminated
	deletedPodsCounter *syncCounterMap
}

// NewController create new QueueJob Controller
func NewController(config *rest.Config) *Controller {
	cc := &Controller{
		config:             config,
		clients:            kubernetes.NewForConfigOrDie(config),
		arbclients:         clientset.NewForConfigOrDie(config),
		initQueue:          cache.NewFIFO(queueJobKey),
		updateQueue:        cache.NewFIFO(queueJobKey),
		deletedPodsCounter: newSyncCounterMap(),
	}

	queueJobClient, _, err := client.NewClient(cc.config)
	if err != nil {
		panic(err)
	}

	cc.queueJobInformer = arbinformers.NewSharedInformerFactory(queueJobClient, 0).QueueJob().QueueJobs()
	cc.queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *arbv1.QueueJob:
					glog.V(4).Infof("Filter QueueJob name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    cc.addQueueJob,
				UpdateFunc: cc.updateQueueJob,
				DeleteFunc: cc.deleteQueueJob,
			},
		})
	cc.queueJobLister = cc.queueJobInformer.Lister()

	cc.podInformer = informers.NewSharedInformerFactory(cc.clients, 0).Core().V1().Pods()
	cc.podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.Pod:
				glog.V(4).Infof("Filter Pod name(%s) namespace(%s)\n", t.Name, t.Namespace)
				return true
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    cc.addPod,
			UpdateFunc: cc.updatePod,
			DeleteFunc: cc.deletePod,
		},
	})
	cc.podStore = cc.podInformer.Lister()

	return cc
}

// Run start QueueJob Controller
func (cc *Controller) Run(stopCh chan struct{}) {
	// initialized
	createQueueJobCRD(cc.config)

	go cc.queueJobInformer.Informer().Run(stopCh)
	go cc.podInformer.Informer().Run(stopCh)

	go wait.Until(cc.initWorker, time.Second, stopCh)
	go wait.Until(cc.updateWorker, time.Second, stopCh)
}

func (cc *Controller) addQueueJob(obj interface{}) {
	qj, ok := obj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("obj is not QueueJob")
		return
	}

	cc.enqueueInitQueue(qj)
}

func (cc *Controller) updateQueueJob(oldObj, newObj interface{}) {
	_, ok := oldObj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("oldObj is not QueueJob")
		return
	}
	newQJ, ok := newObj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("newObj is not QueueJob")
		return
	}

	cc.enqueueUpdateQueue(newQJ)
}

func (cc *Controller) deleteQueueJob(obj interface{}) {}

func (cc *Controller) addPod(obj interface{}) {}

func (cc *Controller) updatePod(oldObj, newObj interface{}) {}

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

	// update delete pod counter for a QueueJob
	if len(pod.Labels) != 0 && len(pod.Labels[QueueJobNameLabel]) > 0 {
		cc.deletedPodsCounter.decreaseCounter(fmt.Sprintf("%s/%s", pod.Namespace, pod.Labels[QueueJobNameLabel]))
	}
}

func (cc *Controller) enqueueInitQueue(obj interface{}) {
	err := cc.initQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue QueueJob to initQueue, err %#v", err)
	}
}

func (cc *Controller) enqueueUpdateQueue(obj interface{}) {
	err := cc.updateQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue QueueJob to updateQueue, err %#v", err)
	}
}

func (cc *Controller) initWorker() {
	item, err := cc.initQueue.Pop(func(obj interface{}) error {
		return nil
	})
	if err != nil {
		glog.Errorf("Fail to pop item from initQueue, err %#v", err)
		return
	}

	queuejob, ok := item.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.QueueJob: %v", queuejob)
		return
	}

	// create PDB for QueueJob to support gang-scheduling
	err = cc.initPDB(int(queuejob.Spec.Replicas), queuejob.Spec.Template.Labels)
	if err != nil {
		glog.Errorf("Failed to create PDB for QueueJob %s, err %#v", queuejob.Name, err)
		return
	}

	// Add labels and selectors which are used by controller for a QueueJob
	// And update to api server
	err = cc.initLabelsForQueueJob(queuejob)
	if err != nil {
		glog.Errorf("Failed to init Labels for QueueJob %s, err %#v", queuejob.Name, err)
	}
}

func (cc *Controller) initLabelsForQueueJob(qj *arbv1.QueueJob) error {
	// Get QueueJob from apiserver
	updated, err := cc.queueJobLister.QueueJobs(qj.Namespace).Get(qj.Name)
	if err != nil {
		glog.Errorf("Fail to get QueueJob %s, err %#v", updated.Name, err)
		return err
	}

	// Add labels for QueueJob
	if updated.Labels == nil {
		updated.Labels = make(map[string]string)
	}
	updated.Labels[QueueJobNameLabel] = updated.Name
	updated.Labels[ControllerUIDLabel] = string(updated.UID)

	// Add selector for QueueJob
	if updated.Spec.Selector == nil {
		updated.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: make(map[string]string),
		}
	}
	updated.Spec.Selector.MatchLabels[ControllerUIDLabel] = updated.Labels[ControllerUIDLabel]

	// Add labels for pod template
	if updated.Spec.Template.Labels == nil {
		updated.Spec.Template.Labels = make(map[string]string)
	}
	updated.Spec.Template.Labels[QueueJobNameLabel] = updated.Labels[QueueJobNameLabel]
	updated.Spec.Template.Labels[ControllerUIDLabel] = updated.Labels[ControllerUIDLabel]

	_, err = cc.arbclients.ArbV1().QueueJobs(updated.Namespace).Update(updated)
	if err != nil {
		glog.Errorf("Fail to update QueueJob %s, err %#v", updated.Name, err)
		return err
	}
	return nil
}

func (cc *Controller) updateWorker() {
	item, err := cc.updateQueue.Pop(func(obj interface{}) error {
		return nil
	})
	if err != nil {
		glog.Errorf("Fail to pop item from updateQueue, err %#v", err)
		return
	}

	queuejob, ok := item.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.QueueJob: %v", queuejob)
		return
	}

	// sync Pods for a QueueJob
	jobDone, err := cc.syncQueueJob(queuejob)
	if err != nil {
		glog.Errorf("Failed to sync QueueJob %s, err %#v", queuejob.Name, err)
	}
	if !jobDone {
		// QueueJob doesn't finish, re-queue it to handle following case:
		// 1. pod terminated unexpectedly
		// 2. queuejob replicas is updated
		// if the queuejob is not changed, then syncQueueJob will do nothing for it
		cc.enqueueUpdateQueue(queuejob)
	}
}

func (cc *Controller) initPDB(min int, selectorMap map[string]string) error {
	selector := &metav1.LabelSelector{
		MatchLabels: selectorMap,
	}
	minAvailable := intstr.FromInt(min)
	pdb := &v1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("pdb-%s", generateUUID()),
		},
		Spec: v1beta1.PodDisruptionBudgetSpec{
			Selector:     selector,
			MinAvailable: &minAvailable,
		},
	}

	_, err := cc.clients.Policy().PodDisruptionBudgets("default").Create(pdb)

	return err
}

func (cc *Controller) syncQueueJob(qj *arbv1.QueueJob) (bool, error) {
	// check if there are still terminating pods for this QueueJob
	counter, ok := cc.deletedPodsCounter.get(fmt.Sprintf("%s/%s", qj.Namespace, qj.Name))
	if ok && counter >= 0 {
		return false, fmt.Errorf("There are still teminating pods for QueueJob %s/%s, can not sync it now", qj.Namespace, qj.Name)
	}

	sharedQueueJob, err := cc.queueJobLister.QueueJobs(qj.Namespace).Get(qj.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			glog.V(4).Infof("Job has been deleted: %v", qj.Name)
			return true, nil
		}
		return false, err
	}
	queueJob := *sharedQueueJob

	pods, err := cc.getPodsForQueueJob(&queueJob)
	if err != nil {
		return false, err
	}
	glog.V(4).Infof("There are %d pods of QueueJob %s\n", len(pods), queueJob.Name)

	activePods := controller.FilterActivePods(pods)
	glog.V(4).Infof("There are %d active pods of QueueJob %s\n", len(activePods), queueJob.Name)

	succeeded, _ := getStatus(pods)
	jobDone, err := cc.manageQueueJob(activePods, succeeded, &queueJob)

	return jobDone, err
}

func (cc *Controller) getPodsForQueueJob(qj *arbv1.QueueJob) ([]*v1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(qj.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert QueueJob selector: %v", err)
	}

	// List all pods under QueueJob
	pods, err := cc.podStore.Pods(qj.Namespace).List(selector)
	if err != nil {
		return nil, err
	}

	return pods, nil
}

// manageQueueJob is the core method responsible for managing the number of running
// pods according to what is specified in the job.Spec.
// Does NOT modify <activePods>.
func (cc *Controller) manageQueueJob(activePods []*v1.Pod, succeeded int32, qj *arbv1.QueueJob) (bool, error) {
	jobDone := false
	var err error
	active := int32(len(activePods))
	replicas := qj.Spec.Replicas

	if active+succeeded > replicas {
		// the QueueJob replicas is reduce by user, terminated all pods for gang scheduling
		// and re-create pods for the queuejob in next loop
		jobDone = false
		// TODO(jinzhejz): need make sure manage this QueueJob after all old pods are terminated
		err = cc.terminatePodsForQueueJob(qj)
	} else if active+succeeded == replicas {
		// pod number match QueueJob replicas perfectly
		if succeeded == replicas {
			// all pods exit successfully
			jobDone = true
		} else {
			// some pods are still running
			jobDone = false
		}
	} else if active+succeeded < replicas {
		if active+succeeded == 0 {
			// it is a new QueueJob, create pods for it
			diff := replicas - active - succeeded

			wait := sync.WaitGroup{}
			wait.Add(int(diff))
			for i := int32(0); i < diff; i++ {
				go func(ix int32) {
					defer wait.Done()
					newPod := buildPod(fmt.Sprintf("%s-%d-%s", qj.Name, ix, generateUUID()), qj.Namespace, qj.Spec.Template, []metav1.OwnerReference{*metav1.NewControllerRef(qj, controllerKind)}, ix)
					for {
						_, err := cc.clients.Core().Pods(newPod.Namespace).Create(newPod)
						if err == nil {
							// Create Pod successfully
							break
						} else {
							// Failed to create Pod, wait a moment and then create it again
							// This is to ensure all pods under the same QueueJob created
							// So gang-scheduling could schedule the QueueJob successfully
							glog.Warningf("Failed to create pod %s for QueueJob %s, err %#v, wait 2 seconds and re-create it", newPod.Name, qj.Name, err)
							time.Sleep(2 * time.Second)
						}
					}
				}(i)
			}
			wait.Wait()
			jobDone = false
		} else if active+succeeded > 0 {
			// the QueueJob replicas is reduce by user, terminated all pods for gang scheduling
			// and re-create pods for the queuejob in next loop
			jobDone = false
			// TODO(jinzhejz): need make sure manage this QueueJob after all old pods are terminated
			err = cc.terminatePodsForQueueJob(qj)
		}
	}

	return jobDone, err
}

func (cc *Controller) terminatePodsForQueueJob(qj *arbv1.QueueJob) error {
	pods, err := cc.getPodsForQueueJob(qj)
	if len(pods) == 0 || err != nil {
		return err
	}

	cc.deletedPodsCounter.set(fmt.Sprintf("%s/%s", qj.Namespace, qj.Name), len(pods))

	wait := sync.WaitGroup{}
	wait.Add(len(pods))
	for _, pod := range pods {
		go func(p *v1.Pod) {
			defer wait.Done()
			err := cc.clients.Core().Pods(p.Namespace).Delete(p.Name, &metav1.DeleteOptions{})
			if err != nil {
				glog.Warning("Fail to delete pod %s for QueueJob %s/%s", p.Name, qj.Namespace, qj.Name)
				cc.deletedPodsCounter.decreaseCounter(fmt.Sprintf("%s/%s", qj.Namespace, qj.Name))
			}
		}(pod)
	}
	wait.Wait()

	return nil
}
