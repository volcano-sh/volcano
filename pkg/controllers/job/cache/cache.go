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

	"hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/controllers/job/apis"

	"k8s.io/api/core/v1"
)

type Cache interface {
	Get(key string) (*apis.JobInfo, error)
	GetStatus(key string) (*v1alpha1.JobStatus, error)
	Add(obj *v1alpha1.Job) error
	Update(obj *v1alpha1.Job) error
	Delete(obj *v1alpha1.Job) error

	AddPod(pod *v1.Pod) error
	UpdatePod(pod *v1.Pod) error
	DeletePod(pod *v1.Pod) error
}

type jobCache struct {
	sync.Mutex

	jobs map[string]*apis.JobInfo
}

func keyFn(ns, name string) string {
	return fmt.Sprintf("%s/%s", ns, name)
}

func JobKeyByReq(req *apis.Request) string {
	return keyFn(req.Namespace, req.JobName)
}

func JobKey(req *v1alpha1.Job) string {
	return keyFn(req.Namespace, req.Name)
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
		jobs: map[string]*apis.JobInfo{},
	}
}

func (jc *jobCache) Get(key string) (*apis.JobInfo, error) {
	jc.Lock()
	defer jc.Unlock()

	job, found := jc.jobs[key]
	if !found {
		return nil, fmt.Errorf("failed to find job <%s>", key)
	}

	jobInfo := &apis.JobInfo{
		Job:  job.Job,
		Pods: make(map[string]map[string]*v1.Pod),
	}

	// Copy Pods.
	for key, pods := range job.Pods {
		jobInfo.Pods[key] = make(map[string]*v1.Pod)
		for pn, pod := range pods {
			jobInfo.Pods[key][pn] = pod
		}
	}

	return jobInfo, nil
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

func (jc *jobCache) Add(obj *v1alpha1.Job) error {
	jc.Lock()
	defer jc.Unlock()

	key := JobKey(obj)
	if _, found := jc.jobs[key]; found {
		return fmt.Errorf("duplicated job <%v>", key)
	}

	jc.jobs[key] = &apis.JobInfo{
		Job:  obj,
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
	if _, found := jc.jobs[key]; !found {
		return fmt.Errorf("failed to find job <%v>", key)
	}

	delete(jc.jobs, key)

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

	taskName, found := pod.Annotations[v1alpha1.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if _, found := job.Pods[taskName]; !found {
		job.Pods[taskName] = make(map[string]*v1.Pod)
	}
	if _, found := job.Pods[taskName][pod.Name]; found {
		return fmt.Errorf("duplicated pod")
	}
	job.Pods[taskName][pod.Name] = pod

	return nil
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

	taskName, found := pod.Annotations[v1alpha1.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if _, found := job.Pods[taskName]; !found {
		job.Pods[taskName] = make(map[string]*v1.Pod)
	}
	job.Pods[taskName][pod.Name] = pod

	return nil
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

	taskName, found := pod.Annotations[v1alpha1.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if pods, found := job.Pods[taskName]; found {
		delete(pods, pod.Name)
	}

	return nil
}
