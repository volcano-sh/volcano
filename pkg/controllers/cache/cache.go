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

package cache

import (
	"fmt"
	"sync"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

type jobCache struct {
	sync.Mutex

	jobs        map[string]*apis.JobInfo
	deletedJobs workqueue.RateLimitingInterface
}

func keyFn(ns, name string) string {
	return fmt.Sprintf("%s/%s", ns, name)
}

func JobKeyByName(namespace string, name string) string {
	return keyFn(namespace, name)
}

func JobKeyByReq(req *apis.Request) string {
	return keyFn(req.Namespace, req.JobName)
}

func JobKey(job *v1alpha1.Job) string {
	return keyFn(job.Namespace, job.Name)
}

func jobTerminated(job *apis.JobInfo) bool {
	return job.Job == nil && len(job.Pods) == 0
}

func jobKeyOfPod(pod *v1.Pod) (string, error) {
	jobName, found := pod.Annotations[v1alpha1.JobNameKey]
	if !found {
		return "", fmt.Errorf("failed to find job name of pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	return keyFn(pod.Namespace, jobName), nil
}

func New() Cache {
	return &jobCache{
		jobs:        map[string]*apis.JobInfo{},
		deletedJobs: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

func (jc *jobCache) Get(key string) (*apis.JobInfo, error) {
	jc.Lock()
	defer jc.Unlock()

	job, found := jc.jobs[key]
	if !found {
		return nil, fmt.Errorf("failed to find job <%s>", key)
	}

	if job.Job == nil {
		return nil, fmt.Errorf("job <%s> is not ready", key)
	}

	return job.Clone(), nil
}

func (jc *jobCache) GetStatus(key string) (*v1alpha1.JobStatus, error) {
	jc.Lock()
	defer jc.Unlock()

	job, found := jc.jobs[key]
	if !found {
		return nil, fmt.Errorf("failed to find job <%s>", key)
	}

	status := job.Job.Status

	return &status, nil
}

func (jc *jobCache) Add(job *v1alpha1.Job) error {
	jc.Lock()
	defer jc.Unlock()

	key := JobKey(job)
	if jobInfo, found := jc.jobs[key]; found {
		if jobInfo.Job == nil {
			jobInfo.SetJob(job)

			return nil
		}
		return fmt.Errorf("duplicated jobInfo <%v>", key)
	}

	jc.jobs[key] = &apis.JobInfo{
		Name:      job.Name,
		Namespace: job.Namespace,

		Job:  job,
		Pods: make(map[string]map[string]*v1.Pod),
	}

	return nil
}

func (jc *jobCache) Update(obj *v1alpha1.Job) error {
	jc.Lock()
	defer jc.Unlock()

	key := JobKey(obj)
	if job, found := jc.jobs[key]; !found {
		return fmt.Errorf("failed to find job <%v>", key)
	} else {
		job.Job = obj
	}

	return nil
}

func (jc *jobCache) Delete(obj *v1alpha1.Job) error {
	jc.Lock()
	defer jc.Unlock()

	key := JobKey(obj)
	if jobInfo, found := jc.jobs[key]; !found {
		return fmt.Errorf("failed to find job <%v>", key)
	} else {
		jobInfo.Job = nil
		jc.deleteJob(jobInfo)
	}

	return nil
}

func (jc *jobCache) AddPod(pod *v1.Pod) error {
	jc.Lock()
	defer jc.Unlock()

	key, err := jobKeyOfPod(pod)
	if err != nil {
		return err
	}

	job, found := jc.jobs[key]
	if !found {
		job = &apis.JobInfo{
			Pods: make(map[string]map[string]*v1.Pod),
		}
		jc.jobs[key] = job
	}

	return job.AddPod(pod)
}

func (jc *jobCache) UpdatePod(pod *v1.Pod) error {
	jc.Lock()
	defer jc.Unlock()

	key, err := jobKeyOfPod(pod)
	if err != nil {
		return err
	}

	job, found := jc.jobs[key]
	if !found {
		job = &apis.JobInfo{
			Pods: make(map[string]map[string]*v1.Pod),
		}
		jc.jobs[key] = job
	}

	return job.UpdatePod(pod)
}

func (jc *jobCache) DeletePod(pod *v1.Pod) error {
	jc.Lock()
	defer jc.Unlock()

	key, err := jobKeyOfPod(pod)
	if err != nil {
		return err
	}

	job, found := jc.jobs[key]
	if !found {
		job = &apis.JobInfo{
			Pods: make(map[string]map[string]*v1.Pod),
		}
		jc.jobs[key] = job
	}

	if err := job.DeletePod(pod); err != nil {
		return err
	}

	if jc.jobs[key].Job == nil {
		jc.deleteJob(job)
	}

	return nil
}

func (jc *jobCache) Run(stopCh <-chan struct{}) {
	wait.Until(jc.worker, 0, stopCh)
}

func (jc *jobCache) TaskCompleted(jobKey, taskName string) bool {
	jc.Lock()
	defer jc.Unlock()

	var taskReplicas, completed int32

	jobInfo, found := jc.jobs[jobKey]
	if !found {
		return false
	}

	taskPods, found := jobInfo.Pods[taskName]

	if !found {
		return false
	}

	if jobInfo.Job == nil {
		return false
	}

	for _, task := range jobInfo.Job.Spec.Tasks {
		if task.Name == taskName {
			taskReplicas = task.Replicas
		}
	}
	if taskReplicas <= 0 {
		return false
	}

	for _, pod := range taskPods {
		if pod.Status.Phase == v1.PodSucceeded {
			completed += 1
		}
	}
	return completed >= taskReplicas
}

func (jc *jobCache) worker() {
	for jc.processCleanupJob() {
	}
}

func (jc *jobCache) processCleanupJob() bool {
	obj, shutdown := jc.deletedJobs.Get()
	if shutdown {
		return false
	}
	defer jc.deletedJobs.Done(obj)

	job, ok := obj.(*apis.JobInfo)
	if !ok {
		glog.Errorf("failed to convert %v to *apis.JobInfo", obj)
		return true
	}

	jc.Mutex.Lock()
	defer jc.Mutex.Unlock()

	if jobTerminated(job) {
		jc.deletedJobs.Forget(obj)
		key := keyFn(job.Namespace, job.Name)
		delete(jc.jobs, key)
		glog.V(3).Infof("Job <%s> was deleted.", key)
	} else {
		// Retry
		jc.deleteJob(job)
	}
	return true
}

func (jc *jobCache) deleteJob(job *apis.JobInfo) {
	glog.V(3).Infof("Try to delete Job <%v/%v>",
		job.Namespace, job.Name)

	jc.deletedJobs.AddRateLimited(job)
}
