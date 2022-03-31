/*
Copyright 2021 The Volcano Authors.

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

package schedulingbase

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("DRF Test", func() {
	It("drf works", func() {
		Skip("Failed when add yaml, test case may fail in some condition")
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)
		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: rep,
					Rep: rep,
				},
			},
		}
		// tasks in j1-reference take all the cluster resource
		// each replicas request 1 CPU
		job.Name = "j1-reference"
		referenceJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, referenceJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		// tasks in j2-drf request half of the cluster resource
		// each replicas request 0.5 CPU
		// drf works to make j2-drf preempt the cluster resource
		job.Name = "j2-drf"
		job.Tasks[0].Req = e2eutil.HalfCPU
		backfillJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, backfillJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
	})
})
