/*
Copyright 2018 The Kubernetes Authors.
Copyright 2025 The Volcano Authors.

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

package cronjob

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
)

// getJobFromTemplate leaves ObjectMeta.Namespace empty, so the AlreadyExists
// branch of createJob must refetch with the CronJob's namespace.
func TestCreateJobRefetchesConflictingJobInCronJobNamespace(t *testing.T) {
	scheduledTime := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "cj", Namespace: "team-a", UID: "cj-uid"},
	}

	// The job the previous reconcile already created, in the CronJob's namespace.
	existing := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            getJobName(cronJob, scheduledTime),
			Namespace:       cronJob.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cronJob, controllerKind)},
		},
	}

	vcClient := volcanoclient.NewSimpleClientset(existing)
	cc := newFakeController()
	cc.vcClient = vcClient
	cc.jobClient = &realJobClient{}

	got, err := cc.createJob(cronJob, scheduledTime)
	if err != nil {
		t.Fatalf("createJob returned %v, want the existing job", err)
	}
	if got.Namespace != cronJob.Namespace {
		t.Fatalf("refetched job namespace = %q, want %q", got.Namespace, cronJob.Namespace)
	}
}
