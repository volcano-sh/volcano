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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletapi "k8s.io/kubernetes/pkg/kubelet/apis"
	"k8s.io/kubernetes/pkg/scheduler/api"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

var _ = Describe("Job Life Cycle", func() {
	It("Delete job that is pending state", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "pending-delete-job",
			tasks: []taskSpec{
				{
					name: "success",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
					req:  cpuResource("10000"),
				},
			},
		})

		// job phase: pending
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Inqueue state", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "inqueue-delete-job",
			tasks: []taskSpec{
				{
					name: "success",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
					affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      api.NodeFieldSelectorKeyNodeName,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"test"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		})

		// job phase: pending -> inqueue
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Running state", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "running-delete-job",
			tasks: []taskSpec{
				{
					name: "success",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
				},
			},
		})

		// job phase: pending -> Inqueue -> running
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Completed state", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "complete-delete-job",
			tasks: []taskSpec{
				{
					name: "completed-task",
					img:  defaultBusyBoxImage,
					min:  2,
					rep:  2,
					//Sleep 5 seconds ensure job in running state
					command: "sleep 5",
				},
			},
		})

		// job phase: pending -> running -> Completed
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Completed})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Failed job", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-delete-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.AbortJobAction,
					Event:  vkv1.PodFailedEvent,
				},
			},
			tasks: []taskSpec{
				{
					name:          "fail",
					img:           defaultNginxImage,
					min:           1,
					rep:           1,
					command:       "sleep 10s && exit 3",
					restartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Aborted
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Aborted})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is terminated job", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "terminate-delete-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Event:  vkv1.PodFailedEvent,
				},
			},
			tasks: []taskSpec{
				{
					name:          "fail",
					img:           defaultNginxImage,
					min:           1,
					rep:           1,
					command:       "sleep 10s && exit 3",
					restartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Terminated
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Terminated})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Create and Delete job with CPU requirement", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "terminate-delete-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Event:  vkv1.PodFailedEvent,
				},
			},
			tasks: []taskSpec{
				{
					name:          "complete",
					img:           defaultNginxImage,
					min:           1,
					rep:           1,
					command:       "sleep 10s",
					restartPolicy: v1.RestartPolicyNever,
					req:           cpuResource("1"),
				},
			},
		})

		// job phase: pending -> running -> completed
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Completed})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = context.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(job.Name, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(context, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Checking Event Generation for job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		job := createJob(context, &jobSpec{
			name: "terminate-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Event:  vkv1.PodFailedEvent,
				},
			},
			tasks: []taskSpec{
				{
					name:          "complete",
					img:           defaultNginxImage,
					min:           1,
					rep:           1,
					command:       "sleep 10s && xyz",
					restartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		err := waitJobTerminateAction(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Checking Unschedulable Event Generation for job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		nodeName, rep := computeNode(context, oneCPU)

		nodeAffinity := &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      kubeletapi.LabelHostname,
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		}

		job := createJob(context, &jobSpec{
			name: "unschedulable-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Event:  vkv1.PodFailedEvent,
				},
			},
			tasks: []taskSpec{
				{
					name:          "complete",
					img:           defaultNginxImage,
					min:           rep + 1,
					rep:           rep + 1,
					command:       "sleep 10s",
					restartPolicy: v1.RestartPolicyNever,
					req:           cpuResource("1"),
					limit:         cpuResource("1"),
					affinity:      &v1.Affinity{NodeAffinity: nodeAffinity},
				},
			},
		})

		err := waitJobUnschedulable(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

})
