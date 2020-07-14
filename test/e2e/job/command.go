/*
Copyright 2019 The Volcano Authors.

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

package job

import (
	"bytes"
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctlJob "volcano.sh/volcano/pkg/cli/job"
	jobUtil "volcano.sh/volcano/pkg/controllers/job"
)

var _ = Describe("Job E2E Test: Test Job Command", func() {
	It("List running jobs", func() {
		var outBuffer bytes.Buffer
		jobName := "test-job"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		})
		//Pod is running
		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		//Job Status is running
		err = waitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		//Command outputs are identical
		outputs := ListJobs(ctx.namespace)
		jobs, err := ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		ctlJob.PrintJobs(jobs, &outBuffer)
		Expect(outputs).To(Equal(outBuffer.String()), "List command result should be:\n %s",
			outBuffer.String())
	})

	It("Suspend running job&Resume aborted job", func() {
		jobName := "test-suspend-running-job"
		taskName := "long-live-task"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					name: taskName,
					img:  defaultNginxImage,
					min:  1,
					rep:  1,
				},
			},
		})
		//Job is running
		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		//Suspend job and wait status change
		SuspendJob(jobName, ctx.namespace)
		err = waitJobStateAborted(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		//Pod is gone
		podName := jobUtil.MakePodName(jobName, taskName, 0)
		err = waitPodGone(ctx, podName, job.Namespace)
		Expect(err).NotTo(HaveOccurred())

		//Resume job
		ResumeJob(jobName, ctx.namespace)

		//Job is running again
		err = waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Suspend pending job", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		jobName := "test-suspend-pending-job"
		taskName := "long-live-task"

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					name: taskName,
					img:  defaultNginxImage,
					req:  cpuResource(fmt.Sprintf("%dm", 1000*rep)),
					min:  1,
					rep:  1,
				},
			},
		})

		//Job is pending
		err := waitJobPending(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStatePending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		//Suspend job and wait status change
		SuspendJob(jobName, ctx.namespace)
		err = waitJobStateAborted(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		//Pod is gone
		podName := jobUtil.MakePodName(jobName, taskName, 0)
		_, err = ctx.kubeclient.CoreV1().Pods(ctx.namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job related pod should be deleted when job aborted.")
	})

	It("delete a job with all nodes taints", func() {

		jobName := "test-del-job"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}

		err := taintAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		})

		err = waitJobPending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		// Pod is running
		err = waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
		// Job Status is running
		err = waitJobStateReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		_, err = ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Delete job
		DeleteJob(jobName, ctx.namespace)

		_, err = ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job should be deleted on vcctl job delete.")
	})
})
