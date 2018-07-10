/*
Copyright 2017 The Kubernetes Authors.

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

package test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("E2E Test", func() {
	It("Schedule QueueJob with SchedulerName", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)
		queueJob := createQueueJobWithScheduler(context, "kar-scheduler", "qj-1", 2, rep, "busybox", oneCPU)
		err := waitJobReady(context, queueJob.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule QueueJob", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)
		queueJob := createQueueJob(context, "qj-1", 2, rep, "busybox", oneCPU)
		err := waitJobReady(context, queueJob.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, "nginx", oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		queueJob := createQueueJob(context, "gang-qj", rep, rep, "busybox", oneCPU)
		err = waitJobNotReady(context, queueJob.Name)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, queueJob.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		qj1 := createQueueJob(context, "preemptee-qj", 1, rep, "nginx", slot)
		err := waitTasksReady(context, qj1.Name, int(rep))
		Expect(err).NotTo(HaveOccurred())

		qj2 := createQueueJob(context, "preemptor-qj", 1, rep, "nginx", slot)
		err = waitTasksReady(context, qj2.Name, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, qj1.Name, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("TaskPriority", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		replicaset := createReplicaSet(context, "rs-1", rep/2, "nginx", slot)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		ts := []taskSpec{
			{
				name: "worker",
				img:  "nginx",
				pri:  workerPriority,
				rep:  rep,
				req:  slot,
			},
			{
				name: "master",
				img:  "nginx",
				pri:  masterPriority,
				rep:  1,
				req:  slot,
			},
		}

		qj := createQueueJobEx(context, "multi-pod-qj", rep/2, ts)

		expectedTasks := map[string]int32{"master": 1, "worker": rep/2 - 1}
		err = waitTasksReadyEx(context, qj.Name, expectedTasks)
		Expect(err).NotTo(HaveOccurred())
	})
})
