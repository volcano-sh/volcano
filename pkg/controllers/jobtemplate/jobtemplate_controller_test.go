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

package jobtemplate

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestHandleJobTemplateLeavesCachedJobTemplateUnchanged(t *testing.T) {
	fakeController := newFakeController()
	namespace := "default"

	jobTemplate := &jobflowv1alpha1.JobTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "jobtemplate", Namespace: namespace},
	}
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job",
			Namespace: namespace,
			Labels:    map[string]string{CreatedByJobTemplate: GetTemplateString(namespace, jobTemplate.Name)},
		},
	}
	if err := fakeController.jobInformer.Informer().GetIndexer().Add(job); err != nil {
		t.Fatalf("add job to the informer: %v", err)
	}
	if err := fakeController.jobTemplateInformer.Informer().GetIndexer().Add(jobTemplate); err != nil {
		t.Fatalf("add jobtemplate to the informer: %v", err)
	}

	err := fakeController.handleJobTemplate(&apis.FlowRequest{
		Namespace:       namespace,
		JobTemplateName: jobTemplate.Name,
		Action:          jobflowv1alpha1.SyncJobTemplateAction,
	})
	if err == nil {
		t.Fatal("expected the status update to fail")
	}

	cached, err := fakeController.jobTemplateLister.JobTemplates(namespace).Get(jobTemplate.Name)
	if err != nil {
		t.Fatalf("get jobtemplate from the lister: %v", err)
	}
	if len(cached.Status.JobDependsOnList) != 0 {
		t.Errorf("handleJobTemplate changed the cached JobTemplate status to %+v", cached.Status)
	}
}
