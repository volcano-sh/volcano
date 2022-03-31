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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job Life Cycle", func() {
	It("Delete job that is pending state", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "pending-delete-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
					Req:  e2eutil.CpuResource("10000"),
				},
			},
		})

		// job phase: pending
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Running state", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "running-delete-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Completed state", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "complete-delete-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "completed-task",
					Img:  e2eutil.DefaultBusyBoxImage,
					Min:  2,
					Rep:  2,
					// Sleep 5 seconds ensure job in running state
					Command: "sleep 5",
				},
			},
		})

		// job phase: pending -> running -> Completed
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is Failed job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "failed-delete-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           1,
					Rep:           1,
					Command:       "sleep 10s && exit 3",
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Aborted
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Delete job that is terminated job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "terminate-delete-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           1,
					Rep:           1,
					Command:       "sleep 10s && exit 3",
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Terminated
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Create and Delete job with CPU requirement", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "terminate-delete-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "complete",
					Img:           e2eutil.DefaultNginxImage,
					Min:           1,
					Rep:           1,
					Command:       "sleep 10s",
					RestartPolicy: v1.RestartPolicyNever,
					Req:           e2eutil.CpuResource("1"),
				},
			},
		})

		// job phase: pending -> running -> completed
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Checking Event Generation for job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "terminate-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "complete",
					Img:           e2eutil.DefaultNginxImage,
					Min:           1,
					Rep:           1,
					Command:       "sleep 10s && xyz",
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		err := e2eutil.WaitJobTerminateAction(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Checking Unschedulable Event Generation for job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		nodeName, rep := e2eutil.ComputeNode(ctx, e2eutil.OneCPU)

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

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "unschedulable-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:          "complete",
					Img:           e2eutil.DefaultNginxImage,
					Min:           rep + 1,
					Rep:           rep + 1,
					Command:       "sleep 10s",
					RestartPolicy: v1.RestartPolicyNever,
					Req:           e2eutil.CpuResource("1"),
					Limit:         e2eutil.CpuResource("1"),
					Affinity:      &v1.Affinity{NodeAffinity: nodeAffinity},
				},
			},
		})

		err := e2eutil.WaitJobUnschedulable(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

})
