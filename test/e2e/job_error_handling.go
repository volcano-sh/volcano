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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Running, vkv1.Restarting})
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Running, vkv1.Terminating, vkv1.Terminated})
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Running, vkv1.Aborting, vkv1.Aborted})
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Running})
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
		err := waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Pending, vkv1.Running})
		Expect(err).NotTo(HaveOccurred())

		By("delete one pod of job")
		podName := jobutil.MakePodName(job.Name, "delete", 0)
		context.kubeclient.CoreV1().Pods(job.Namespace).Delete(podName, &metav1.DeleteOptions{})

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

	It("Job error handling: Restart job when job is unschedulable", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, defaultNginxImage, oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &jobSpec{
			name:      "job-restart-when-unschedulable",
			namespace: "test",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		}

		job := createJob(context, jobSpec)
		err = waitJobPhases(context, job, []vkv1.JobPhase{
			vkv1.Pending, vkv1.Restarting})
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())
		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Running})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Job error handling: Abort job when job is unschedulable", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, defaultNginxImage, oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &jobSpec{
			name:      "job-abort-when-unschedulable",
			namespace: "test",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.AbortJobAction,
					Event:  vkv1.JobUnschedulableEvent,
				},
			},
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		}

		job := createJob(context, jobSpec)
		err = waitJobPending(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobPhases(context, job, []vkv1.JobPhase{vkv1.Aborted})
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())
	})

})
