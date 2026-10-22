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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"

	batchv1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestGetJobsByCronJob(t *testing.T) {
	cronJobUID := types.UID("test-uid-123")
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cronjob",
			Namespace: "default",
			UID:       cronJobUID,
		},
	}

	testCases := []struct {
		name        string
		jobsToAdd   []*batchv1.Job
		expectedLen int
	}{
		{
			name: "matching controller",
			jobsToAdd: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "batch/v1",
								Kind:       "CronJob",
								Name:       "test-cronjob",
								UID:        cronJobUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job2",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "batch/v1",
								Kind:       "CronJob",
								Name:       "test-cronjob",
								UID:        cronJobUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedLen: 2,
		},
		{
			name: "non-matching controller kind",
			jobsToAdd: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job3",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "batch/v1",
								Kind:       "Deployment",
								Name:       "test-cronjob",
								UID:        cronJobUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedLen: 0,
		},
		{
			name: "different namespace job",
			jobsToAdd: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job4",
						Namespace: "other-namespace",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "batch/v1",
								Kind:       "CronJob",
								Name:       "test-cronjob",
								UID:        cronJobUID,
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedLen: 0,
		},
		{
			name: "job without controller reference",
			jobsToAdd: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job5",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "batch/v1",
								Kind:       "CronJob",
								Name:       "test-cronjob",
								UID:        cronJobUID,
							},
						},
					},
				},
			},
			expectedLen: 0,
		},
		{
			name: "jobs with mismatched UIDs",
			jobsToAdd: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job6",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "batch/v1",
								Kind:       "CronJob",
								Name:       "test-cronjob",
								UID:        "different-uid",
								Controller: ptr.To(true),
							},
						},
					},
				},
			},
			expectedLen: 0,
		},
		{
			name:        "empty job list",
			jobsToAdd:   []*batchv1.Job{},
			expectedLen: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			controller := newFakeController()
			for _, job := range tc.jobsToAdd {
				jobCopy := job.DeepCopy()
				if err := controller.jobInformer.Informer().GetIndexer().Add(jobCopy); err != nil {
					t.Fatalf("failed to add job to informer: %v", err)
				}
			}
			jobs, err := controller.getJobsByCronJob(cronJob)
			if err != nil {
				t.Fatalf("getJobsByCronJob returned error: %v", err)
			}
			if len(jobs) != tc.expectedLen {
				t.Errorf("expected %d jobs, got %d", tc.expectedLen, len(jobs))
				for i, job := range jobs {
					t.Logf("job[%d]: %s/%s, OwnerRef: %v", i, job.Namespace, job.Name, job.OwnerReferences)
				}
			}
		})
	}
}
func TestUpdateCronJob(t *testing.T) {
	tests := []struct {
		name                  string
		now                   time.Time
		oldCronJob            *batchv1.CronJob
		newCronJob            *batchv1.CronJob
		changeResourceVersion bool
		expectedDelay         time.Duration
	}{
		{
			name: "spec.template changed",
			now:  justTen(),
			oldCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"volcano": "yes"},
							Annotations: map[string]string{"cronjob": "yes"},
						},
					},
				},
			},
			newCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
			},
			changeResourceVersion: true,
			expectedDelay:         0 * time.Second,
		},
		{
			name: "spec.schedule changed",
			now:  justTen(),
			oldCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					Schedule: "0 * * * *",
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
				Status: batchv1.CronJobStatus{
					LastScheduleTime: &metav1.Time{Time: justTen()},
				},
			},
			newCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					Schedule: "*/1 * * * *",
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
				Status: batchv1.CronJobStatus{
					LastScheduleTime: &metav1.Time{Time: justTen()},
				},
			},
			changeResourceVersion: true,
			expectedDelay:         1*time.Minute + nextScheduleDelta,
		},
		{
			name: "spec.schedule with @every changed - cadence decrease",
			now:  justTen(),
			oldCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					Schedule: "@every 1m",
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
				Status: batchv1.CronJobStatus{
					LastScheduleTime: &metav1.Time{Time: justTen()},
				},
			},
			newCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					Schedule: "@every 3m",
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "foo"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
				Status: batchv1.CronJobStatus{
					LastScheduleTime: &metav1.Time{Time: justTen()},
				},
			},
			changeResourceVersion: true,
			expectedDelay:         3*time.Minute + nextScheduleDelta,
		},
		{
			name: "spec.schedule with @every changed - cadence increase",
			now:  justTen(),
			oldCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					Schedule: "@every 3m",
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
				Status: batchv1.CronJobStatus{
					LastScheduleTime: &metav1.Time{Time: justTen()},
				},
			},
			newCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					Schedule: "@every 1m",
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "foo"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
				Status: batchv1.CronJobStatus{
					LastScheduleTime: &metav1.Time{Time: justTen()},
				},
			},
			changeResourceVersion: true,
			expectedDelay:         1*time.Minute + nextScheduleDelta,
		},
		{
			name: "spec.timeZone not changed",
			now:  justTen(),
			oldCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					TimeZone: stringPtr("Pacific/Honolulu"),
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
			},
			newCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					TimeZone: stringPtr("Pacific/Honolulu"),
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "foo"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
			},
			changeResourceVersion: true,
			expectedDelay:         0 * time.Second,
		},
		{
			name: "spec.timeZone changed",
			now:  justTen(),
			oldCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "b"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
			},
			newCronJob: &batchv1.CronJob{
				Spec: batchv1.CronJobSpec{
					TimeZone: stringPtr("Pacific/Honolulu"),
					JobTemplate: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"a": "foo"},
							Annotations: map[string]string{"x": "y"},
						},
					},
				},
			},
			changeResourceVersion: true,
			expectedDelay:         0 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, _ := setupTestController()
			controller.now = func() time.Time { return tt.now }
			queue := &fakeQueue{
				TypedRateLimitingInterface: workqueue.NewTypedRateLimitingQueueWithConfig(
					workqueue.DefaultTypedControllerRateLimiter[string](),
					workqueue.TypedRateLimitingQueueConfig[string]{
						Name: "test-update-cronjob",
					},
				)}
			controller.queue = queue
			if tt.changeResourceVersion {
				tt.newCronJob.ResourceVersion = "old version"
				tt.newCronJob.ResourceVersion = "new version"
			}
			controller.updateCronJob(tt.oldCronJob, tt.newCronJob)
			if queue.delay.Seconds() != tt.expectedDelay.Seconds() {
				t.Errorf("Expected delay %#v got %#v", tt.expectedDelay.Seconds(), queue.delay.Seconds())
			}
		})
	}
}

func TestProcessFinishedJobs(t *testing.T) {
	now := time.Now()

	testCases := []struct {
		name                   string
		failedJobsHistoryLimit *int32
		activeJobs             []corev1.ObjectReference
		jobs                   []*batchv1.Job
		expectedActiveLen      int
		expectedDeletedJobs    []string
		expectedUpdateStatus   bool
	}{
		{
			name:                   "terminated job should be appended to failedJobs and cleaned up if it exceeds the limit",
			failedJobsHistoryLimit: ptr.To[int32](1),
			activeJobs: []corev1.ObjectReference{
				{
					Name:      "job-terminated-1",
					Namespace: "default",
					UID:       "uid-1",
				},
				{
					Name:      "job-terminated-2",
					Namespace: "default",
					UID:       "uid-2",
				},
			},
			jobs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-terminated-1",
						Namespace:         "default",
						UID:               "uid-1",
						CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
					},
					Status: batchv1.JobStatus{
						State: batchv1.JobState{
							Phase: batchv1.Terminated,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-terminated-2",
						Namespace:         "default",
						UID:               "uid-2",
						CreationTimestamp: metav1.Time{Time: now.Add(-5 * time.Minute)},
					},
					Status: batchv1.JobStatus{
						State: batchv1.JobState{
							Phase: batchv1.Terminated,
						},
					},
				},
			},
			expectedActiveLen:    0,
			expectedDeletedJobs:  []string{"job-terminated-1"},
			expectedUpdateStatus: true,
		},
		{
			name:                   "terminated job is removed from active list but not deleted if within limit",
			failedJobsHistoryLimit: ptr.To[int32](2),
			activeJobs: []corev1.ObjectReference{
				{
					Name:      "job-terminated-1",
					Namespace: "default",
					UID:       "uid-1",
				},
			},
			jobs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-terminated-1",
						Namespace: "default",
						UID:       "uid-1",
					},
					Status: batchv1.JobStatus{
						State: batchv1.JobState{
							Phase: batchv1.Terminated,
						},
					},
				},
			},
			expectedActiveLen:    0,
			expectedDeletedJobs:  nil,
			expectedUpdateStatus: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			controller, _ := setupTestController()
			cronJob := &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cronjob",
					Namespace: "default",
				},
				Spec: batchv1.CronJobSpec{
					FailedJobsHistoryLimit: tc.failedJobsHistoryLimit,
				},
				Status: batchv1.CronJobStatus{
					Active: tc.activeJobs,
				},
			}

			updateStatus := controller.processFinishedJobs(cronJob, tc.jobs)

			if len(cronJob.Status.Active) != tc.expectedActiveLen {
				t.Errorf("expected active list len to be %d, got %d", tc.expectedActiveLen, len(cronJob.Status.Active))
			}

			fakeJobClient := controller.jobClient.(*fakeJobClient)
			if len(fakeJobClient.DeleteJobName) != len(tc.expectedDeletedJobs) {
				t.Errorf("expected deleted jobs %v, got %v", tc.expectedDeletedJobs, fakeJobClient.DeleteJobName)
			} else {
				for i, name := range tc.expectedDeletedJobs {
					if fakeJobClient.DeleteJobName[i] != name {
						t.Errorf("expected deleted job at %d to be %s, got %s", i, name, fakeJobClient.DeleteJobName[i])
					}
				}
			}

			if updateStatus != tc.expectedUpdateStatus {
				t.Errorf("expected updateStatus to be %v, got %v", tc.expectedUpdateStatus, updateStatus)
			}
		})
	}
}

type fakeQueue struct {
	workqueue.TypedRateLimitingInterface[string]
	delay time.Duration
	key   interface{}
}

func (f *fakeQueue) AddAfter(key string, delay time.Duration) {
	f.delay = delay
	f.key = key
}
