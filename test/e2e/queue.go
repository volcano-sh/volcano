/*
Copyright 2018 The Kubernetes Authors.

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
)

var _ = Describe("Predicates E2E Test", func() {
	It("Reclaim", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		createQueues(context)
		defer deleteQueues(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: "nginx",
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		job.name = "q1-qj-1"
		job.queue = "q1"
		_, pg1 := createJob(context, job)
		err := waitPodGroupReady(context, pg1)
		checkError(context, err)

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			checkError(context, err)
		}

		job.name = "q2-qj-2"
		job.queue = "q2"
		_, pg2 := createJob(context, job)
		err = waitTasksReady(context, pg2, expected)
		checkError(context, err)

		err = waitTasksReady(context, pg1, expected)
		checkError(context, err)
	})

})
