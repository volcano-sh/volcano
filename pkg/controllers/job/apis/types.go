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

package apis

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

type JobInfo struct {
	Namespace string
	Name      string

	Job  *v1alpha1.Job
	// We construct pods map in the style of:
	// |--->Job Name
	// |     |--->Task Name
	// |           |--->Pod Name
	// |                 |---> Version 1 : Pod
	// |                 |---> Version 2 : Pod
	Pods map[string]map[string]map[string]*v1.Pod
}

func (ji *JobInfo) JobAbandoned() bool{
	return ji.Job.Status.State.Version < 0
}

func (ji *JobInfo) JobStarted() bool{
	return ji.Job.Status.State.Version != 0
}

func (ji *JobInfo) StartNewRound(phase v1alpha1.JobPhase, reason, message string) {
	if ji.Job.Status.State.Version < 0 {
		glog.Errorf("Can not start a job while it's in terminated or aborted status.")
	}
	ji.Job.Status.State = v1alpha1.JobState{
		Version: ji.Job.Status.State.Version + 1,
		Phase: phase,
		Reason: reason,
		Message: message,
	}
	glog.Infof("Job <%s/%s> Started a new round %s", ji.Job.Namespace, ji.Job.Name, ji.Job.Status.State)
}

func (ji *JobInfo) AbortCurrentRound(phase v1alpha1.JobPhase, reason, message string) {
	if ji.Job.Status.State.Version <= 0 {
		glog.Errorf("Can not abort a job while it's already finished.")
	}
	ji.Job.Status.State = v1alpha1.JobState{
		Version: ji.Job.Status.State.Version * -1,
		Phase: phase,
		Reason: reason,
		Message: message,
	}
	glog.Infof("Job <%s/%s> Aborted current round %s", ji.Job.Namespace, ji.Job.Name, ji.Job.Status.State)
}

func (ji *JobInfo) ResumeCurrentRound(phase v1alpha1.JobPhase, reason, message string) {
	if ji.Job.Status.State.Version >= 0 {
		glog.Errorf("Can not resume a job while it's already running.")
	}
	ji.Job.Status.State = v1alpha1.JobState{
		Version: ji.Job.Status.State.Version * -1 + 1,
		Phase: phase,
		Reason: reason,
		Message: message,
	}
	glog.Infof("Job <%s/%s> resumed current round %s", ji.Job.Namespace, ji.Job.Name, ji.Job.Status.State)
}

func (ji *JobInfo) Clone() *JobInfo {
	job := &JobInfo{
		Namespace: ji.Namespace,
		Name:      ji.Name,
		Job:       ji.Job.DeepCopy(),

		Pods: make(map[string]map[string]map[string]*v1.Pod),
	}

	for key, pods := range ji.Pods {
		job.Pods[key] = make(map[string]map[string]*v1.Pod)
		for pn, pds := range pods {
			job.Pods[key][pn] = make(map[string]*v1.Pod)
			for version, pod := range pds {
				job.Pods[key][pn][version] = pod
			}
		}
	}

	return job
}

func (ji *JobInfo) SetJob(job *v1alpha1.Job) {
	ji.Name = job.Name
	ji.Namespace = job.Namespace
	ji.Job = job
}

func (ji *JobInfo) AddPod(pod *v1.Pod) error {
	taskName, found := pod.Annotations[v1alpha1.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to find taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}
	jobVersion, found := pod.Annotations[v1alpha1.JobVersion]
	if !found {
		return fmt.Errorf("failed to find jobVersion of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if _, found := ji.Pods[taskName]; !found {
		ji.Pods[taskName] = make(map[string]map[string]*v1.Pod)
	}
	if _, found := ji.Pods[taskName][pod.Name]; !found {
		ji.Pods[taskName][pod.Name] = make(map[string]*v1.Pod)
	}
	if _, found := ji.Pods[taskName][pod.Name][jobVersion]; found{
		return fmt.Errorf("duplicated pod")
	}
	ji.Pods[taskName][pod.Name][jobVersion] = pod

	return nil
}

func (ji *JobInfo) UpdatePod(pod *v1.Pod) error {
	taskName, found := pod.Annotations[v1alpha1.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}
	jobVersion, found := pod.Annotations[v1alpha1.JobVersion]
	if !found {
		return fmt.Errorf("failed to find jobVersion of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if _, found := ji.Pods[taskName]; !found {
		return fmt.Errorf("can not find task %s in cache", taskName)
	}
	if _, found := ji.Pods[taskName][pod.Name]; !found {
		return fmt.Errorf("can not find pod <%s/%s> in cache",
			pod.Namespace, pod.Name)
	}
	if _, found := ji.Pods[taskName][pod.Name][jobVersion]; !found {
		return fmt.Errorf("can not find pod <%s/%s> version: %s in cache",
			pod.Namespace, pod.Name, jobVersion)
	}
	ji.Pods[taskName][pod.Name][jobVersion] = pod

	return nil
}

func (ji *JobInfo) DeletePod(pod *v1.Pod) error {
	taskName, found := pod.Annotations[v1alpha1.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}
	jobVersion, found := pod.Annotations[v1alpha1.JobVersion]
	if !found {
		return fmt.Errorf("failed to find jobVersion of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if pods, found := ji.Pods[taskName]; found {
		if pds, found := ji.Pods[taskName][pod.Name]; found {
			delete(pds, jobVersion)
			if len(pds) == 0 {
				delete(ji.Pods[taskName], pod.Name)
			}
		}
		if len(pods) == 0 {
			delete(ji.Pods, taskName)
		}
	}

	return nil
}

type Request struct {
	Namespace string
	JobName   string
	TaskName  string

	Event  v1alpha1.Event
	Action v1alpha1.Action
	JobVersion int32
}

func (r Request) String() string {
	return fmt.Sprintf(
		"Job: %s/%s, Task:%s, Event:%s, Action:%s, JobVersion: %d",
		r.Namespace, r.JobName, r.TaskName, r.Event, r.Action, r.JobVersion)

}
