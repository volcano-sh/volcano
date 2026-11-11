/*
Copyright 2026 The Volcano Authors.

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

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestHandleJobFlowLeavesCachedJobFlowUnchanged(t *testing.T) {
	fakeController := newFakeController()
	namespace := "default"

	jobFlow := &jobflowv1alpha1.JobFlow{
		ObjectMeta: metav1.ObjectMeta{Name: "jobflow", Namespace: namespace},
		Spec: jobflowv1alpha1.JobFlowSpec{
			Flows: []jobflowv1alpha1.Flow{{Name: "a"}},
		},
	}
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getJobName(jobFlow.Name, "a"),
			Namespace: namespace,
			Labels:    map[string]string{CreatedByJobFlow: GenerateObjectString(namespace, jobFlow.Name)},
		},
		Status: v1alpha1.JobStatus{State: v1alpha1.JobState{Phase: v1alpha1.Running}},
	}
	if err := fakeController.jobInformer.Informer().GetIndexer().Add(job); err != nil {
		t.Fatalf("add job to the informer: %v", err)
	}
	if err := fakeController.jobFlowInformer.Informer().GetIndexer().Add(jobFlow); err != nil {
		t.Fatalf("add jobflow to the informer: %v", err)
	}

	err := fakeController.handleJobFlow(&apis.FlowRequest{
		Namespace:   namespace,
		JobFlowName: jobFlow.Name,
		Action:      jobflowv1alpha1.SyncJobFlowAction,
	})
	if err == nil {
		t.Fatal("expected the status update to fail")
	}

	cached, err := fakeController.jobFlowLister.JobFlows(namespace).Get(jobFlow.Name)
	if err != nil {
		t.Fatalf("get jobflow from the lister: %v", err)
	}
	if !equality.Semantic.DeepEqual(cached.Status, jobflowv1alpha1.JobFlowStatus{}) {
		t.Errorf("handleJobFlow changed the cached JobFlow status to %+v", cached.Status)
	}
}
