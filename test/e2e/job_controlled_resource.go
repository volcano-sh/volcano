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

var _ = Describe("Job E2E Test: Test Job PVCs", func() {
	It("Generate PVC name if not specified", func() {
		jobName := "job-pvc-name-empty"
		namespace := "test"
		taskName := "task"
		pvcName := "specifiedpvcname"
		context := initTestContext()
		defer cleanupTestContext(context)

		job := createJob(context, &jobSpec{
			namespace: namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: taskName,
				},
			},
			volumes: []v1alpha1.VolumeSpec{
				{
					MountPath:       "/mountone",
					VolumeClaimName: pvcName,
				},
				{
					MountPath: "/mounttwo",
				},
			},
		})

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		job, err = context.vkclient.BatchV1alpha1().Jobs(namespace).Get(jobName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(len(job.Spec.Volumes)).To(Equal(2),
			"Two volumes should be created")
		for _, volume := range job.Spec.Volumes {
			Expect(volume.VolumeClaimName).Should(Or(ContainSubstring(jobName), Equal(pvcName)),
				"PVC name should be generated for manually specified.")
		}
	})
})
