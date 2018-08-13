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

	arbextv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/extensions/v1alpha1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/utils"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset/versioned"
	arbinformers "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/externalversions"
	extinfov1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/externalversions/extensions/v1alpha1"
	extlisterv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/listers/extensions/v1alpha1"
)

// Controller the Job Controller type
type Controller struct {
	config      *rest.Config
	jobInformer extinfov1.JobInformer
	podInformer coreinformers.PodInformer
	kubeclients *kubernetes.Clientset
	arbclients  *versioned.Clientset

	// A store of jobs
	jobLister extlisterv1.JobLister
	jobSynced func() bool

	// A store of pods, populated by the podController
	podListr  corelisters.PodLister
	podSynced func() bool

	// eventQueue that need to sync up
	eventQueue *cache.FIFO
}

// NewController create new Job Controller
func NewController(config *rest.Config) *Controller {
	cc := &Controller{
		config:      config,
		kubeclients: kubernetes.NewForConfigOrDie(config),
		arbclients:  versioned.NewForConfigOrDie(config),
		eventQueue:  cache.NewFIFO(eventKey),
	}

	cc.jobInformer = arbinformers.NewSharedInformerFactory(cc.arbclients, 0).Extensions().V1alpha1().Jobs()
	cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addJob,
		UpdateFunc: cc.updateJob,
		DeleteFunc: cc.deleteJob,
	})
	cc.jobLister = cc.jobInformer.Lister()
	cc.jobSynced = cc.jobInformer.Informer().HasSynced

	cc.podInformer = informers.NewSharedInformerFactory(cc.kubeclients, 0).Core().V1().Pods()
	cc.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addPod,
		UpdateFunc: cc.updatePod,
		DeleteFunc: cc.deletePod,
	})

	cc.podListr = cc.podInformer.Lister()
	cc.podSynced = cc.podInformer.Informer().HasSynced

	return cc
}

// Run start Job Controller
func (cc *Controller) Run(stopCh <-chan struct{}) {
	go cc.jobInformer.Informer().Run(stopCh)
	go cc.podInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.jobSynced, cc.podSynced)

	go wait.Until(cc.worker, time.Second, stopCh)
}

func (cc *Controller) addJob(obj interface{}) {
	qj, ok := obj.(*arbextv1.Job)
	if !ok {
		glog.Errorf("obj is not Job")
		return
	}

	cc.enqueue(qj)
}

func (cc *Controller) updateJob(oldObj, newObj interface{}) {
	newQJ, ok := newObj.(*arbextv1.Job)
	if !ok {
		glog.Errorf("newObj is not Job")
		return
	}

	cc.enqueue(newQJ)
}

func (cc *Controller) deleteJob(obj interface{}) {
	qj, ok := obj.(*arbextv1.Job)
	if !ok {
		glog.Errorf("obj is not Job")
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

	Jobs, err := cc.jobLister.List(labels.Everything())
	if err != nil {
		glog.Errorf("Failed to list Jobs for Pod %v/%v", pod.Namespace, pod.Name)
	}

	ctl := utils.GetController(pod)
	for _, qj := range Jobs {
		if qj.UID == ctl {
			cc.enqueue(qj)
			break
		}
	}
}

func (cc *Controller) enqueue(obj interface{}) {
	err := cc.eventQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue Job to updateQueue, err %#v", err)
	}
}

func (cc *Controller) worker() {
	if _, err := cc.eventQueue.Pop(func(obj interface{}) error {
		var Job *arbextv1.Job
		switch v := obj.(type) {
		case *arbextv1.Job:
			Job = v
		case *v1.Pod:
			Jobs, err := cc.jobLister.List(labels.Everything())
			if err != nil {
				glog.Errorf("Failed to list Jobs for Pod %v/%v", v.Namespace, v.Name)
			}

			ctl := utils.GetController(v)
			for _, qj := range Jobs {
				if qj.UID == ctl {
					Job = qj
					break
				}
			}

		default:
			glog.Errorf("Un-supported type of %v", obj)
			return nil
		}

		if Job == nil {
			if acc, err := meta.Accessor(obj); err != nil {
				glog.Warningf("Failed to get Job for %v/%v", acc.GetNamespace(), acc.GetName())
			}

			return nil
		}

		// sync Pods for a Job
		if err := cc.syncJob(Job); err != nil {
			glog.Errorf("Failed to sync Job %s, err %#v", Job.Name, err)
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

func (cc *Controller) syncJob(qj *arbextv1.Job) error {
	Job, err := cc.jobLister.Jobs(qj.Namespace).Get(qj.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			glog.V(3).Infof("Job has been deleted: %v", qj.Name)
			return nil
		}
		return err
	}

	pods, err := cc.getPodsForJob(Job)
	if err != nil {
		return err
	}

	return cc.manageJob(Job, pods)
}

func (cc *Controller) getPodsForJob(qj *arbextv1.Job) (map[string][]*v1.Pod, error) {
	pods := map[string][]*v1.Pod{}

	for _, ts := range qj.Spec.TaskSpecs {
		selector, err := metav1.LabelSelectorAsSelector(ts.Selector)
		if err != nil {
			return nil, fmt.Errorf("couldn't convert Job selector: %v", err)
		}

		// List all pods under Job
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

// manageJob is the core method responsible for managing the number of running
// pods according to what is specified in the job.Spec.
func (cc *Controller) manageJob(qj *arbextv1.Job, pods map[string][]*v1.Pod) error {
	var err error

	runningSum := int32(0)
	pendingSum := int32(0)
	succeededSum := int32(0)
	failedSum := int32(0)

	ss, err := cc.arbclients.Scheduling().PodGroups(qj.Namespace).List(metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", qj.Name),
	})

	if len(ss.Items) == 0 {
		schedSpc := createPodGroup(qj)
		_, err := cc.arbclients.Scheduling().PodGroups(qj.Namespace).Create(schedSpc)
		if err != nil {
			glog.Errorf("Failed to create PodGroup for Job %v/%v: %v",
				qj.Namespace, qj.Name, err)
		}
	} else {
		glog.V(3).Infof("There's %v PodGroup for Job %v/%v",
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

		glog.V(3).Infof("There are %d pods of Job %s (%s): replicas %d, pending %d, running %d, succeeded %d, failed %d",
			len(pods), qj.Name, name, replicas, pending, running, succeeded, failed)

		// Create pod if necessary
		if diff := replicas - pending - running - succeeded; diff > 0 {
			glog.V(3).Infof("Try to create %v Pods for Job %v/%v", diff, qj.Namespace, qj.Name)

			var errs []error
			wait := sync.WaitGroup{}
			wait.Add(int(diff))
			for i := int32(0); i < diff; i++ {
				go func(ix int32) {
					defer wait.Done()
					newPod := createJobPod(qj, &ts.Template, ix)
					_, err := cc.kubeclients.Core().Pods(newPod.Namespace).Create(newPod)
					if err != nil {
						// Failed to create Pod, wait a moment and then create it again
						// This is to ensure all pods under the same Job created
						// So gang-scheduling could schedule the Job successfully
						glog.Errorf("Failed to create pod %s for Job %s, err %#v",
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

	qj.Status = arbextv1.JobStatus{
		Pending:      pendingSum,
		Running:      runningSum,
		Succeeded:    succeededSum,
		Failed:       failedSum,
		MinAvailable: int32(qj.Spec.MinAvailable),
	}

	// TODO(k82cn): replaced it with `UpdateStatus`
	if _, err := cc.arbclients.Extensions().Jobs(qj.Namespace).Update(qj); err != nil {
		glog.Errorf("Failed to update status of Job %v/%v: %v",
			qj.Namespace, qj.Name, err)
		return err
	}

	return err
}
