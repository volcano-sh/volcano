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

package jobtemplate

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestAddJobTemplateFunc(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name        string
		jobTemplate *jobflowv1alpha1.JobTemplate
		ExpectValue int
	}{
		{
			Name: "AddJobTemplate Success",
			jobTemplate: &jobflowv1alpha1.JobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "jobtemplate1",
					Namespace: namespace,
				},
			},
			ExpectValue: 1,
		},
	}
	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			fakeController.addJobTemplate(testcase.jobTemplate)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}
}

func TestAddJob(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name        string
		job         *batch.Job
		ExpectValue int
	}{
		{
			Name: "AddJob Success",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "job1",
					Namespace:   namespace,
					Labels:      map[string]string{CreatedByJobTemplate: "volcano.sh/createdByJobTemplate"},
					Annotations: map[string]string{CreatedByJobTemplate: "test.jobtemplate1"},
				},
			},
			ExpectValue: 1,
		},
	}
	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			fakeController.addJob(testcase.job)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}
}

func TestEnqueueJobTemplate(t *testing.T) {

	namespace := "test"

	req1 := apis.FlowRequest{
		Namespace:       namespace,
		JobTemplateName: "name1",

		Action: jobflowv1alpha1.SyncJobTemplateAction,
	}
	req2 := apis.FlowRequest{
		Namespace:       namespace,
		JobTemplateName: "name2",

		Action: jobflowv1alpha1.SyncJobTemplateAction,
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

			fakeController.enqueueJobTemplate(testcase.oldReq)
			fakeController.enqueueJobTemplate(testcase.newReq)
			queueLen := fakeController.queue.Len()
			if testcase.ExpectValue != queueLen {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, queueLen)
			}
		})
	}

}
