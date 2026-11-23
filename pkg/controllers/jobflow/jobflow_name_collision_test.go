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
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

// jobflow "a-b" with flows c and d (d depends on c). its job names are a-b-c and
// a-b-d, and a-b-c is also the name jobflow "a" gives its flow "b-c".
func collidingJobFlow(t *testing.T, existing *v1alpha1.Job) (*jobflowcontroller, *jobflowv1alpha1.JobFlow) {
	t.Helper()
	jf := newFakeController()
	for _, name := range []string{"c", "d"} {
		tmpl := &jobflowv1alpha1.JobTemplate{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
		if err := jf.jobTemplateInformer.Informer().GetIndexer().Add(tmpl); err != nil {
			t.Fatal(err)
		}
	}
	if existing != nil {
		if err := jf.jobInformer.Informer().GetIndexer().Add(existing); err != nil {
			t.Fatal(err)
		}
	}
	flow := &jobflowv1alpha1.JobFlow{
		TypeMeta:   metav1.TypeMeta{APIVersion: "flow.volcano.sh/v1alpha1", Kind: "JobFlow"},
		ObjectMeta: metav1.ObjectMeta{Name: "a-b", Namespace: "ns", UID: types.UID("uid-a-b")},
		Spec: jobflowv1alpha1.JobFlowSpec{Flows: []jobflowv1alpha1.Flow{
			{Name: "c"},
			{Name: "d", DependsOn: &jobflowv1alpha1.DependsOn{Targets: []string{"c"}}},
		}},
	}
	return jf, flow
}

func completedJob(name string, owner *metav1.OwnerReference) *v1alpha1.Job {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Status:     v1alpha1.JobStatus{State: v1alpha1.JobState{Phase: v1alpha1.Completed}},
	}
	if owner != nil {
		job.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return job
}

func jobExists(t *testing.T, jf *jobflowcontroller, name string) bool {
	t.Helper()
	_, err := jf.vcClient.BatchV1alpha1().Jobs("ns").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatal(err)
	}
	return err == nil
}

func TestDeployJobIgnoresJobsOfOtherJobFlows(t *testing.T) {
	tests := map[string]*v1alpha1.Job{
		"job of another jobflow": completedJob("a-b-c", &metav1.OwnerReference{
			APIVersion: "flow.volcano.sh/v1alpha1", Kind: "JobFlow", Name: "a",
			UID: types.UID("uid-a"), Controller: ptr.To(true),
		}),
		"job not created by any jobflow": completedJob("a-b-c", nil),
	}
	for name, foreign := range tests {
		t.Run(name, func(t *testing.T) {
			jf, flow := collidingJobFlow(t, foreign)
			if err := jf.deployJob(flow); err == nil {
				t.Errorf("expected an error for job a-b-c, which jobflow a-b does not own")
			}
			if jobExists(t, jf, "a-b-d") {
				t.Errorf("a-b-d was created, but its dependency c never ran; a-b-c belongs to someone else")
			}
		})
	}
}

func TestDeployJobUsesItsOwnCompletedDependency(t *testing.T) {
	own := completedJob("a-b-c", &metav1.OwnerReference{
		APIVersion: "flow.volcano.sh/v1alpha1", Kind: "JobFlow", Name: "a-b",
		UID: types.UID("uid-a-b"), Controller: ptr.To(true),
	})
	jf, flow := collidingJobFlow(t, own)
	if err := jf.deployJob(flow); err != nil {
		t.Fatalf("deployJob: %v", err)
	}
	if !jobExists(t, jf, "a-b-d") {
		t.Errorf("a-b-d was not created although its own dependency a-b-c completed")
	}
}
