/*
Copyright 2018 The Volcano Authors.

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

var _ = Describe("Queue E2E Test", func() {
	cleanupResources := CleanupResources{}
	var context *context

	BeforeEach(func() {
		context = gContext
	})

	AfterEach(func() {
		deleteResources(gContext, cleanupResources)
	})

	It("Reclaim", func() {
		slot := oneCPU
		rep := clusterSize(context, slot)
		jobName1 := "q1-qj-1"
		jobName2 := "q2-qj-2"
		cleanupResources.Jobs = []string{jobName1, jobName2}

		spec := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		spec.name = jobName1
		spec.queue = "q1"
		job1 := createJob(context, spec)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			Expect(err).NotTo(HaveOccurred())
		}

		spec.name = jobName2
		spec.queue = "q2"
		job2 := createJob(context, spec)
		err = waitTasksReady(context, job2, expected)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1, expected)
		Expect(err).NotTo(HaveOccurred())
	})

})
