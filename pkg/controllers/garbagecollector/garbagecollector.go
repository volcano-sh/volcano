/*
Copyright 2019 The Volcano Authors.

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

package garbagecollector

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	batchinformers "volcano.sh/apis/pkg/client/informers/externalversions/batch/v1alpha1"
	batchlisters "volcano.sh/apis/pkg/client/listers/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func init() {
	framework.RegisterController(&gccontroller{})
}

// gccontroller runs reflectors to watch for changes of managed API
// objects. Currently it only watches Jobs. Triggered by Job creation
// and updates, it enqueues Jobs that have non-nil `.spec.ttlSecondsAfterFinished`
// to the `queue`. The gccontroller has workers who consume `queue`, check whether
// the Job TTL has expired or not; if the Job TTL hasn't expired, it will add the
// Job to the queue after the TTL is expected to expire; if the TTL has expired, the
// worker will send requests to the API server to delete the Jobs accordingly.
// This is implemented outside of Job controller for separation of concerns, and
// because it will be extended to handle other finishable resource types.
type gccontroller struct {
	vcClient vcclientset.Interface

	jobInformer batchinformers.JobInformer

	// A store of jobs
	jobLister batchlisters.JobLister
	jobSynced func() bool

	// queues that need to be updated.
	queue workqueue.RateLimitingInterface
}

func (gc *gccontroller) Name() string {
	return "gc-controller"
}

// Initialize creates an instance of gccontroller.
func (gc *gccontroller) Initialize(opt *framework.ControllerOption) error {
	gc.vcClient = opt.VolcanoClient
	jobInformer := informerfactory.NewSharedInformerFactory(gc.vcClient, 0).Batch().V1alpha1().Jobs()

	gc.jobInformer = jobInformer
	gc.jobLister = jobInformer.Lister()
	gc.jobSynced = jobInformer.Informer().HasSynced
	gc.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    gc.addJob,
		UpdateFunc: gc.updateJob,
	})

	return nil
}

// Run starts the worker to clean up Jobs.
func (gc *gccontroller) Run(stopCh <-chan struct{}) {
	defer gc.queue.ShutDown()

	klog.Infof("Starting garbage collector")
	defer klog.Infof("Shutting down garbage collector")

	go gc.jobInformer.Informer().Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, gc.jobSynced) {
		return
	}

	go wait.Until(gc.worker, time.Second, stopCh)

	<-stopCh
}

func (gc *gccontroller) addJob(obj interface{}) {
	job := obj.(*v1alpha1.Job)
	klog.V(4).Infof("Adding job %s/%s", job.Namespace, job.Name)

	if job.DeletionTimestamp == nil && needsCleanup(job) {
		gc.enqueue(job)
	}
}

func (gc *gccontroller) updateJob(old, cur interface{}) {
	job := cur.(*v1alpha1.Job)
	klog.V(4).Infof("Updating job %s/%s", job.Namespace, job.Name)

	if job.DeletionTimestamp == nil && needsCleanup(job) {
		gc.enqueue(job)
	}
}

func (gc *gccontroller) enqueue(job *v1alpha1.Job) {
	klog.V(4).Infof("Add job %s/%s to cleanup", job.Namespace, job.Name)
	key, err := cache.MetaNamespaceKeyFunc(job)
	if err != nil {
		klog.Errorf("couldn't get key for object %#v: %v", job, err)
		return
	}

	gc.queue.Add(key)
}

func (gc *gccontroller) enqueueAfter(job *v1alpha1.Job, after time.Duration) {
	key, err := cache.MetaNamespaceKeyFunc(job)
	if err != nil {
		klog.Errorf("couldn't get key for object %#v: %v", job, err)
		return
	}

	gc.queue.AddAfter(key, after)
}

func (gc *gccontroller) worker() {
	for gc.processNextWorkItem() {
	}
}

func (gc *gccontroller) processNextWorkItem() bool {
	key, quit := gc.queue.Get()
	if quit {
		return false
	}
	defer gc.queue.Done(key)

	err := gc.processJob(key.(string))
	gc.handleErr(err, key)

	return true
}

func (gc *gccontroller) handleErr(err error, key interface{}) {
	if err == nil {
		gc.queue.Forget(key)
		return
	}

	klog.Errorf("error cleaning up Job %v, will retry: %v", key, err)
	gc.queue.AddRateLimited(key)
}

// processJob will check the Job's state and TTL and delete the Job when it
// finishes and its TTL after finished has expired. If the Job hasn't finished or
// its TTL hasn't expired, it will be added to the queue after the TTL is expected
// to expire.
// This function is not meant to be invoked concurrently with the same key.
func (gc *gccontroller) processJob(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Checking if Job %s/%s is ready for cleanup", namespace, name)
	// Ignore the Jobs that are already deleted or being deleted, or the ones that don't need clean up.
	job, err := gc.jobLister.Jobs(namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if expired, err := gc.processTTL(job); err != nil {
		return err
	} else if !expired {
		return nil
	}

	// The Job's TTL is assumed to have expired, but the Job TTL might be stale.
	// Before deleting the Job, do a final sanity check.
	// If TTL is modified before we do this check, we cannot be sure if the TTL truly expires.
	// The latest Job may have a different UID, but it's fine because the checks will be run again.
	fresh, err := gc.vcClient.BatchV1alpha1().Jobs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Use the latest Job TTL to see if the TTL truly expires.
	if expired, err := gc.processTTL(fresh); err != nil {
		return err
	} else if !expired {
		return nil
	}
	// Cascade deletes the Jobs if TTL truly expires.
	policy := metav1.DeletePropagationForeground
	options := metav1.DeleteOptions{
		PropagationPolicy: &policy,
		Preconditions:     &metav1.Preconditions{UID: &fresh.UID},
	}
	klog.V(4).Infof("Cleaning up Job %s/%s", namespace, name)
	return gc.vcClient.BatchV1alpha1().Jobs(fresh.Namespace).Delete(context.TODO(), fresh.Name, options)
}

// processTTL checks whether a given Job's TTL has expired, and add it to the queue after the TTL is expected to expire
// if the TTL will expire later.
func (gc *gccontroller) processTTL(job *v1alpha1.Job) (expired bool, err error) {
	// We don't care about the Jobs that are going to be deleted, or the ones that don't need clean up.
	if job.DeletionTimestamp != nil || !needsCleanup(job) {
		return false, nil
	}

	now := time.Now()
	t, err := timeLeft(job, &now)
	if err != nil {
		return false, err
	}

	// TTL has expired
	if *t <= 0 {
		return true, nil
	}

	gc.enqueueAfter(job, *t)
	return false, nil
}

// needsCleanup checks whether a Job has finished and has a TTL set.
func needsCleanup(j *v1alpha1.Job) bool {
	return j.Spec.TTLSecondsAfterFinished != nil && isJobFinished(j)
}

func isJobFinished(job *v1alpha1.Job) bool {
	return job.Status.State.Phase == v1alpha1.Completed ||
		job.Status.State.Phase == v1alpha1.Failed ||
		job.Status.State.Phase == v1alpha1.Terminated
}

func getFinishAndExpireTime(j *v1alpha1.Job) (*time.Time, *time.Time, error) {
	if !needsCleanup(j) {
		return nil, nil, fmt.Errorf("job %s/%s should not be cleaned up", j.Namespace, j.Name)
	}
	finishAt, err := jobFinishTime(j)
	if err != nil {
		return nil, nil, err
	}
	finishAtUTC := finishAt.UTC()
	expireAtUTC := finishAtUTC.Add(time.Duration(*j.Spec.TTLSecondsAfterFinished) * time.Second)
	return &finishAtUTC, &expireAtUTC, nil
}

func timeLeft(j *v1alpha1.Job, since *time.Time) (*time.Duration, error) {
	finishAt, expireAt, err := getFinishAndExpireTime(j)
	if err != nil {
		return nil, err
	}
	if finishAt.UTC().After(since.UTC()) {
		klog.Warningf("Warning: Found Job %s/%s finished in the future. This is likely due to time skew in the cluster. Job cleanup will be deferred.", j.Namespace, j.Name)
	}
	remaining := expireAt.UTC().Sub(since.UTC())
	klog.V(4).Infof("Found Job %s/%s finished at %v, remaining TTL %v since %v, TTL will expire at %v", j.Namespace, j.Name, finishAt.UTC(), remaining, since.UTC(), expireAt.UTC())
	return &remaining, nil
}

// jobFinishTime takes an already finished Job and returns the time it finishes.
func jobFinishTime(finishedJob *v1alpha1.Job) (metav1.Time, error) {
	if finishedJob.Status.State.LastTransitionTime.IsZero() {
		return metav1.Time{}, fmt.Errorf("unable to find the time when the Job %s/%s finished", finishedJob.Namespace, finishedJob.Name)
	}
	return finishedJob.Status.State.LastTransitionTime, nil
}
