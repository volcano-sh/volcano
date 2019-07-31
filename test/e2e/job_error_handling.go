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
	"strconv"

	"k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	jobutil "volcano.sh/volcano/pkg/controllers/job"
)

var _ = Describe("Job Error Handling", func() {
	It("job level LifecyclePolicy, Event: PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-restart-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.RestartJobAction,
					Event:  vkv1.PodFailedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: TerminateJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-terminate-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Event:  vkv1.PodFailedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Terminating, vkv1.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: AbortJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-abort-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.AbortJobAction,
					Event:  vkv1.PodFailedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Aborting, vkv1.Aborted})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "evicted-restart-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.RestartJobAction,
					Event:  vkv1.PodEvictedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Restarting, vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: TerminateJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "evicted-terminate-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Event:  vkv1.PodEvictedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Terminating, vkv1.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: PodEvicted; Action: AbortJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "evicted-abort-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.AbortJobAction,
					Event:  vkv1.PodEvictedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Aborting -> Aborted
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Aborting, vkv1.Aborted})
		Expect(err).NotTo(HaveOccurred())
	})

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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Restarting, vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Restart job when job is unschedulable", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		jobSpec := &jobSpec{
			name:      "job-restart-when-unschedulable",
			namespace: "test",
			policies: []vkv1.LifecyclePolicy{
				{
					Event:  vkv1.JobUnknownEvent,
					Action: vkv1.RestartJobAction,
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
		job := createJob(context, jobSpec)
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		By("Taint all nodes")
		taints := []v1.Taint{
			{
				Key:    "unschedulable-taint-key",
				Value:  "unschedulable-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		err = taintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		podName := jobutil.MakePodName(job.Name, "test", 0)
		By("Kill one of the pod in order to trigger unschedulable status")
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Job is restarting")
		err = waitJobPhases(context, job, []vkv1.JobPhase{
			vkv1.Restarting, vkv1.Pending})
		Expect(err).NotTo(HaveOccurred())

		By("Untaint all nodes")
		err = removeTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())
		By("Job is running again")
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Abort job when job is unschedulable", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		jobSpec := &jobSpec{
			name:      "job-abort-when-unschedulable",
			namespace: "test",
			policies: []vkv1.LifecyclePolicy{
				{
					Event:  vkv1.JobUnknownEvent,
					Action: vkv1.AbortJobAction,
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
		job := createJob(context, jobSpec)
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		By("Taint all nodes")
		taints := []v1.Taint{
			{
				Key:    "unschedulable-taint-key",
				Value:  "unschedulable-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		err = taintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		podName := jobutil.MakePodName(job.Name, "test", 0)
		By("Kill one of the pod in order to trigger unschedulable status")
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Job is aborted")
		err = waitJobPhases(context, job, []vkv1.JobPhase{
			vkv1.Aborting, vkv1.Aborted})
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event: TaskCompleted; Action: CompletedJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "any-complete-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.CompleteJobAction,
					Event:  vkv1.TaskCompletedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{
			vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Completing, vkv1.Completed})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, error code: 3; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		var erroCode int32 = 3
		job := createJob(context, &jobSpec{
			name: "errorcode-restart-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action:   vkv1.RestartJobAction,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("job level LifecyclePolicy, Event[]: PodEvicted, PodFailed; Action: TerminateJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "evicted-terminate-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.TerminateJobAction,
					Events: []vkv1.Event{vkv1.PodEvictedEvent,
						vkv1.PodFailedEvent,
						vkv1.PodEvictedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Terminating, vkv1.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
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
					policies: []vkv1.LifecyclePolicy{
						{
							Action: vkv1.RestartJobAction,
							Event:  vkv1.PodFailedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running -> restarting
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodEvicted; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
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
					policies: []vkv1.LifecyclePolicy{
						{
							Action: vkv1.RestartJobAction,
							Event:  vkv1.PodEvictedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Restarting -> Running
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Restarting, vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: PodEvicted; Action: TerminateJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
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
					policies: []vkv1.LifecyclePolicy{
						{
							Action: vkv1.TerminateJobAction,
							Event:  vkv1.PodEvictedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		err = context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// job phase: Terminating -> Terminated
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Terminating, vkv1.Terminated})
		Expect(err).NotTo(HaveOccurred())
	})
	It("Task level LifecyclePolicy, Event: TaskCompleted; Action: CompletedJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "any-complete-job",
			tasks: []taskSpec{
				{
					name: "completed-task",
					img:  defaultBusyBoxImage,
					min:  2,
					rep:  2,
					//Sleep 5 seconds ensure job in running state
					command: "sleep 5",
					policies: []vkv1.LifecyclePolicy{
						{
							Action: vkv1.CompleteJobAction,
							Event:  vkv1.TaskCompletedEvent,
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{
			vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Completing, vkv1.Completed})
		Expect(err).NotTo(HaveOccurred())

	})

	It("job level LifecyclePolicy, Event: PodFailed; Action: AbortJob and Task level lifecyclePolicy, Event : PodFailed; Action: RestartJob", func() {
		By("init test context")
		context := initTestContext()
		defer cleanupTestContext(context)

		By("create job")
		job := createJob(context, &jobSpec{
			name: "failed-restart-job",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.AbortJobAction,
					Event:  vkv1.PodFailedEvent,
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
					policies: []vkv1.LifecyclePolicy{
						{
							Action: vkv1.RestartJobAction,
							Event:  vkv1.PodFailedEvent,
						},
					},
				},
			},
		})

		// job phase: pending -> running -> Restarting
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running, vkv1.Restarting})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Task Priority", func() {
		By("init test context")
		context := initTestContext()
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Inqueue, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
		expteced := map[string]int{
			masterPriority: int(nodecount),
			workerPriority: 0,
		}

		err = waitTasksReadyEx(context, job, expteced)
		Expect(err).NotTo(HaveOccurred())
	})

})
