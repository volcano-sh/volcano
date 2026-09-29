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

package util

import (
	"context"
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestPodRecreatedAndRunning(t *testing.T) {
	original := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "job-task-0", UID: "old-uid"},
		Spec:       v1.PodSpec{NodeName: "node-0"},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	replacement := original.DeepCopy()
	replacement.UID = "new-uid"
	deletingOriginal := original.DeepCopy()
	now := metav1.Now()
	deletingOriginal.DeletionTimestamp = &now
	deletingReplacement := replacement.DeepCopy()
	deletingReplacement.DeletionTimestamp = deletingOriginal.DeletionTimestamp
	pendingReplacement := replacement.DeepCopy()
	pendingReplacement.Status.Phase = v1.PodPending
	unboundReplacement := replacement.DeepCopy()
	unboundReplacement.Spec.NodeName = ""
	missingUID := replacement.DeepCopy()
	missingUID.UID = ""
	apiErr := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, original.Name, errors.New("denied"))

	// Model the successive observations during deletion and recreation. In
	// particular, neither the old Running Pod nor a same-name Pending Pod is ready.
	steps := []struct {
		name    string
		pod     *v1.Pod
		err     error
		want    bool
		wantErr bool
	}{
		{name: "old pod still running", pod: original},
		{name: "old pod terminating", pod: deletingOriginal},
		{name: "pod not yet recreated", err: apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, original.Name)},
		{name: "replacement pending", pod: pendingReplacement},
		{name: "replacement unbound", pod: unboundReplacement},
		{name: "replacement terminating", pod: deletingReplacement},
		{name: "replacement missing UID", pod: missingUID},
		{name: "replacement running", pod: replacement, want: true},
		{name: "API error", err: apiErr, wantErr: true},
	}

	client := fake.NewClientset()
	var observed *v1.Pod
	var observedErr error
	client.PrependReactor("get", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != original.Namespace || action.(clienttesting.GetAction).GetName() != original.Name {
			t.Fatalf("unexpected GET: %#v", action)
		}
		return true, observed, observedErr
	})
	condition := podRecreatedAndRunning(client.CoreV1().Pods(original.Namespace), original)
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			observed, observedErr = step.pod, step.err
			got, err := condition(context.Background())
			if got != step.want {
				t.Errorf("condition() = %v, want %v", got, step.want)
			}
			if step.wantErr {
				if !errors.Is(err, step.err) {
					t.Errorf("condition() error = %v, want %v", err, step.err)
				}
			} else if err != nil {
				t.Errorf("condition() unexpected error: %v", err)
			}
		})
	}
}
