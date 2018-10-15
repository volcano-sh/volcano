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
	. "github.com/onsi/gomega"
)

var _ = Describe("Predicates E2E Test", func() {

	It("Reclaim", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		jobName1 := "n1/qj-1"
		jobName2 := "n2/qj-2"

		slot := oneCPU
		rep := clusterSize(context, slot)

		createJob(context, jobName1, 1, rep, "nginx", slot, nil, nil)
		err := waitJobReady(context, jobName1)
		Expect(err).NotTo(HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			Expect(err).NotTo(HaveOccurred())
		}

		createJob(context, jobName2, 1, rep, "nginx", slot, nil, nil)
		err = waitTasksReady(context, jobName2, expected)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, jobName1, expected)
		Expect(err).NotTo(HaveOccurred())
	})

})
