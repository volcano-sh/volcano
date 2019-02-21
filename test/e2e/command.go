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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	ctlJob "volcano.sh/volcano/pkg/cli/job"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Job E2E Test: List Job Command", func() {
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
					min: 1,
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
		Expect(outputs).To(Equal(outBuffer.String()), "List command result should be %s.",
			outBuffer.String())
	})
})
