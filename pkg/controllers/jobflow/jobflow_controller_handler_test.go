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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestAddJobFlowFunc(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name        string
		jobFlow     *jobflowv1alpha1.JobFlow
		ExpectValue int
	}{
		{
			Name: "AddJobFlow Success",
			jobFlow: &jobflowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "jobflow1",
					Namespace: namespace,
				},
			},
			ExpectValue: 1,
		},
	}
	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			fakeController.addJobFlow(testcase.jobFlow)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}
}

func TestUpdateJobFlowFunc(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name        string
		newJobFlow  *jobflowv1alpha1.JobFlow
		oldJobFlow  *jobflowv1alpha1.JobFlow
		ExpectValue int
	}{
		{
			Name: "UpdateJobFlow Success",
			newJobFlow: &jobflowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "jobflow1",
					Namespace: namespace,
				},
				Spec: jobflowv1alpha1.JobFlowSpec{
					Flows:           nil,
					JobRetainPolicy: jobflowv1alpha1.Delete,
				},
				Status: jobflowv1alpha1.JobFlowStatus{
					State: jobflowv1alpha1.State{
						Phase: jobflowv1alpha1.Succeed,
					},
				},
			},
			oldJobFlow: &jobflowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "jobflow1",
					Namespace:       namespace,
					ResourceVersion: "1223",
				},
				Spec: jobflowv1alpha1.JobFlowSpec{
					Flows:           nil,
					JobRetainPolicy: jobflowv1alpha1.Delete,
				},
				Status: jobflowv1alpha1.JobFlowStatus{
					State: jobflowv1alpha1.State{
						Phase: jobflowv1alpha1.Succeed,
					},
				},
			},
			ExpectValue: 1,
		},
	}
	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			fakeController.updateJobFlow(testcase.oldJobFlow, testcase.newJobFlow)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}
}

func TestUpdateJobFunc(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name        string
		newJob      *batch.Job
		oldJob      *batch.Job
		ExpectValue int
	}{
		{
			Name: "UpdateJob Success",
			newJob: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			oldJob: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "1223",
					OwnerReferences: []metav1.OwnerReference{},
				},
			},
			ExpectValue: 1,
		},
	}
	jobFlow := &jobflowv1alpha1.JobFlow{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "jobflow1",
			Namespace: namespace,
		},
		Spec:   jobflowv1alpha1.JobFlowSpec{},
		Status: jobflowv1alpha1.JobFlowStatus{},
	}
	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			if err := controllerutil.SetControllerReference(jobFlow, testcase.oldJob, scheme.Scheme); err != nil {
				t.Errorf("SetControllerReference error : %s", err.Error())
			}
			if err := controllerutil.SetControllerReference(jobFlow, testcase.newJob, scheme.Scheme); err != nil {
				t.Errorf("SetControllerReference error : %s", err.Error())
			}
			fakeController.updateJob(testcase.oldJob, testcase.newJob)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}
}

func TestEnqueueJobFlow(t *testing.T) {

	namespace := "test"

	req1 := apis.FlowRequest{
		Namespace:   namespace,
		JobFlowName: "name1",

		Action: jobflowv1alpha1.SyncJobFlowAction,
		Event:  jobflowv1alpha1.OutOfSyncEvent,
	}
	req2 := apis.FlowRequest{
		Namespace:   namespace,
		JobFlowName: "name2",

		Action: jobflowv1alpha1.SyncJobFlowAction,
		Event:  jobflowv1alpha1.OutOfSyncEvent,
	}

	testCases := []struct {
		Name        string
		newReq      apis.FlowRequest
		oldReq      apis.FlowRequest
		ExpectValue int
	}{
		{
			Name:        "de-duplicate",
			newReq:      req1,
			oldReq:      req1,
			ExpectValue: 1,
		},
		{
			Name:        "no-deduplicate",
			newReq:      req1,
			oldReq:      req2,
			ExpectValue: 2,
		},
	}

	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			fakeController.enqueueJobFlow(testcase.oldReq)
			fakeController.enqueueJobFlow(testcase.newReq)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}

}
