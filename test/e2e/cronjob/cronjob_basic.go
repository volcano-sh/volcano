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
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("CronJob E2E Test", func() {
	sleepCommand := []string{"sh", "-c", "echo 'CronJob executed at' $(date); sleep 180"}
	normalCommand := []string{"sh", "-c", "echo 'CronJob executed at' $(date)"}
	failCommand := []string{"sh", "-c", "echo 'CronJob executed at' $(date); exit 1"}
	It("Create and schedule basic cronjob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		cronJobName := "test-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.AllowConcurrent, normalCommand)

		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		Eventually(func() bool {
			cj, err := getCronJob(ctx, ctx.Namespace, cronJobName)
			if err != nil {
				return false
			}
			return cj.Status.LastScheduleTime != nil
		}, 3*time.Minute, 10*time.Second).Should(BeTrue())

		By("Ensuring one job created")
		Eventually(func() int {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return 0
			}
			return len(jobs.Items)
		}, 3*time.Minute, 10*time.Second).Should(BeNumerically(">=", 2))

		By("CronJobsScheduledAnnotation annotation exists on job")
		jobs, _ := getJobList(ctx, createdCronJob)
		timeAnnotation := jobs.Items[0].Annotations[v1alpha1.CronJobScheduledTimestampAnnotation]
		_, err = time.Parse(time.RFC3339, timeAnnotation)
		Expect(err).NotTo(HaveOccurred(), "Failed to parse cronjob-scheduled-timestamp annotation")

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("CronJob with suspend functionality", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		suspend := true
		cronJobName := "suspended-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "*/1 * * * *", v1alpha1.AllowConcurrent, normalCommand)
		cronJob.Spec.Suspend = &suspend
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Consistently(func() int {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return -1
			}
			return len(jobs.Items)
		}, 3*time.Minute, 10*time.Second).Should(Equal(0))
		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("CronJob with allow concurrency", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create cronJob")
		cronJobName := "allow-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.AllowConcurrent, sleepCommand)
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Ensuring more than one job in active")
		Eventually(func() int {
			active, err := getActiveNum(ctx, ctx.Namespace, cronJobName)
			if err != nil {
				Fail(fmt.Sprintf("Failed to get active jobs: %v", err))
			}
			return active
		}, 3*time.Minute, 10*time.Second).Should(BeNumerically(">=", 2))

		By("Ensuring at least two jobs in job lister")
		Eventually(func() int {
			active, err := getJobActiveNum(ctx, createdCronJob)
			if err != nil {
				Fail(fmt.Sprintf("Failed to list jobs: %v", err))
			}
			return active
		}, 3*time.Minute, 10*time.Second).Should(BeNumerically(">=", 2))

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("CronJob with forbid concurrency", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create cronJob with ForbidConcurrent policy")
		cronJobName := "forbid-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.ForbidConcurrent, sleepCommand)
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Waiting for first job to start")
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil || len(jobs.Items) == 0 {
				return false
			}
			return true
		}, 3*time.Minute, 5*time.Second).Should(BeTrue())

		By("Ensuring only one job is active at any time")
		Consistently(func() int {
			active, err := getActiveNum(ctx, ctx.Namespace, cronJobName)
			if err != nil {
				Fail(fmt.Sprintf("Failed to get active jobs: %v", err))
			}
			return active
		}, 5*time.Minute, 10*time.Second).Should(Equal(1))

		By("Ensuring only one job is running")
		Consistently(func() int {
			active, err := getJobActiveNum(ctx, createdCronJob)
			if err != nil {
				Fail(fmt.Sprintf("Failed to list jobs: %v", err))
			}
			return active
		}, 5*time.Minute, 10*time.Second).Should(Equal(1))

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("CronJob with replace concurrency", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create cronJob with ReplaceConcurrent policy")
		cronJobName := "replace-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.ReplaceConcurrent, sleepCommand)
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Waiting for first job to start running")
		var firstJob *v1alpha1.Job
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil || len(jobs.Items) == 0 {
				return false
			}
			for _, job := range jobs.Items {
				if !isJobFinished(&job) {
					firstJob = &job
					return true
				}
			}
			return false
		}, 3*time.Minute, 5*time.Second).Should(BeTrue())

		firstJobName := firstJob.Name

		By("Waiting for second job to replace the first one")
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return false
			}
			for _, job := range jobs.Items {
				if job.Name != firstJobName {
					return true
				}
			}
			return false
		}, 3*time.Minute, 5*time.Second).Should(BeTrue())

		By("Ensuring first job is being terminated or terminated")
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return false
			}

			for _, job := range jobs.Items {
				if job.Name == firstJobName {
					if job.Status.Terminating > 0 || isJobFinished(&job) {
						return true
					}
					fmt.Printf("First job %s still running: Active=%d, Terminating=%d\n",
						firstJobName, job.Status.Running, job.Status.Terminating)
					return false
				}
			}
			return true
		}, 2*time.Minute, 5*time.Second).Should(BeTrue())

		By("Ensuring only one job is active at any time")
		Consistently(func() int {
			active, err := getActiveNum(ctx, ctx.Namespace, cronJobName)
			if err != nil {
				Fail(fmt.Sprintf("Failed to get active jobs: %v", err))
			}
			return active
		}, 5*time.Minute, 10*time.Second).Should(Equal(1))

		By("Ensuring only one job is running")
		Consistently(func() int {
			active, err := getJobActiveNum(ctx, createdCronJob)
			if err != nil {
				Fail(fmt.Sprintf("Failed to list jobs: %v", err))
			}
			return active
		}, 5*time.Minute, 10*time.Second).Should(Equal(1))

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("many miss schedule", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		cronJobName := "many-miss-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.AllowConcurrent, normalCommand)
		cronJob.CreationTimestamp = metav1.Time{Time: time.Now().Add(-2 * time.Hour)}
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Waiting for first job to be scheduled")
		Eventually(func() bool {
			cj, err := getCronJob(ctx, ctx.Namespace, cronJobName)
			if err != nil {
				return false
			}
			return cj.Status.LastScheduleTime != nil
		}, 2*time.Minute, 10*time.Second).Should(BeTrue())

		By("Waiting for first job to start")
		Eventually(func() int {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return 0
			}
			return len(jobs.Items)
		}, 2*time.Minute, 10*time.Second).Should(BeNumerically(">=", 1))

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})
	It("remove finished job from cronjob active", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		cronJobName := "process-finished-job-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.AllowConcurrent, normalCommand)
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Waiting for first job to be finished")
		var firstFinishedJob v1alpha1.Job
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "ERROR: Failed to get job list: %v\n", err)
				return false
			}
			if len(jobs.Items) == 0 {
				fmt.Fprintf(GinkgoWriter, "DEBUG: No jobs found yet\n")
				return false
			}

			for _, job := range jobs.Items {
				fmt.Fprintf(GinkgoWriter, "DEBUG: Job %s phase: %s\n", job.Name, job.Status.State.Phase)
				if isJobFinished(&job) {
					firstFinishedJob = job
					fmt.Fprintf(GinkgoWriter, "DEBUG: Found finished job: %s (UID: %s)\n", job.Name, job.UID)
					return true
				}
			}
			return false
		}, 10*time.Minute, 10*time.Second).Should(BeTrue())
		By("Ensuring finished job not in cronjob active")
		Eventually(func() bool {
			cronJob, err := getCronJob(ctx, ctx.Namespace, cronJobName)
			if err != nil {
				return false
			}
			activeJobs := cronJob.Status.Active
			for i := range activeJobs {
				if activeJobs[i].UID == firstFinishedJob.UID {
					return false
				}
			}
			return true
		}, 3*time.Minute, 10*time.Second).Should(BeTrue())

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("delete finished and suc jobs with limit", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		cronJobName := "suc-limit-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.AllowConcurrent, normalCommand)
		var sucLimit int32 = 1
		cronJob.Spec.SuccessfulJobsHistoryLimit = &sucLimit
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Waiting for a job suc finished")
		var finishedJob v1alpha1.Job
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "ERROR: Failed to get job list: %v\n", err)
				return false
			}
			if len(jobs.Items) == 0 {
				fmt.Fprintf(GinkgoWriter, "DEBUG: No jobs found yet\n")
				return false
			}
			for _, job := range jobs.Items {
				fmt.Fprintf(GinkgoWriter, "DEBUG: Job %s phase: %s\n", job.Name, job.Status.State.Phase)
				if job.Status.State.Phase == v1alpha1.Completed {
					finishedJob = job
					fmt.Fprintf(GinkgoWriter, "DEBUG: Found suc job: %s (UID: %s)\n", job.Name, job.UID)
					return true
				}
			}
			return false
		}, 3*time.Minute, 10*time.Second).Should(BeTrue())

		By("Ensuring finished job does not exist")
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return false
			}
			for _, job := range jobs.Items {
				if job.Name == finishedJob.Name {
					return false
				}
			}
			return true
		}, 3*time.Minute, 10*time.Second).Should(BeTrue())

		By("Ensuring only one suc finished job exists")
		Consistently(func() int {
			count, err := getJobSucNum(ctx, createdCronJob)
			if err != nil {
				Fail(fmt.Sprintf("Failed to get jobs: %v", err))
			}
			return count
		}, 3*time.Minute, 10*time.Second).Should(Equal(1))

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})

	It("delete finished and fail jobs with limit", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		cronJobName := "fail-limit-cronjob"
		cronJob := createTestCronjob(cronJobName, ctx.Namespace, "* * * * *", v1alpha1.AllowConcurrent, failCommand)
		var failLimit int32 = 1
		cronJob.Spec.FailedJobsHistoryLimit = &failLimit
		createdCronJob, err := createCronJob(ctx, ctx.Namespace, cronJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdCronJob.Name).Should(Equal(cronJobName))

		By("Waiting for a job fail finished")
		var finishedJob v1alpha1.Job
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return false
			}
			for _, job := range jobs.Items {
				if job.Status.State.Phase == v1alpha1.Failed {
					finishedJob = job
					return true
				}
			}
			return false
		}, 3*time.Minute, 10*time.Second).Should(BeTrue())

		By("Ensuring finished job does not exist")
		Eventually(func() bool {
			jobs, err := getJobList(ctx, createdCronJob)
			if err != nil {
				return false
			}
			for _, job := range jobs.Items {
				if job.Name == finishedJob.Name {
					return false
				}
			}
			return true
		}, 3*time.Minute, 10*time.Second).Should(BeTrue())

		By("Ensuring only one fail finished job exists")
		Consistently(func() int {
			count, err := getJobFailNum(ctx, createdCronJob)
			if err != nil {
				Fail(fmt.Sprintf("Failed to get jobs: %v", err))
			}
			return count
		}, 3*time.Minute, 10*time.Second).Should(Equal(1))

		By("Cleaning up test resources")
		err = deleteCronJob(ctx, ctx.Namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred(), "Failed to delete CronJob")
	})
})

func isJobFinished(job *v1alpha1.Job) bool {
	if job.Status.State.Phase == v1alpha1.Completed || job.Status.State.Phase == v1alpha1.Failed || job.Status.State.Phase == v1alpha1.Terminated {
		return true
	}
	return false
}
func createTestCronjob(name, nameSpace, schedule string, concurrency v1alpha1.ConcurrencyPolicy, command []string) *v1alpha1.CronJob {
	return &v1alpha1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: nameSpace,
		},
		Spec: v1alpha1.CronJobSpec{
			Schedule:          schedule,
			ConcurrencyPolicy: concurrency,
			JobTemplate: v1alpha1.JobTemplateSpec{
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "test-task",
							Replicas: 1,
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									Containers: []corev1.Container{
										{
											Name:            "test-container",
											Image:           e2eutil.DefaultBusyBoxImage,
											ImagePullPolicy: v1.PullIfNotPresent,
											Command:         command,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func getJobActiveNum(ctx *e2eutil.TestContext, cronJob *v1alpha1.CronJob) (int, error) {
	jobs, err := getJobList(ctx, cronJob)
	if err != nil {
		return -1, err
	}
	activeCount := 0
	for _, job := range jobs.Items {
		if !isJobFinished(&job) {
			activeCount++
		}
	}
	return activeCount, nil
}

func getJobSucNum(ctx *e2eutil.TestContext, cronJob *v1alpha1.CronJob) (int, error) {
	jobs, err := getJobList(ctx, cronJob)
	if err != nil {
		return -1, err
	}
	sucCount := 0
	for _, job := range jobs.Items {
		if job.Status.State.Phase == v1alpha1.Completed {
			sucCount++
		}
	}
	return sucCount, nil
}
func getJobFailNum(ctx *e2eutil.TestContext, cronJob *v1alpha1.CronJob) (int, error) {
	jobs, err := getJobList(ctx, cronJob)
	if err != nil {
		return -1, err
	}
	failCount := 0
	for _, job := range jobs.Items {
		if job.Status.State.Phase == v1alpha1.Failed {
			failCount++
		}
	}
	return failCount, nil
}

func getActiveNum(ctx *e2eutil.TestContext, ns, name string) (int, error) {
	cronjob, err := getCronJob(ctx, ns, name)
	if err != nil {
		return -1, err
	}
	return len(cronjob.Status.Active), nil
}

func createCronJob(ctx *e2eutil.TestContext, ns string, cronJob *v1alpha1.CronJob) (*v1alpha1.CronJob, error) {
	return ctx.Vcclient.BatchV1alpha1().CronJobs(ns).Create(
		context.TODO(), cronJob, metav1.CreateOptions{})
}
func getCronJob(ctx *e2eutil.TestContext, ns, name string) (*v1alpha1.CronJob, error) {
	return ctx.Vcclient.BatchV1alpha1().CronJobs(ns).Get(
		context.TODO(), name, metav1.GetOptions{})
}
func deleteCronJob(ctx *e2eutil.TestContext, ns, name string) error {
	return ctx.Vcclient.BatchV1alpha1().CronJobs(ns).Delete(
		context.TODO(), name, metav1.DeleteOptions{})
}

func getJobList(ctx *e2eutil.TestContext, cronJob *v1alpha1.CronJob) (*v1alpha1.JobList, error) {
	jobList, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).List(
		context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	var filteredJobs []v1alpha1.Job
	for _, job := range jobList.Items {
		controllerRef := metav1.GetControllerOf(&job)
		if controllerRef == nil {
			continue
		}

		if controllerRef.Kind == "CronJob" &&
			controllerRef.APIVersion == v1alpha1.SchemeGroupVersion.String() &&
			controllerRef.Name == cronJob.Name &&
			controllerRef.UID == cronJob.UID {
			filteredJobs = append(filteredJobs, job)
		}
	}
	return &v1alpha1.JobList{
		Items: filteredJobs,
	}, nil
}
