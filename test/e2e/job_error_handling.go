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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	jobutil "volcano.sh/volcano/pkg/controllers/job"
)

var _ = Describe("Job Error Handling", func() {
	It("job level LifecyclePolicy, Event: Any; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "any-restart-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.RestartJobAction,
					Event:  vkv1.AnyEvent,
				},
			},
			tasks: []taskSpec{
				{
					name: "success",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
				},
				{
					name: "delete",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Restarting, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
	})

})
