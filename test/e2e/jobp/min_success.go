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

package jobp

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Check min success", func() {
	It("Min Success", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		jobName := "min-success-job"
		By("create job")
		var minSuccess int32 = 2
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:       jobName,
			MinSuccess: &minSuccess,
			Tasks: []e2eutil.TaskSpec{
				{
					Name:    "short",
					Img:     e2eutil.DefaultBusyBoxImage,
					Min:     1,
					Rep:     3,
					Command: "sleep 3",
				},
				{
					Name:    "log",
					Img:     e2eutil.DefaultBusyBoxImage,
					Min:     1,
					Rep:     2,
					Command: "sleep 100000",
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// wait short tasks completed
		err = e2eutil.WaitTasksCompleted(ctx, job, minSuccess)
		Expect(err).NotTo(HaveOccurred())

		// wait job completed
		err = e2eutil.WaitJobStates(ctx, job, []vcbatch.JobPhase{vcbatch.Completed}, e2eutil.OneMinute)
		Expect(err).NotTo(HaveOccurred())
	})
})
