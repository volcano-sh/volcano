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
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func (jf *jobflowcontroller) enqueue(req apis.FlowRequest) {
	jf.queue.Add(req)
}

func (jf *jobflowcontroller) addJobFlow(obj interface{}) {
	jobFlow, ok := obj.(*jobflowv1alpha1.JobFlow)
	if !ok {
		klog.Errorf("Failed to convert %v to jobFlow", obj)
		return
	}

	// use struct instead of pointer
	req := apis.FlowRequest{
		Namespace:   jobFlow.Namespace,
		JobFlowName: jobFlow.Name,

		Action: jobflowv1alpha1.SyncJobFlowAction,
		Event:  jobflowv1alpha1.OutOfSyncEvent,
	}

	jf.enqueueJobFlow(req)
}

func (jf *jobflowcontroller) updateJobFlow(oldObj, newObj interface{}) {
	oldJobFlow, ok := oldObj.(*jobflowv1alpha1.JobFlow)
	if !ok {
		klog.Errorf("Failed to convert %v to jobflow", oldJobFlow)
		return
	}

	newJobFlow, ok := newObj.(*jobflowv1alpha1.JobFlow)
	if !ok {
		klog.Errorf("Failed to convert %v to jobflow", newJobFlow)
		return
	}

	if newJobFlow.ResourceVersion == oldJobFlow.ResourceVersion {
		return
	}

	//Todo The update operation of JobFlow is reserved for possible future use. The current update operation on JobFlow will not affect the JobFlow process
	if newJobFlow.Status.State.Phase != jobflowv1alpha1.Succeed || newJobFlow.Spec.JobRetainPolicy != jobflowv1alpha1.Delete {
		return
	}

	req := apis.FlowRequest{
		Namespace:   newJobFlow.Namespace,
		JobFlowName: newJobFlow.Name,

		Action: jobflowv1alpha1.SyncJobFlowAction,
		Event:  jobflowv1alpha1.OutOfSyncEvent,
	}

	jf.enqueueJobFlow(req)
}

func (jf *jobflowcontroller) updateJob(oldObj, newObj interface{}) {
	oldJob, ok := oldObj.(*batch.Job)
	if !ok {
		klog.Errorf("Failed to convert %v to vcjob", oldObj)
		return
	}

	newJob, ok := newObj.(*batch.Job)
	if !ok {
		klog.Errorf("Failed to convert %v to vcjob", newObj)
		return
	}

	// Filter out jobs that are not created from volcano jobflow
	if !isControlledBy(newJob, helpers.JobFlowKind) {
		return
	}

	if newJob.ResourceVersion == oldJob.ResourceVersion {
		return
	}

	jobFlowName := getJobFlowNameByJob(newJob)
	if jobFlowName == "" {
		return
	}

	req := apis.FlowRequest{
		Namespace:   newJob.Namespace,
		JobFlowName: jobFlowName,
		Action:      jobflowv1alpha1.SyncJobFlowAction,
		Event:       jobflowv1alpha1.OutOfSyncEvent,
	}

	jf.enqueueJobFlow(req)
}
