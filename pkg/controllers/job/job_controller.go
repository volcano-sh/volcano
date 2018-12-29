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

	kbv1alpha1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	kbext "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinfo "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	kblister "github.com/kubernetes-sigs/kube-batch/pkg/client/listers/scheduling/v1alpha1"

	vnapi "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
	"hpw.cloud/volcano/pkg/client/clientset/versioned"
	informersv1 "hpw.cloud/volcano/pkg/client/informers/externalversions"
	vninfo "hpw.cloud/volcano/pkg/client/informers/externalversions/batch/v1alpha1"
	listersv1 "hpw.cloud/volcano/pkg/client/listers/batch/v1alpha1"
)

// Controller the Job Controller type
type Controller struct {
	config      *rest.Config
	kubeClients *kubernetes.Clientset
	vnClients   *versioned.Clientset
	kbClients   *kbver.Clientset

	jobInformer vninfo.JobInformer
	podInformer coreinformers.PodInformer
	pgInformer  kbinfo.PodGroupInformer

	pgLister kblister.PodGroupLister
	pgSynced func() bool

	// A store of jobs
	jobLister listersv1.JobLister
	jobSynced func() bool

	// A store of pods, populated by the podController
	podListr  corelisters.PodLister
	podSynced func() bool

	// eventQueue that need to sync up
	eventQueue *cache.FIFO
}

// NewJobController create new QueueJob Controller
func NewJobController(config *rest.Config) *Controller {
	cc := &Controller{
		config:      config,
		kubeClients: kubernetes.NewForConfigOrDie(config),
		vnClients:   versioned.NewForConfigOrDie(config),
		kbClients:   kbver.NewForConfigOrDie(config),
		eventQueue:  cache.NewFIFO(eventKey),
	}

	cc.pgInformer = kbext.NewSharedInformerFactory(cc.kbClients, 0).Scheduling().V1alpha1().PodGroups()
	cc.pgLister = cc.pgInformer.Lister()
	cc.pgSynced = cc.pgInformer.Informer().HasSynced

	cc.jobInformer = informersv1.NewSharedInformerFactory(cc.vnClients, 0).Batch().V1alpha1().Jobs()
	cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addJob,
		UpdateFunc: cc.updateJob,
		DeleteFunc: cc.deleteJob,
	})
	cc.jobLister = cc.jobInformer.Lister()
	cc.jobSynced = cc.jobInformer.Informer().HasSynced

	cc.podInformer = informers.NewSharedInformerFactory(cc.kubeClients, 0).Core().V1().Pods()
	cc.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addPod,
		UpdateFunc: cc.updatePod,
		DeleteFunc: cc.deletePod,
	})

	cc.podListr = cc.podInformer.Lister()
	cc.podSynced = cc.podInformer.Informer().HasSynced

	return cc
}

// Run start JobController
func (cc *Controller) Run(stopCh <-chan struct{}) {
	go cc.jobInformer.Informer().Run(stopCh)
	go cc.podInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.jobSynced, cc.podSynced, cc.pgSynced)

	go wait.Until(cc.worker, time.Second, stopCh)
}

func (cc *Controller) worker() {
	if _, err := cc.eventQueue.Pop(func(obj interface{}) error {
		var job *vnapi.Job
		switch v := obj.(type) {
		case *vnapi.Job:
			job = v
		case *v1.Pod:
			jobs, err := cc.jobLister.List(labels.Everything())
			if err != nil {
				glog.Errorf("Failed to list QueueJobs for Pod %v/%v", v.Namespace, v.Name)
			}

			ctl := helpers.GetController(v)
			for _, j := range jobs {
				if j.UID == ctl {
					job = j
					break
				}
			}

		default:
			glog.Errorf("Un-supported type of %v", obj)
			return nil
		}

		if job == nil {
			if acc, err := meta.Accessor(obj); err != nil {
				glog.Warningf("Failed to get QueueJob for %v/%v", acc.GetNamespace(), acc.GetName())
			}

			return nil
		}

		// sync Pods for a QueueJob
		if err := cc.syncJob(job); err != nil {
			glog.Errorf("Failed to sync QueueJob %s, err %#v", job.Name, err)
			// If any error, requeue it.
			return err
		}

		return nil
	}); err != nil {
		glog.Errorf("Fail to pop item from updateQueue, err %#v", err)
		return
	}
}

func (cc *Controller) syncJob(qj *vnapi.Job) error {
	queueJob, err := cc.jobLister.Jobs(qj.Namespace).Get(qj.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			glog.V(3).Infof("Job has been deleted: %v", qj.Name)
			return nil
		}
		return err
	}

	pods, err := cc.getPodsForJob(queueJob)
	if err != nil {
		return err
	}

	return cc.manageJob(queueJob, pods)
}

func (cc *Controller) getPodsForJob(job *vnapi.Job) (map[string][]*v1.Pod, error) {
	pods := map[string][]*v1.Pod{}

	for _, ts := range job.Spec.TaskSpecs {
		selector, err := metav1.LabelSelectorAsSelector(ts.Selector)
		if err != nil {
			return nil, fmt.Errorf("couldn't convert Job selector: %v", err)
		}

		// List all pods under Job
		ps, err := cc.podListr.Pods(job.Namespace).List(selector)
		if err != nil {
			return nil, err
		}

		// TODO (k82cn): optimize by cache
		for _, pod := range ps {
			if !metav1.IsControlledBy(pod, job) {
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
func (cc *Controller) manageJob(job *vnapi.Job, pods map[string][]*v1.Pod) error {
	var err error

	runningSum := int32(0)
	pendingSum := int32(0)
	succeededSum := int32(0)
	failedSum := int32(0)

	// If no PodGroup of this Job, create it.
	if _, err := cc.pgLister.PodGroups(job.Namespace).Get(job.Name); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		pg := &kbv1alpha1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      job.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: kbv1alpha1.PodGroupSpec{
				MinMember: job.Spec.MinAvailable,
			},
		}

		if _, e := cc.kbClients.SchedulingV1alpha1().PodGroups(job.Namespace).Create(pg); e != nil {
			// If failed to create PodGroup, return error.
			return e
		}
	}

	for _, ts := range job.Spec.TaskSpecs {
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
			len(pods), job.Name, name, replicas, pending, running, succeeded, failed)

		// Create pod if necessary
		if diff := replicas - pending - running - succeeded; diff > 0 {
			glog.V(3).Infof("Try to create %v Pods for QueueJob %v/%v", diff, job.Namespace, job.Name)

			var errs []error
			wait := sync.WaitGroup{}
			wait.Add(int(diff))
			for i := int32(0); i < diff; i++ {
				go func(ix int32) {
					defer wait.Done()
					newPod := createJobPod(job, &ts.Template, ix)
					_, err := cc.kubeClients.Core().Pods(newPod.Namespace).Create(newPod)
					if err != nil {
						// Failed to create Pod, wait a moment and then create it again
						// This is to ensure all pods under the same QueueJob created
						// So gang-scheduling could schedule the QueueJob successfully
						glog.Errorf("Failed to create pod %s for QueueJob %s, err %#v",
							newPod.Name, job.Name, err)
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

	job.Status = vnapi.JobStatus{
		Pending:      pendingSum,
		Running:      runningSum,
		Succeeded:    succeededSum,
		Failed:       failedSum,
		MinAvailable: int32(job.Spec.MinAvailable),
	}

	// TODO(k82cn): replaced it with `UpdateStatus`
	if _, err := cc.vnClients.BatchV1alpha1().Jobs(job.Namespace).Update(job); err != nil {
		glog.Errorf("Failed to update status of QueueJob %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}

	return err
}
