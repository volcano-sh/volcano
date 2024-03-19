/*
Copyright 2022 The Volcano Authors.

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

package jobflow

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	v1alpha1flow "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	"volcano.sh/volcano/pkg/controllers/jobflow/state"
)

func (jf *jobflowcontroller) syncJobFlow(jobFlow *v1alpha1flow.JobFlow, updateStateFn state.UpdateJobFlowStatusFn) error {
	klog.V(4).Infof("Begin to sync JobFlow %s.", jobFlow.Name)
	defer klog.V(4).Infof("End sync JobFlow %s.", jobFlow.Name)

	// JobRetainPolicy Judging whether jobs are necessary to delete
	if jobFlow.Spec.JobRetainPolicy == v1alpha1flow.Delete && jobFlow.Status.State.Phase == v1alpha1flow.Succeed {
		if err := jf.deleteAllJobsCreatedByJobFlow(jobFlow); err != nil {
			klog.Errorf("Failed to delete jobs of JobFlow %v/%v: %v",
				jobFlow.Namespace, jobFlow.Name, err)
			return err
		}
		return nil
	}

	// deploy job by dependence order.
	if err := jf.deployJob(jobFlow); err != nil {
		klog.Errorf("Failed to create jobs of JobFlow %v/%v: %v",
			jobFlow.Namespace, jobFlow.Name, err)
		return err
	}

	// update jobFlow status
	jobFlowStatus, err := jf.getAllJobStatus(jobFlow)
	if err != nil {
		return err
	}
	jobFlow.Status = *jobFlowStatus
	updateStateFn(&jobFlow.Status, len(jobFlow.Spec.Flows))
	_, err = jf.vcClient.FlowV1alpha1().JobFlows(jobFlow.Namespace).UpdateStatus(context.Background(), jobFlow, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of JobFlow %v/%v: %v",
			jobFlow.Namespace, jobFlow.Name, err)
		return err
	}

	return nil
}

func (jf *jobflowcontroller) deployJob(jobFlow *v1alpha1flow.JobFlow) error {
	// load jobTemplate by flow and deploy it
	for _, flow := range jobFlow.Spec.Flows {
		jobName := getJobName(jobFlow.Name, flow.Name)
		if _, err := jf.jobLister.Jobs(jobFlow.Namespace).Get(jobName); err != nil {
			if errors.IsNotFound(err) {
				// If it is not distributed, judge whether the dependency of the VcJob meets the requirements
				if flow.DependsOn == nil || flow.DependsOn.Targets == nil {
					if err := jf.createJob(jobFlow, flow); err != nil {
						return err
					}
				} else {
					// query whether the dependencies of the job have been met
					flag, err := jf.judge(jobFlow, flow)
					if err != nil {
						return err
					}
					if flag {
						if err := jf.createJob(jobFlow, flow); err != nil {
							return err
						}
					}
				}
				continue
			}
			return err
		}
	}
	return nil
}

// judge query whether the dependencies of the job have been met. If it is satisfied, create the job, if not, judge the next job. Create the job if satisfied
func (jf *jobflowcontroller) judge(jobFlow *v1alpha1flow.JobFlow, flow v1alpha1flow.Flow) (bool, error) {
	for _, targetName := range flow.DependsOn.Targets {
		targetJobName := getJobName(jobFlow.Name, targetName)
		job, err := jf.jobLister.Jobs(jobFlow.Namespace).Get(targetJobName)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Info(fmt.Sprintf("No %v Job found！", targetJobName))
				return false, nil
			}
			return false, err
		}
		if job.Status.State.Phase != v1alpha1.Completed {
			return false, nil
		}
	}
	return true, nil
}

// createJob
func (jf *jobflowcontroller) createJob(jobFlow *v1alpha1flow.JobFlow, flow v1alpha1flow.Flow) error {
	job := new(v1alpha1.Job)
	if err := jf.loadJobTemplateAndSetJob(jobFlow, flow.Name, getJobName(jobFlow.Name, flow.Name), job); err != nil {
		return err
	}
	if _, err := jf.vcClient.BatchV1alpha1().Jobs(jobFlow.Namespace).Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	jf.recorder.Eventf(jobFlow, corev1.EventTypeNormal, "Created", fmt.Sprintf("create a job named %v!", job.Name))
	return nil
}

// getAllJobStatus Get the information of all created jobs
func (jf *jobflowcontroller) getAllJobStatus(jobFlow *v1alpha1flow.JobFlow) (*v1alpha1flow.JobFlowStatus, error) {
	jobList, err := jf.getAllJobsCreatedByJobFlow(jobFlow)
	if err != nil {
		klog.Error(err, "get jobList error")
		return nil, err
	}

	statusListJobMap := map[v1alpha1.JobPhase][]string{
		v1alpha1.Pending:     make([]string, 0),
		v1alpha1.Running:     make([]string, 0),
		v1alpha1.Completing:  make([]string, 0),
		v1alpha1.Completed:   make([]string, 0),
		v1alpha1.Terminating: make([]string, 0),
		v1alpha1.Terminated:  make([]string, 0),
		v1alpha1.Failed:      make([]string, 0),
	}

	UnKnowJobs := make([]string, 0)
	conditions := make(map[string]v1alpha1flow.Condition)
	for _, job := range jobList {
		if _, ok := statusListJobMap[job.Status.State.Phase]; ok {
			statusListJobMap[job.Status.State.Phase] = append(statusListJobMap[job.Status.State.Phase], job.Name)
		} else {
			UnKnowJobs = append(UnKnowJobs, job.Name)
		}
		conditions[job.Name] = v1alpha1flow.Condition{
			Phase:           job.Status.State.Phase,
			CreateTimestamp: job.CreationTimestamp,
			RunningDuration: job.Status.RunningDuration,
			TaskStatusCount: job.Status.TaskStatusCount,
		}
	}
	jobStatusList := make([]v1alpha1flow.JobStatus, 0)
	if jobFlow.Status.JobStatusList != nil {
		jobStatusList = jobFlow.Status.JobStatusList
	}
	for _, job := range jobList {
		runningHistories := getRunningHistories(jobStatusList, job)
		endTimeStamp := metav1.Time{}
		if job.Status.RunningDuration != nil {
			endTimeStamp = metav1.Time{Time: job.CreationTimestamp.Add(job.Status.RunningDuration.Duration)}
		}
		jobStatus := v1alpha1flow.JobStatus{
			Name:             job.Name,
			State:            job.Status.State.Phase,
			StartTimestamp:   job.CreationTimestamp,
			EndTimestamp:     endTimeStamp,
			RestartCount:     job.Status.RetryCount,
			RunningHistories: runningHistories,
		}
		jobFlag := true
		for i := range jobStatusList {
			if jobStatusList[i].Name == jobStatus.Name {
				jobFlag = false
				jobStatusList[i] = jobStatus
			}
		}
		if jobFlag {
			jobStatusList = append(jobStatusList, jobStatus)
		}
	}

	jobFlowStatus := v1alpha1flow.JobFlowStatus{
		PendingJobs:    statusListJobMap[v1alpha1.Pending],
		RunningJobs:    statusListJobMap[v1alpha1.Running],
		FailedJobs:     statusListJobMap[v1alpha1.Failed],
		CompletedJobs:  statusListJobMap[v1alpha1.Completed],
		TerminatedJobs: statusListJobMap[v1alpha1.Terminated],
		UnKnowJobs:     UnKnowJobs,
		JobStatusList:  jobStatusList,
		Conditions:     conditions,
		State:          jobFlow.Status.State,
	}
	return &jobFlowStatus, nil
}

func getRunningHistories(jobStatusList []v1alpha1flow.JobStatus, job *v1alpha1.Job) []v1alpha1flow.JobRunningHistory {
	runningHistories := make([]v1alpha1flow.JobRunningHistory, 0)
	flag := true
	for _, jobStatusGet := range jobStatusList {
		if jobStatusGet.Name == job.Name && jobStatusGet.RunningHistories != nil {
			flag = false
			runningHistories = jobStatusGet.RunningHistories
			// State change
			if len(runningHistories) == 0 {
				continue
			}
			if runningHistories[len(runningHistories)-1].State != job.Status.State.Phase {
				runningHistories[len(runningHistories)-1].EndTimestamp = metav1.Time{
					Time: time.Now(),
				}
				runningHistories = append(runningHistories, v1alpha1flow.JobRunningHistory{
					StartTimestamp: metav1.Time{Time: time.Now()},
					EndTimestamp:   metav1.Time{},
					State:          job.Status.State.Phase,
				})
			}
		}
	}
	if flag && job.Status.State.Phase != "" {
		runningHistories = append(runningHistories, v1alpha1flow.JobRunningHistory{
			StartTimestamp: metav1.Time{
				Time: time.Now(),
			},
			EndTimestamp: metav1.Time{},
			State:        job.Status.State.Phase,
		})
	}
	return runningHistories
}

func (jf *jobflowcontroller) loadJobTemplateAndSetJob(jobFlow *v1alpha1flow.JobFlow, flowName string, jobName string, job *v1alpha1.Job) error {
	// load jobTemplate
	jobTemplate, err := jf.jobTemplateLister.JobTemplates(jobFlow.Namespace).Get(flowName)
	if err != nil {
		return err
	}

	*job = v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
			Namespace:   jobFlow.Namespace,
			Labels:      map[string]string{CreatedByJobTemplate: GetTemplateString(jobFlow.Namespace, flowName)},
			Annotations: map[string]string{CreatedByJobTemplate: GetTemplateString(jobFlow.Namespace, flowName)},
		},
		Spec:   jobTemplate.Spec,
		Status: v1alpha1.JobStatus{},
	}

	return controllerutil.SetControllerReference(jobFlow, job, scheme.Scheme)
}

func (jf *jobflowcontroller) deleteAllJobsCreatedByJobFlow(jobFlow *v1alpha1flow.JobFlow) error {
	jobList, err := jf.getAllJobsCreatedByJobFlow(jobFlow)
	if err != nil {
		return err
	}

	for _, job := range jobList {
		err := jf.vcClient.BatchV1alpha1().Jobs(jobFlow.Namespace).Delete(context.Background(), job.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("Failed to delete job of JobFlow %v/%v: %v",
				jobFlow.Namespace, jobFlow.Name, err)
			return err
		}
	}
	return nil
}

func (jf *jobflowcontroller) getAllJobsCreatedByJobFlow(jobFlow *v1alpha1flow.JobFlow) ([]*v1alpha1.Job, error) {
	var flowNames []string
	for _, flow := range jobFlow.Spec.Flows {
		flowNames = append(flowNames, GetTemplateString(jobFlow.Namespace, flow.Name))
	}
	selector := labels.NewSelector()
	r, err := labels.NewRequirement(CreatedByJobTemplate, selection.In, flowNames)
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*r)
	return jf.jobLister.Jobs(jobFlow.Namespace).List(selector)
}
