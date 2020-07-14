/*
Copyright 2020 The Volcano Authors.

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

package scheduling

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DRF Test", func() {
	It("drf works", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)
		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep,
					rep: rep,
				},
			},
		}
		// tasks in j1-reference take all the cluster resource
		// each replicas request 1 CPU
		job.name = "j1-reference"
		referenceJob := createJob(ctx, job)
		err := waitTasksReady(ctx, referenceJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		// tasks in j2-drf request half of the cluster resource
		// each replicas request 0.5 CPU
		// drf works to make j2-drf preempt the cluster resource
		job.name = "j2-drf"
		job.tasks[0].req = halfCPU
		backfillJob := createJob(ctx, job)
		err = waitTasksReady(ctx, backfillJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
	})
})
