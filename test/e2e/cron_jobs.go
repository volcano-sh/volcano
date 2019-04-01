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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

var _ = Describe("Job E2E Test: Test CronJobs", func() {
	It("Create CronJob", func() {
		cronJobName := "cronjob-scheduled"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)

		jobSpec := jobSpec{
			namespace: namespace,
			name:      "inner-job-name",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: 1,
					rep: 1,
				},
			},
		}
		jobObject := generateJobObject(context, &jobSpec)

		cronJob := v1alpha1.CronJob{
			ObjectMeta: v1.ObjectMeta{
				Name:      cronJobName,
				Namespace: namespace,
			},
			Spec: v1alpha1.CronJobSpec{
				Schedule: "@every 10s",
				Template: jobObject.Spec,
			},
		}
		_, err := context.vkclient.BatchV1alpha1().CronJobs(namespace).Create(&cronJob)
		Expect(err).NotTo(HaveOccurred())

		//Wait for job running
		err = waitCronJobLastRunNameNotEmpty(context, namespace, cronJobName)
		Expect(err).NotTo(HaveOccurred())
		lastRunName := getCronJobLastRunName(context, namespace, cronJobName)

		job, err := context.vkclient.BatchV1alpha1().Jobs(namespace).Get(lastRunName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		//Job exist and will complete
		err = waitJobPhases(context, job, []v1alpha1.JobPhase{v1alpha1.Running})
		Expect(err).NotTo(HaveOccurred())
	})
})
