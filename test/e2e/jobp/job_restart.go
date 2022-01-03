/*
 Copyright 2021 The Volcano Authors.

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

package jobp

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	jobctl "volcano.sh/volcano/pkg/controllers/job"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Test job restart", func() {
	It("Retain failed pod on last Retry", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		jobName := "last-retry-job"
		By("create job")
		var minSuccess int32 = 2
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:       jobName,
			MinSuccess: &minSuccess,
			Min:        2,
			Policies: []vcbatch.LifecyclePolicy{
				{Event: v1alpha1.PodEvictedEvent, Action: v1alpha1.RestartJobAction},
				{Event: v1alpha1.PodFailedEvent, Action: v1alpha1.RestartJobAction},
			},
			MaxRetry: 2,
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "running-task",
					Img:           e2eutil.DefaultBusyBoxImage,
					Min:           1,
					Rep:           1,
					Command:       "sleep 1000",
					RestartPolicy: v1.RestartPolicyNever,
					MaxRetry:      1,
				},
				{
					Name:          "failed-task",
					Img:           e2eutil.DefaultBusyBoxImage,
					Min:           1,
					Rep:           1,
					Command:       "sh fake.sh",
					RestartPolicy: v1.RestartPolicyNever,
					MaxRetry:      1,
				},
			},
		})

		// wait job failed
		err := e2eutil.WaitJobStates(ctx, job, []vcbatch.JobPhase{vcbatch.Failed}, e2eutil.FiveMinute)
		Expect(err).NotTo(HaveOccurred())

		// check job restart count
		curjob, err := e2eutil.VcClient.BatchV1alpha1().Jobs(job.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(curjob.Status.RetryCount).Should(Equal(int32(2)))

		// wait running pod deleted
		err = e2eutil.WaitPodGone(ctx, jobctl.MakePodName(jobName, "running-task", 0), job.Namespace)
		Expect(err).NotTo(HaveOccurred())

		// failed pod should be existing
		pod, err := e2eutil.KubeClient.CoreV1().Pods(job.Namespace).Get(context.TODO(), jobctl.MakePodName(jobName, "failed-task", 0), metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.DeletionTimestamp).Should(BeNil())
	})

	It("Retain succeeded pod when job complete", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		jobName := "complete-retry-job"
		By("create job")
		var minSuccess int32 = 2
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:       jobName,
			MinSuccess: &minSuccess,
			Min:        2,
			Policies: []vcbatch.LifecyclePolicy{
				{Event: v1alpha1.PodEvictedEvent, Action: v1alpha1.RestartJobAction},
				{Event: v1alpha1.PodFailedEvent, Action: v1alpha1.RestartJobAction},
			},
			MaxRetry: 1,
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "running-task",
					Img:           e2eutil.DefaultBusyBoxImage,
					Min:           1,
					Rep:           1,
					Command:       "sleep 1000",
					RestartPolicy: v1.RestartPolicyNever,
					MaxRetry:      1,
				},
				{
					Name: "succeeded-task",
					Img:  e2eutil.DefaultBusyBoxImage,
					Policies: []vcbatch.LifecyclePolicy{
						{Event: v1alpha1.TaskCompletedEvent, Action: v1alpha1.CompleteJobAction},
					},
					Min:           1,
					Rep:           1,
					Command:       "sleep 1",
					RestartPolicy: v1.RestartPolicyNever,
					MaxRetry:      1,
				},
			},
		})

		// wait job failed
		err := e2eutil.WaitJobStates(ctx, job, []vcbatch.JobPhase{vcbatch.Completed}, e2eutil.FiveMinute)
		Expect(err).NotTo(HaveOccurred())

		// check job restart count
		curjob, err := e2eutil.VcClient.BatchV1alpha1().Jobs(job.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(curjob.Status.RetryCount).Should(Equal(int32(0)))

		// wait running pod deleted
		err = e2eutil.WaitPodGone(ctx, jobctl.MakePodName(jobName, "running-task", 0), job.Namespace)
		Expect(err).NotTo(HaveOccurred())

		// succeeded pod should be existing
		pod, err := e2eutil.KubeClient.CoreV1().Pods(job.Namespace).Get(context.TODO(), jobctl.MakePodName(jobName, "succeeded-task", 0), metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.DeletionTimestamp).Should(BeNil())
	})
})
