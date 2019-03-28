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
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
)

var _ = Describe("Job E2E Test: Test Job Plugins", func() {
	It("Env Plugin", func() {
		jobName := "job-with-env-plugin"
		namespace := "test"
		taskName := "task"
		foundVolume := false
		context := initTestContext()
		defer cleanupTestContext(context)

		job := createJob(context, &jobSpec{
			namespace: namespace,
			name:      jobName,
			plugins: map[string][]string{
				"env": {},
			},
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: taskName,
				},
			},
		})

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-env", jobName)
		_, err = context.kubeclient.CoreV1().ConfigMaps(namespace).Get(
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := context.kubeclient.CoreV1().Pods(namespace).Get(
			fmt.Sprintf(helpers.TaskNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())
	})

	It("SSh Plugin", func() {
		jobName := "job-with-ssh-plugin"
		namespace := "test"
		taskName := "task"
		foundVolume := false
		context := initTestContext()
		defer cleanupTestContext(context)

		job := createJob(context, &jobSpec{
			namespace: namespace,
			name:      jobName,
			plugins: map[string][]string{
				"ssh": {"--no-root"},
			},
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: taskName,
				},
			},
		})

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-ssh", jobName)
		_, err = context.kubeclient.CoreV1().ConfigMaps(namespace).Get(
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := context.kubeclient.CoreV1().Pods(namespace).Get(
			fmt.Sprintf(helpers.TaskNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())
	})
})
