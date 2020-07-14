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
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	jobutil "volcano.sh/volcano/pkg/controllers/job"
)

var _ = Describe("Job Error Handling", func() {
	It("job level LifecyclePolicy, Event: PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-restart-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.RestartJobAction,
					Event:  vcbus.PodFailedEvent,
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
					name:          "fail",
					img:           defaultNginxImage,
					min:           2,
					rep:           2,
					command:       "sleep 10s && xxx",
					restartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> restarting
		err := waitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: TerminateJob", func() {
		By("init test context")
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-terminate-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodFailedEvent,
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
					name:          "fail",
					img:           defaultNginxImage,
					min:           2,
					rep:           2,
					command:       "sleep 10s && xxx",
					restartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Terminating -> Terminated
		err := waitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: AbortJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "failed-abort-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodFailedEvent,
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
					name:          "fail",
					img:           defaultNginxImage,
					min:           2,
					rep:           2,
					command:       "sleep 10s && xxx",
					restartPolicy: v1.RestartPolicyNever,
				},
			},
		})

		// job phase: pending -> running -> Aborting -> Aborted
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Aborting, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: RestartJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "evicted-restart-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.RestartJobAction,
					Event:  vcbus.PodEvictedEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Restarting, vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: TerminateJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "evicted-terminate-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Event:  vcbus.PodEvictedEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: AbortJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "evicted-abort-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodEvictedEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Aborting -> Aborted
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Aborting, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: Any; Action: RestartJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "any-restart-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.RestartJobAction,
					Event:  vcbus.AnyEvent,
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Restarting, vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Restart job when job is unschedulable", func() {
		By("init test context")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		jobSpec := &jobSpec{
			name:      "job-restart-when-unschedulable",
			namespace: ctx.namespace,
			policies: []vcbatch.LifecyclePolicy{
				{
					Event:  vcbus.JobUnknownEvent,
					Action: vcbus.RestartJobAction,
				},
			},
			tasks: []taskSpec{
				{
					name: "test",
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  rep,
					rep:  rep,
				},
			},
		}
		By("Create the Job")
		job := createJob(ctx, jobSpec)
		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		By("Taint all nodes")
		taints := []v1.Taint{
			{
				Key:    "unschedulable-taint-key",
				Value:  "unschedulable-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		err = taintAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		podName := jobutil.MakePodName(job.Name, "test", 0)
		By("Kill one of the pod in order to trigger unschedulable status")
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Job is restarting")
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Restarting, vcbatch.Pending})
		Expect(err).NotTo(HaveOccurred())

		By("Untaint all nodes")
		err = removeTaintsFromAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())
		By("Job is running again")
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Abort job when job is unschedulable", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		jobSpec := &jobSpec{
			name:      "job-abort-when-unschedulable",
			namespace: ctx.namespace,
			policies: []vcbatch.LifecyclePolicy{
				{
					Event:  vcbus.JobUnknownEvent,
					Action: vcbus.AbortJobAction,
				},
			},
			tasks: []taskSpec{
				{
					name: "test",
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  rep,
					rep:  rep,
				},
			},
		}
		By("Create the Job")
		job := createJob(ctx, jobSpec)
		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		By("Taint all nodes")
		taints := []v1.Taint{
			{
				Key:    "unschedulable-taint-key",
				Value:  "unschedulable-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		err = taintAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())

		podName := jobutil.MakePodName(job.Name, "test", 0)
		By("Kill one of the pod in order to trigger unschedulable status")
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Job is aborted")
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Aborting, vcbatch.Aborted})
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(ctx, taints)
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: TaskCompleted; Action: CompletedJob", func() {
		By("init test context")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name:      "any-complete-job",
			namespace: ctx.namespace,
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.CompleteJobAction,
					Event:  vcbus.TaskCompletedEvent,
				},
			},
			tasks: []taskSpec{
				{
					name: "completed-task",
					img:  defaultBusyBoxImage,
					min:  2,
					rep:  2,
					//Sleep 5 seconds ensure job in running state
					command: "sleep 5",
				},
				{
					name: "terminating-task",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
				},
			},
		})

		By("job scheduled, then task 'completed_task' finished and job finally complete")
		// job phase: pending -> running -> completing -> completed
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running, vcbatch.Completing, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, error code: 3; Action: RestartJob", func() {
		By("init test context")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		var erroCode int32 = 3
		job := createJob(ctx, &jobSpec{
			name:      "errorcode-restart-job",
			namespace: ctx.namespace,
			policies: []vcbatch.LifecyclePolicy{
				{
					Action:   vcbus.RestartJobAction,
					ExitCode: &erroCode,
				},
			},
			tasks: []taskSpec{
				{
					name: "success",
					img:  defaultNginxImage,
					min:  1,
					rep:  1,
				},
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

		// job phase: pending -> running -> restarting
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event[]: PodEvicted, PodFailed; Action: TerminateJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "evicted-terminate-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.TerminateJobAction,
					Events: []vcbus.Event{vcbus.PodEvictedEvent,
						vcbus.PodFailedEvent,
						vcbus.PodEvictedEvent,
					},
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
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-restart-job",
			tasks: []taskSpec{
				{
					name: "success",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
				},
				{
					name:          "fail",
					img:           defaultNginxImage,
					min:           2,
					rep:           2,
					command:       "sleep 10s && xxx",
					restartPolicy: v1.RestartPolicyNever,
					policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.RestartJobAction,
							Event:  vcbus.PodFailedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running -> restarting
		err := waitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodEvicted; Action: RestartJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "evicted-restart-job",

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
					policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.RestartJobAction,
							Event:  vcbus.PodEvictedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Restarting, vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodEvicted; Action: TerminateJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "evicted-terminate-job",
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
					policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.TerminateJobAction,
							Event:  vcbus.PodEvictedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = ctx.kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = waitJobPhases(ctx, job, []vcbatch.JobPhase{vcbatch.Terminating, vcbatch.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: TaskCompleted; Action: CompletedJob", func() {
		By("init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("create job")
		job := createJob(ctx, &jobSpec{
			name: "any-complete-job",
			tasks: []taskSpec{
				{
					name: "completed-task",
					img:  defaultBusyBoxImage,
					min:  2,
					rep:  2,
					//Sleep 5 seconds ensure job in running state
					command: "sleep 5",
					policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.CompleteJobAction,
							Event:  vcbus.TaskCompletedEvent,
						},
					},
				},
				{
					name: "terminating-task",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
				},
			},
		})

		By("job scheduled, then task 'completed_task' finished and job finally complete")
		// job phase: pending -> running -> completing -> completed
		err := waitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running, vcbatch.Completing, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: AbortJob and Task level lifecyclePolicy, Event : PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-restart-job",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.AbortJobAction,
					Event:  vcbus.PodFailedEvent,
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
					name:          "fail",
					img:           defaultNginxImage,
					min:           2,
					rep:           2,
					command:       "sleep 10s && xxx",
					restartPolicy: v1.RestartPolicyNever,
					policies: []vcbatch.LifecyclePolicy{
						{
							Action: vcbus.RestartJobAction,
							Event:  vcbus.PodFailedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running -> Restarting
		err := waitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Task Priority", func() {
		By("init test context")
		context := initTestContext(options{
			priorityClasses: map[string]int32{
				masterPriority: masterPriorityValue,
				workerPriority: workerPriorityValue,
			},
		})
		defer cleanupTestContext(context)

		rep := clusterSize(context, oneCPU)
		nodecount := clusterNodeNumber(context)
		By("create job")
		job := createJob(context, &jobSpec{
			name: "task-priority-job",
			min:  int32(nodecount),
			tasks: []taskSpec{
				{
					name:         "higherprioritytask",
					img:          defaultNginxImage,
					rep:          int32(nodecount),
					req:          cpuResource(strconv.Itoa(int(rep)/nodecount - 1)),
					taskpriority: masterPriority,
				},
				{
					name:         "lowerprioritytask",
					img:          defaultNginxImage,
					rep:          int32(nodecount),
					req:          cpuResource(strconv.Itoa(int(rep)/nodecount - 1)),
					taskpriority: workerPriority,
				},
			},
		})

		// job phase: pending -> running
		err := waitJobPhases(context, job, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
		expteced := map[string]int{
			masterPriority: nodecount,
			workerPriority: 0,
		}

		err = waitTasksReadyEx(context, job, expteced)
		Expect(err).NotTo(HaveOccurred())
	})

})
