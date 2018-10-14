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

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job E2E Test", func() {
	It("Schedule Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		_, pg := createJobEx(context, &jobSpec{
			name:      "qj-1",
			namespace: "test",
			tasks: []taskSpec{
				{
					img: "busybox",
					req: oneCPU,
					min: 2,
					rep: rep,
				},
			},
		})

		err := waitPodGroupReady(context, pg)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
			namespace: "test",
			tasks: []taskSpec{
				{
					img: "busybox",
					req: oneCPU,
					min: 2,
					rep: rep,
				},
			},
		}

		job.name = "mqj-1"
		_, pg1 := createJobEx(context, job)
		job.name = "mqj-2"
		_, pg2 := createJobEx(context, job)
		job.name = "mqj-3"
		_, pg3 := createJobEx(context, job)

		err := waitPodGroupReady(context, pg1)
		Expect(err).NotTo(HaveOccurred())

		err = waitPodGroupReady(context, pg2)
		Expect(err).NotTo(HaveOccurred())

		err = waitPodGroupReady(context, pg3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, "nginx", oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		// job := &jobSpec{
		// 	name:      "gang-qj",
		// 	namespace: "test",
		// 	tasks: []taskSpec{
		// 		{
		// 			img: "busybox",
		// 			req: oneCPU,
		// 			min: rep,
		// 			rep: rep,
		// 		},
		// 	},
		// }

		job := createJob(context, "gang-qj", rep, rep, "busybox", oneCPU, nil, nil)
		err = waitJobNotReady(context, job.Name)
		// _, pg := createJobEx(context, job)
		// err = waitPodGroupUnschedulable(context, pg)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job.Name)
		// err = waitPodGroupReady(context, pg)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Full Occupied", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job1 := createJob(context, "gang-fq-qj1", rep, rep, "nginx", oneCPU, nil, nil)
		err := waitJobReady(context, job1.Name)
		Expect(err).NotTo(HaveOccurred())

		job2 := createJob(context, "gang-fq-qj2", rep, rep, "nginx", oneCPU, nil, nil)
		err = waitJobNotReady(context, job2.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job1.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
			namespace: "test",
			tasks: []taskSpec{
				{
					img: "nginx",
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		job.name = "preemptee-qj"
		_, pg1 := createJobEx(context, job)
		err := waitTasksReadyEx(context, pg1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor-qj"
		_, pg2 := createJobEx(context, job)
		err = waitTasksReadyEx(context, pg1, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReadyEx(context, pg2, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Multiple Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job1 := createJob(context, "preemptee-qj", 1, rep, "nginx", slot, nil, nil)
		err := waitTasksReady(context, job1.Name, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job2 := createJob(context, "preemptor-qj", 1, rep, "nginx", slot, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		job3 := createJob(context, "preemptor-qj2", 1, rep, "nginx", slot, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1.Name, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2.Name, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job3.Name, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule BestEffort Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		pgName := "test"

		createPodGroup(context, "burstable", pgName, 2)

		bestEffort := createJobWithoutPodGroup(context, "besteffort", 1, "busybox", nil, pgName)
		burstable := createJobWithoutPodGroup(context, "burstable", 1, "busybox", oneCPU, pgName)

		err := waitJobReadyWithPodGroup(context, pgName, bestEffort.Name, burstable.Name)
		Expect(err).NotTo(HaveOccurred())
	})
})
