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
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	ctlJob "hpw.cloud/volcano/pkg/cli/job"
	"k8s.io/apimachinery/pkg/util/uuid"
	"strconv"
)

var _ = Describe("Job E2E Test: List Job Command", func() {
	It("List running jobs", func() {
		jobName := fmt.Sprintf("RandomName_%s", uuid.NewUUID())
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

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
		outputs := ListJobs(namespace)
		jobs := ParseListOutput(outputs, jobName)
		Expect(strconv.Atoi(jobs[0][ctlJob.Running])).Should(
			BeNumerically(">=", 1), "There should be at least one pod running")
		Expect(jobs[0][ctlJob.Phase]).To(
			Equal(v1alpha1.Running), fmt.Sprintf("Job Phase should be: %s", v1alpha1.Running))

	})
})
