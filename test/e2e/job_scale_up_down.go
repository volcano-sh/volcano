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

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/job/plugins/svc"
)

var _ = Describe("Dynamic Job scale up and down", func() {
	It("Scale up", func() {
		By("init test context")
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		jobName := "scale-up-job"
		By("create job")
		job := createJob(context, &jobSpec{
			name: jobName,
			plugins: map[string][]string{
				"svc": {},
			},
			tasks: []taskSpec{
				{
					name: "default",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
					req:  halfCPU,
				},
			},
		})

		// job phase: pending -> running
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		// scale up
		job.Spec.MinAvailable = 4
		job.Spec.Tasks[0].Replicas = 4
		err = updateJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		// wait for tasks scaled up
		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		// check configmap updated
		pluginName := fmt.Sprintf("%s-svc", jobName)
		cm, err := context.kubeclient.CoreV1().ConfigMaps(context.namespace).Get(
			pluginName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		hosts := svc.GenerateHosts(job)
		Expect(hosts).To(Equal(cm.Data))

		// TODO: check others

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Scale down", func() {
		By("init test context")
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		jobName := "scale-down-job"
		By("create job")
		job := createJob(context, &jobSpec{
			name: jobName,
			plugins: map[string][]string{
				"svc": {},
			},
			tasks: []taskSpec{
				{
					name: "default",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
					req:  halfCPU,
				},
			},
		})

		// job phase: pending -> running
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		// scale up
		job.Spec.MinAvailable = 1
		job.Spec.Tasks[0].Replicas = 1
		err = updateJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		// wait for tasks scaled up
		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		// check configmap updated
		pluginName := fmt.Sprintf("%s-svc", jobName)
		cm, err := context.kubeclient.CoreV1().ConfigMaps(context.namespace).Get(
			pluginName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		hosts := svc.GenerateHosts(job)
		Expect(hosts).To(Equal(cm.Data))

		// TODO: check others

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

})
