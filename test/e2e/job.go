/*
Copyright 2017 The Volcano Authors.

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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job E2E Test", func() {
	It("Schedule Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := createJob(context, &jobSpec{
			name: "qj-1",
			tasks: []taskSpec{
				{
					img: "busybox",
					req: oneCPU,
					min: 2,
					rep: rep,
				},
			},
		})

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
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
		job1 := createJob(context, job)
		job.name = "mqj-2"
		job2 := createJob(context, job)
		job.name = "mqj-3"
		job3 := createJob(context, job)

		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, "nginx", oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &jobSpec{
			name:      "gang-qj",
			namespace: "test",
			tasks: []taskSpec{
				{
					img: "busybox",
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		}

		job := createJob(context, jobSpec)
		err = waitJobPending(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobUnschedulable(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Full Occupied", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
			namespace: "test",
			tasks: []taskSpec{
				{
					img: "nginx",
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		}

		job.name = "gang-fq-qj1"
		job1 := createJob(context, job)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		job.name = "gang-fq-qj2"
		job2 := createJob(context, job)
		err = waitJobPending(context, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
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
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor-qj"
		job2 := createJob(context, job)
		err = waitTasksReady(context, job1, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Multiple Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
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
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor-qj1"
		job2 := createJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor-qj2"
		job3 := createJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job3, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule BestEffort Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			name: "test",
			tasks: []taskSpec{
				{
					img: "nginx",
					req: slot,
					min: 2,
					rep: rep,
				},
				{
					img: "nginx",
					min: 2,
					rep: rep / 2,
				},
			},
		}

		job := createJob(context, spec)

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Statement", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			namespace: "test",
			tasks: []taskSpec{
				{
					img: "nginx",
					req: slot,
					min: rep,
					rep: rep,
				},
			},
		}

		spec.name = "st-qj-1"
		job1 := createJob(context, spec)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		now := time.Now()

		spec.name = "st-qj-2"
		job2 := createJob(context, spec)
		err = waitJobUnschedulable(context, job2)
		Expect(err).NotTo(HaveOccurred())

		// No preemption event
		evicted, err := jobEvicted(context, job1, now)()
		Expect(err).NotTo(HaveOccurred())
		Expect(evicted).NotTo(BeTrue())
	})
})
