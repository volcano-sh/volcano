/*
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

package admission

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Job Validating E2E Test", func() {

	ginkgo.It("Should allow job creation with valid configuration", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("valid-job", testCtx.Namespace)

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject job creation with duplicate task names", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := &v1alpha1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "duplicate-task-job",
				Namespace: testCtx.Namespace,
			},
			Spec: v1alpha1.JobSpec{
				MinAvailable: 1,
				Queue:        "default",
				Tasks: []v1alpha1.TaskSpec{
					{
						Name:     "duplicated-task",
						Replicas: 1,
						Template: createPodTemplate(),
					},
					{
						Name:     "duplicated-task",
						Replicas: 1,
						Template: createPodTemplate(),
					},
				},
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("duplicated task name"))
	})

	ginkgo.It("Should reject job creation with invalid minAvailable less than zero", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-min-available-job", testCtx.Namespace)
		job.Spec.MinAvailable = -1

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("job 'minAvailable' must be >= 0"))
	})

	ginkgo.It("Should reject job creation with invalid maxRetry less than zero", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-max-retry-job", testCtx.Namespace)
		job.Spec.MaxRetry = -1

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("'maxRetry' cannot be less than zero"))
	})

	ginkgo.It("Should reject job creation with invalid TTLSecondsAfterFinished less than zero", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-ttl-job", testCtx.Namespace)
		invalidTTL := int32(-1)
		job.Spec.TTLSecondsAfterFinished = &invalidTTL

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("'ttlSecondsAfterFinished' cannot be less than zero"))
	})

	ginkgo.It("Should reject job creation with no tasks specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := &v1alpha1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "no-task-job",
				Namespace: testCtx.Namespace,
			},
			Spec: v1alpha1.JobSpec{
				MinAvailable: 1,
				Queue:        "default",
				Tasks:        []v1alpha1.TaskSpec{},
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("No task specified in job spec"))
	})

	ginkgo.It("Should reject job creation with task replicas less than zero", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-replica-job", testCtx.Namespace)
		job.Spec.Tasks[0].Replicas = -1

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("'replicas' < 0 in task"))
	})

	ginkgo.It("Should reject job creation with task minAvailable less than zero", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-task-min-available-job", testCtx.Namespace)
		invalidMinAvailable := int32(-1)
		job.Spec.Tasks[0].MinAvailable = &invalidMinAvailable

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("'minAvailable' < 0 in task"))
	})

	ginkgo.It("Should reject job creation with task minAvailable greater than replicas", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-task-min-available-gt-replicas-job", testCtx.Namespace)
		invalidMinAvailable := int32(5)
		job.Spec.Tasks[0].Replicas = 3
		job.Spec.Tasks[0].MinAvailable = &invalidMinAvailable

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("'minAvailable' is greater than 'replicas' in task"))
	})

	ginkgo.It("Should reject job creation with job minAvailable greater than total replicas", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-job-min-available-job", testCtx.Namespace)
		job.Spec.MinAvailable = 5
		job.Spec.Tasks[0].Replicas = 1

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("job 'minAvailable' should not be greater than total replicas"))
	})

	ginkgo.It("Should reject job creation with invalid task name (non-DNS1123)", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-task-name-job", testCtx.Namespace)
		job.Spec.Tasks[0].Name = "Task-1" // Uppercase not allowed

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("a lowercase RFC 1123 label must consist of lower case alphanumeric characters"))
	})

	ginkgo.It("Should reject job creation with duplicate policy events", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("duplicate-policy-job", testCtx.Namespace)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Event:  busv1alpha1.PodFailedEvent,
				Action: busv1alpha1.AbortJobAction,
			},
			{
				Event:  busv1alpha1.PodFailedEvent,
				Action: busv1alpha1.RestartJobAction,
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("duplicate"))
	})

	ginkgo.It("Should reject job creation with policy event and exit code specified simultaneously", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-policy-event-exitcode-job", testCtx.Namespace)
		exitCode := int32(1)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Event:    busv1alpha1.PodFailedEvent,
				Action:   busv1alpha1.AbortJobAction,
				ExitCode: &exitCode,
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("must not specify event and exitCode simultaneously"))
	})

	ginkgo.It("Should reject job creation with policy having neither event nor exit code", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-policy-no-event-no-exitcode-job", testCtx.Namespace)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Action: busv1alpha1.AbortJobAction,
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("either event and exitCode should be specified"))
	})

	ginkgo.It("Should reject job creation with invalid policy action", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-policy-action-job", testCtx.Namespace)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Event:  busv1alpha1.PodEvictedEvent,
				Action: busv1alpha1.Action("InvalidAction"),
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("invalid policy action"))
	})

	ginkgo.It("Should reject job creation with policy exit code zero", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-policy-exitcode-zero-job", testCtx.Namespace)
		exitCode := int32(0)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Action:   busv1alpha1.AbortJobAction,
				ExitCode: &exitCode,
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("0 is not a valid error code"))
	})

	ginkgo.It("Should reject job creation with duplicate policy exit codes", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("duplicate-policy-exitcode-job", testCtx.Namespace)
		exitCode1 := int32(1)
		exitCode2 := int32(1)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Action:   busv1alpha1.AbortJobAction,
				ExitCode: &exitCode1,
			},
			{
				Action:   busv1alpha1.RestartJobAction,
				ExitCode: &exitCode2,
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("duplicate exitCode"))
	})

	ginkgo.It("Should reject job creation with AnyEvent policy and other event policies", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-any-event-policy-job", testCtx.Namespace)
		job.Spec.Policies = []v1alpha1.LifecyclePolicy{
			{
				Event:  busv1alpha1.AnyEvent,
				Action: busv1alpha1.AbortJobAction,
			},
			{
				Event:  busv1alpha1.PodFailedEvent,
				Action: busv1alpha1.RestartJobAction,
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("if there's * here, no other policy should be here"))
	})

	ginkgo.It("Should reject job creation with invalid volume mount path", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-volume-mount-job", testCtx.Namespace)
		job.Spec.Volumes = []v1alpha1.VolumeSpec{
			{
				MountPath: "", // Empty mount path
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("mountPath is required"))
	})

	ginkgo.It("Should reject job creation with duplicate volume mount paths", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("duplicate-volume-mount-job", testCtx.Namespace)
		job.Spec.Volumes = []v1alpha1.VolumeSpec{
			{
				MountPath:       "/var",
				VolumeClaimName: "pvc1",
			},
			{
				MountPath:       "/var",
				VolumeClaimName: "pvc2",
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("duplicated mountPath"))
	})

	ginkgo.It("Should reject job creation with volume without claim name or claim spec", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-volume-no-claim-job", testCtx.Namespace)
		job.Spec.Volumes = []v1alpha1.VolumeSpec{
			{
				MountPath: "/var",
				// Neither VolumeClaimName nor VolumeClaim specified
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("either VolumeClaim or VolumeClaimName must be specified"))
	})

	ginkgo.It("Should allow job creation with valid task dependencies (DAG)", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := &v1alpha1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-dag-job",
				Namespace: testCtx.Namespace,
			},
			Spec: v1alpha1.JobSpec{
				MinAvailable: 1,
				Queue:        "default",
				Tasks: []v1alpha1.TaskSpec{
					{
						Name:     "t1",
						Replicas: 1,
						DependsOn: &v1alpha1.DependsOn{
							Name: []string{"t2"},
						},
						Template: createPodTemplate(),
					},
					{
						Name:     "t2",
						Replicas: 1,
						Template: createPodTemplate(),
					},
				},
			},
		}

		_, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow job update with valid changes to minAvailable and replicas", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("update-job", testCtx.Namespace)
		createdJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Update minAvailable and task replicas with retry on conflict
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Get(context.TODO(), createdJob.Name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Update minAvailable and task replicas
			latestJob.Spec.MinAvailable = 3
			latestJob.Spec.Tasks[0].Replicas = 5

			_, updateErr = testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Update(context.TODO(), latestJob, metav1.UpdateOptions{})
			// If we get a conflict, retry with fresh object
			if errors.IsConflict(updateErr) {
				continue
			}
			// For any other error or success, break
			break
		}
		gomega.Expect(updateErr).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject job update with invalid minAvailable greater than total replicas", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("invalid-update-job", testCtx.Namespace)
		createdJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Retry update with fresh object on conflict, but expect validation error
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Get(context.TODO(), createdJob.Name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Set minAvailable greater than replicas
			latestJob.Spec.MinAvailable = 10
			latestJob.Spec.Tasks[0].Replicas = 5

			_, updateErr = testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Update(context.TODO(), latestJob, metav1.UpdateOptions{})
			// If we get a conflict, retry with fresh object
			if errors.IsConflict(updateErr) {
				continue
			}
			// For any other error (including validation errors), break and check
			break
		}

		// We expect an error containing our validation message
		gomega.Expect(updateErr).To(gomega.HaveOccurred())
		gomega.Expect(updateErr.Error()).To(gomega.ContainSubstring("not be greater than total replicas"))
	})

	ginkgo.It("Should reject job update attempting to add new tasks", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("add-task-update-job", testCtx.Namespace)
		createdJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Retry update with fresh object on conflict, but expect validation error
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Get(context.TODO(), createdJob.Name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Try to add a new task
			newTask := v1alpha1.TaskSpec{
				Name:     "new-task",
				Replicas: 2,
				Template: createPodTemplate(),
			}
			latestJob.Spec.Tasks = append(latestJob.Spec.Tasks, newTask)

			_, updateErr = testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Update(context.TODO(), latestJob, metav1.UpdateOptions{})
			// If we get a conflict, retry with fresh object
			if errors.IsConflict(updateErr) {
				continue
			}
			// For any other error (including validation errors), break and check
			break
		}

		// We expect an error containing our validation message
		gomega.Expect(updateErr).To(gomega.HaveOccurred())
		gomega.Expect(updateErr.Error()).To(gomega.ContainSubstring("job updates may not add or remove tasks"))
	})

	ginkgo.It("Should reject job update attempting to change queue name", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		job := createBaseJob("change-queue-update-job", testCtx.Namespace)
		createdJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Retry update with fresh object on conflict, but expect validation error
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestJob, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Get(context.TODO(), createdJob.Name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Try to change queue name
			latestJob.Spec.Queue = "another-queue"

			_, updateErr = testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Update(context.TODO(), latestJob, metav1.UpdateOptions{})
			// If we get a conflict, retry with fresh object
			if errors.IsConflict(updateErr) {
				continue
			}
			// For any other error (including validation errors), break and check
			break
		}

		// We expect an error containing our validation message
		gomega.Expect(updateErr).To(gomega.HaveOccurred())
		gomega.Expect(updateErr.Error()).To(gomega.ContainSubstring("job updates may not change fields other than"))
	})
})

// Helper function to create a base valid job
func createBaseJob(name, namespace string) *v1alpha1.Job {
	return &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.JobSpec{
			MinAvailable: 1,
			Queue:        "default",
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:     "task-1",
					Replicas: 1,
					Template: createPodTemplate(),
				},
			},
			Policies: []v1alpha1.LifecyclePolicy{
				{
					Event:  busv1alpha1.PodEvictedEvent,
					Action: busv1alpha1.RestartTaskAction,
				},
			},
		},
	}
}

// Helper function to create a basic pod template
func createPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"name": "test"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "fake-name",
					Image: "busybox:1.24",
				},
			},
		},
	}
}
