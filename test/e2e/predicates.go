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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Predicates E2E Test", func() {

	It("Hostport", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		nn := clusterNodeNumber(context)

		spec := &jobSpec{
			name: "hp-spec",
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					min:      int32(nn),
					req:      oneCPU,
					rep:      int32(nn * 2),
					hostport: 28080,
				},
			},
		}

		job := createJob(context, spec)

		err := waitTasksReady(context, job, nn)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksPending(context, job, nn)
		Expect(err).NotTo(HaveOccurred())
	})

})
