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

package e2e

import (
	"bytes"
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
		namespace := "test"
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := createJob(context, &jobSpec{
			namespace: namespace,
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
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		//Job Status is running
		err = waitJobStateReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		//Command outputs are identical
		outputs := ListJobs(namespace)
		jobs, err := context.vcclient.BatchV1alpha1().Jobs(namespace).List(metav1.ListOptions{})
		ctlJob.PrintJobs(jobs, &outBuffer)
		Expect(outputs).To(Equal(outBuffer.String()), "List command result should be:\n %s",
			outBuffer.String())
	})

	It("Suspend running job&Resume aborted job", func() {
		jobName := "test-suspend-running-job"
		taskName := "long-live-task"
		namespace := "test"
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		job := createJob(context, &jobSpec{
			namespace: namespace,
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
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStateReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		//Suspend job and wait status change
		SuspendJob(jobName, namespace)
		err = waitJobStateAborted(context, job)
		Expect(err).NotTo(HaveOccurred())

		//Pod is gone
		podName := jobUtil.MakePodName(jobName, taskName, 0)
		err = waitPodGone(context, podName, job.Namespace)
		Expect(err).NotTo(HaveOccurred())

		//Resume job
		ResumeJob(jobName, namespace)

		//Job is running again
		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStateReady(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Suspend pending job", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		jobName := "test-suspend-pending-job"
		namespace := "test"
		taskName := "long-live-task"

		job := createJob(context, &jobSpec{
			namespace: namespace,
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
		err := waitJobPending(context, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStatePending(context, job)
		Expect(err).NotTo(HaveOccurred())

		//Suspend job and wait status change
		SuspendJob(jobName, namespace)
		err = waitJobStateAborted(context, job)
		Expect(err).NotTo(HaveOccurred())

		//Pod is gone
		podName := jobUtil.MakePodName(jobName, taskName, 0)
		_, err = context.kubeclient.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job related pod should be deleted when job aborted.")
	})

	It("delete a job with all nodes taints", func() {

		jobName := "test-del-job"
		namespace := "test"
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}

		err := taintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		job := createJob(context, &jobSpec{
			namespace: namespace,
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

		err = waitJobPending(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		// Pod is running
		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		// Job Status is running
		err = waitJobStateReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		_, err = context.vcclient.BatchV1alpha1().Jobs(namespace).Get(jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Delete job
		DeleteJob(jobName, namespace)

		_, err = context.vcclient.BatchV1alpha1().Jobs(namespace).Get(jobName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job should be deleted on vcctl job delete.")

	})
})
