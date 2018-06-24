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

		queueJob := createQueueJob(context, "qj-1", rep, rep, "busybox", oneCPU)
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

		rep := clusterSize(context, oneCPU)

		qj1 := createQueueJob(context, "qj-1", 1, rep, "nginx", oneCPU)
		err := waitTasksReady(context, qj1.Name, int(rep))
		Expect(err).NotTo(HaveOccurred())

		qj2 := createQueueJob(context, "qj-2", 1, rep, "nginx", oneCPU)
		err = waitTasksReady(context, qj2.Name, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, qj1.Name, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})
})
