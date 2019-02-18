/*
Copyright 2017 The Volcano Authors.

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
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job E2E Test: List Job Command", func() {
	It("List running jobs", func() {
		jobName := "qj-1"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := createJob(context, &jobSpec{
			namespace: namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:      "busybox",
					req:      oneCPU,
					min:      1,
					rep:      rep,
					commands: []string{"sleep", "10"},
				},
			},
		})

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		outputs := ListJobs(namespace)
		Expect(outputs).NotTo(Equal(""), "List job output should not be empty")
		Expect(outputs).To(ContainSubstring(jobName),
			fmt.Sprintf("Job %s should in list job command result", jobName))

	})
})
