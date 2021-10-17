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

package jobseq

import (
	"context"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	jobctl "volcano.sh/volcano/pkg/controllers/job"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job Error Handling", func() {
	It("job level LifecyclePolicy, Event: PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		By("create job")
		job := e2eutil.CreateJob(context, &e2eutil.JobSpec{
			Name: "failed-restart-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.RestartJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           2,
					Rep:           2,
					Command:       "sleep 10s && xxx",
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> restarting
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: TerminateJob", func() {
		By("init test context")
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		By("create job")
		job := e2eutil.CreateJob(context, &e2eutil.JobSpec{
			Name: "failed-terminate-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           2,
					Rep:           2,
					Command:       "sleep 10s && xxx",
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Terminating -> Terminated
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: AbortJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "failed-abort-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           2,
					Rep:           2,
					Command:       "sleep 10s && xxx",
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Aborting -> Aborted
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Aborting, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: RestartJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "evicted-restart-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.RestartJobAction,
					Event:  vcbus.PodEvictedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Restarting, vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: TerminateJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "evicted-terminate-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodEvictedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: AbortJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "evicted-abort-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodEvictedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Aborting -> Aborted
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Aborting, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: Any; Action: RestartJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "any-restart-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.RestartJobAction,
					Event:  vcbus.AnyEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Restarting, vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Restart job when job is unschedulable", func() {
		By("init test context")
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		jobSpec := &e2eutil.JobSpec{
			Name:      "job-restart-when-unschedulable",
			Namespace: ctx.Namespace,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Event:  vcbus.JobUnknownEvent,
					Action: vcbus.RestartJobAction,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "test",
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  rep,
					Rep:  rep,
				},
			},
		}
		By("Create the Job")
		job := e2eutil.CreateJob(ctx, jobSpec)
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		By("Taint all nodes")
		taints := []v1.Taint{
			{
				Key:    "unschedulable-taint-key",
				Value:  "unschedulable-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		err = e2eutil.TaintAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		podName := jobctl.MakePodName(job.Name, "test", 0)
		By("Kill one of the pod in order to trigger unschedulable status")
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Job is restarting")
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Restarting, vcbatch.Pending})
		Expect(err).NotTo(HaveOccurred())

		By("Untaint all nodes")
		err = e2eutil.RemoveTaintsFromAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())
		By("Job is running again")
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Abort job when job is unschedulable", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		jobSpec := &e2eutil.JobSpec{
			Name:      "job-abort-when-unschedulable",
			Namespace: ctx.Namespace,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Event:  vcbus.JobUnknownEvent,
					Action: vcbus.AbortJobAction,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "test",
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  rep,
					Rep:  rep,
				},
			},
		}
		By("Create the Job")
		job := e2eutil.CreateJob(ctx, jobSpec)
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		By("Taint all nodes")
		taints := []v1.Taint{
			{
				Key:    "unschedulable-taint-key",
				Value:  "unschedulable-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		err = e2eutil.TaintAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		podName := jobctl.MakePodName(job.Name, "test", 0)
		By("Kill one of the pod in order to trigger unschedulable status")
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Job is aborted")
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Aborting, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.RemoveTaintsFromAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: TaskCompleted; Action: CompletedJob", func() {
		By("init test context")
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:      "any-complete-job",
			Namespace: ctx.Namespace,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.CompleteJobAction,
					Event:  vcbus.TaskCompletedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "completed-task",
					Img:  e2eutil.DefaultBusyBoxImage,
					Min:  2,
					Rep:  2,
					//Sleep 5 seconds ensure job in running state
					Command: "sleep 5",
				},
				{
					Name: "terminating-task",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		By("job scheduled, then task 'completed_task' finished and job finally complete")
		// job phase: pending -> running -> completing -> completed
		// TODO: skip running -> completing for the github CI pool performance
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, Event: TaskFailed; Action: TerminateJob", func() {
		By("init test context")
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:      "task-failed-terminate-job",
			Namespace: ctx.Namespace,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.TaskFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultBusyBoxImage,
					Min:  2,
					Rep:  2,
					//Sleep 5 seconds ensure job in running state
					Command: "sleep 5",
				},
				{
					Name:          "failed",
					Img:           e2eutil.DefaultBusyBoxImage,
					Min:           2,
					Rep:           2,
					Command:       "sleep 10s && xxx",
					RestartPolicy: v1.RestartPolicyNever,
					MaxRetry:      3,
				},
			},
		})

		// job phase: Pending -> Running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("update one pod of job")
		podName := jobctl.MakePodName(job.Name, "failed", 0)
		pod, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod.Status.ContainerStatuses = []v1.ContainerStatus{{RestartCount: 4}}
		_, err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, error code: 3; Action: RestartJob", func() {
		By("init test context")
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		var erroCode int32 = 3
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:      "errorcode-restart-job",
			Namespace: ctx.Namespace,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action:   vcbus.RestartJobAction,
					ExitCode: &erroCode,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  1,
					Rep:  1,
				},
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

		// job phase: pending -> running -> restarting
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event[]: PodEvicted, PodFailed; Action: TerminateJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "evicted-terminate-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Events: []vcbus.Event{vcbus.PodEvictedEvent,
						vcbus.PodFailedEvent,
						vcbus.PodEvictedEvent,
					},
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		By("create job")
		job := e2eutil.CreateJob(context, &e2eutil.JobSpec{
			Name: "failed-restart-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           2,
					Rep:           2,
					Command:       "sleep 10s && xxx",
					RestartPolicy: v1.RestartPolicyNever,
					Policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.RestartJobAction,
							Event:  vcbus.PodFailedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running -> restarting
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodEvicted; Action: RestartJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "evicted-restart-job",

			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
					Policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.RestartJobAction,
							Event:  vcbus.PodEvictedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Restarting, vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodEvicted; Action: TerminateJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "evicted-terminate-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name: "delete",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
					Policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.TerminateJobAction,
							Event:  vcbus.PodEvictedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobctl.MakePodName(job.Name, "delete", 0)
		err = ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: TaskCompleted; Action: CompletedJob", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "any-complete-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "completed-task",
					Img:  e2eutil.DefaultBusyBoxImage,
					Min:  2,
					Rep:  2,
					// Sleep 5 seconds ensure job in running state
					Command: "sleep 5",
					Policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.CompleteJobAction,
							Event:  vcbus.TaskCompletedEvent,
						},
					},
				},
				{
					Name: "terminating-task",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
			},
		})

		By("job scheduled, then task 'completed_task' finished and job finally complete")
		// job phase: pending -> running -> completing -> completed
		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: AbortJob and Task level lifecyclePolicy, Event : PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		By("create job")
		job := e2eutil.CreateJob(context, &e2eutil.JobSpec{
			Name: "failed-restart-job",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodFailedEvent,
				},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "success",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
				},
				{
					Name:          "fail",
					Img:           e2eutil.DefaultNginxImage,
					Min:           2,
					Rep:           2,
					Command:       "sleep 10s && xxx",
					RestartPolicy: v1.RestartPolicyNever,
					Policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.RestartJobAction,
							Event:  vcbus.PodFailedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running -> Restarting
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Task Priority", func() {
		By("init test context")
		context := e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{
				e2eutil.MasterPriority: e2eutil.MasterPriorityValue,
				e2eutil.WorkerPriority: e2eutil.WorkerPriorityValue,
			},
		})
		defer e2eutil.CleanupTestContext(context)

		rep := e2eutil.ClusterSize(context, e2eutil.OneCPU)
		nodecount := e2eutil.ClusterNodeNumber(context)
		By("create job")
		job := e2eutil.CreateJob(context, &e2eutil.JobSpec{
			Name: "task-priority-job",
			Min:  int32(nodecount),
			Tasks: []e2eutil.TaskSpec{
				{
					Name:         "higherprioritytask",
					Img:          e2eutil.DefaultNginxImage,
					Rep:          int32(nodecount),
					Req:          e2eutil.CpuResource(strconv.Itoa(int(rep)/nodecount - 1)),
					Taskpriority: e2eutil.MasterPriority,
				},
				{
					Name:         "lowerprioritytask",
					Img:          e2eutil.DefaultNginxImage,
					Rep:          int32(nodecount),
					Req:          e2eutil.CpuResource(strconv.Itoa(int(rep)/nodecount - 1)),
					Taskpriority: e2eutil.MasterPriority,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
		expteced := map[string]int{
			e2eutil.MasterPriority: nodecount,
			e2eutil.WorkerPriority: 0,
		}

		err = e2eutil.WaitTasksReadyEx(context, job, expteced)
		Expect(err).NotTo(HaveOccurred())
	})

})
