/*
Copyright 2019 The Kubernetes Authors.

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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctlJob "github.com/kubernetes-sigs/volcano/pkg/cli/job"
	jobUtil "github.com/kubernetes-sigs/volcano/pkg/controllers/job"
)

var _ = Describe("Job E2E Test: Test Job Command", func() {
	It("List running jobs", func() {
		var outBuffer bytes.Buffer
		jobName := "test-job"
		namespace := "test"
		context := initTestContext()
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
		jobs, err := context.vkclient.BatchV1alpha1().Jobs(namespace).List(metav1.ListOptions{})
		ctlJob.PrintJobs(jobs, &outBuffer)
		Expect(outputs).To(Equal(outBuffer.String()), "List command result should be:\n %s",
			outBuffer.String())
	})

	It("Suspend running job&Resume aborted job", func() {
		jobName := "test-suspend-running-job"
		taskName := "long-live-task"
		namespace := "test"
		context := initTestContext()
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
		_, err = context.kubeclient.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"Job related pod should be deleted when aborting job.")

		//Resume job
		ResumeJob(jobName, namespace)

		//Job is running again
		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobStateReady(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Suspend pending job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU) * 2

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
})
