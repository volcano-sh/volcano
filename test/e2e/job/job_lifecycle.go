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

package job

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
)

var _ = Describe("Job Life Cycle", func() {
	It("Delete job that is pending state", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Running state", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
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

		// job phase: pending -> running
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Completed state", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Failed job", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "failed-delete-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodFailedEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is terminated job", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "terminate-delete-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Create and Delete job with CPU requirement", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "terminate-delete-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Checking Event Generation for job", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		job := createJob(ctx, &jobSpec{
			name: "terminate-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
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

		err := waitJobTerminateAction(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Checking Unschedulable Event Generation for job", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		nodeName, rep := computeNode(ctx, oneCPU)

		nodeAffinity := &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      v1.LabelHostname,
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		}

		job := createJob(ctx, &jobSpec{
			name: "unschedulable-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
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

		err := waitJobUnschedulable(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

})
