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
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	It("Generate PodGroup and valid minResource when creating job", func() {
		jobName := "job-name-podgroup"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)

		resource := v12.ResourceList{
			"cpu":            resource.MustParse("1000m"),
			"memory":         resource.MustParse("1000Mi"),
			"nvidia.com/gpu": resource.MustParse("1"),
		}

		job := createJob(context, &jobSpec{
			namespace: namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:   defaultNginxImage,
					min:   1,
					rep:   1,
					name:  "task-1",
					req:   resource,
					limit: resource,
				},
				{
					img:   defaultNginxImage,
					min:   1,
					rep:   1,
					name:  "task-2",
					req:   resource,
					limit: resource,
				},
			},
		})

		expected := map[string]int64{
			"cpu":            2,
			"memory":         1024 * 1024 * 2000,
			"nvidia.com/gpu": 2,
		}

		err := waitJobStatePending(context, job)
		Expect(err).NotTo(HaveOccurred())

		pGroup, err := context.kbclient.SchedulingV1alpha1().PodGroups(namespace).Get(jobName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		for name, q := range *pGroup.Spec.MinResources {
			value, ok := expected[string(name)]
			Expect(ok).To(Equal(true), "Resource %s should exists in PodGroup", name)
			Expect(q.Value()).To(Equal(value), "Resource %s 's value should equal to %d", name, value)
		}

	})
})
