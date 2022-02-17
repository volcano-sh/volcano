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

package vcctl

import (
	"bytes"
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobcli "volcano.sh/volcano/pkg/cli/job"
	jobctl "volcano.sh/volcano/pkg/controllers/job"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job E2E Test: Test Job Command", func() {
	It("List running jobs", func() {
		var outBuffer bytes.Buffer
		jobName := "test-job"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: e2eutil.OneCPU,
					Min: rep,
					Rep: rep,
				},
			},
		})
		// Pod is running
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		// Job Status is running
		err = e2eutil.WaitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		// Command outputs are identical
		outputs := ListJobs(ctx.Namespace)
		jobs, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		jobcli.PrintJobs(jobs, &outBuffer)
		Expect(outputs).To(Equal(outBuffer.String()), "List command result should be:\n %s",
			outBuffer.String())
	})

	It("Suspend running job&Resume aborted job", func() {
		jobName := "test-suspend-running-job"
		taskName := "long-live-task"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Name: taskName,
					Img:  e2eutil.DefaultNginxImage,
					Min:  1,
					Rep:  1,
				},
			},
		})
		// Job is running
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// Suspend job and wait status change
		SuspendJob(jobName, ctx.Namespace)
		err = e2eutil.WaitJobStateAborted(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// Pod is gone
		podName := jobctl.MakePodName(jobName, taskName, 0)
		err = e2eutil.WaitPodGone(ctx, podName, job.Namespace)
		Expect(err).NotTo(HaveOccurred())

		// Resume job
		ResumeJob(jobName, ctx.Namespace)

		// Job is running again
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Suspend pending job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		jobName := "test-suspend-pending-job"
		taskName := "long-live-task"

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Name: taskName,
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.CpuResource(fmt.Sprintf("%dm", 1000*rep)),
					Min:  1,
					Rep:  1,
				},
			},
		})

		// Job is pending
		err := e2eutil.WaitJobPending(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitJobStatePending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// Suspend job and wait status change
		SuspendJob(jobName, ctx.Namespace)
		err = e2eutil.WaitJobStateAborted(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// Pod is gone
		podName := jobctl.MakePodName(jobName, taskName, 0)
		_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job related pod should be deleted when job aborted.")
	})

	It("delete a job with all nodes taints", func() {

		jobName := "test-del-job"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}

		err := e2eutil.TaintAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: e2eutil.OneCPU,
					Min: rep / 2,
					Rep: rep,
				},
			},
		})

		err = e2eutil.WaitJobPending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.RemoveTaintsFromAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		// Pod is running
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		// Job Status is running
		err = e2eutil.WaitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Delete job
		DeleteJob(jobName, ctx.Namespace)

		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job should be deleted on vcctl job delete.")
	})
})
