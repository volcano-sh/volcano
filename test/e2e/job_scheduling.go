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

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Job E2E Test", func() {
	ginkgo.It("Schedule Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := createJob(context, &jobSpec{
			name: "qj-1",
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
					req: oneCPU,
					min: 2,
					rep: rep,
				},
			},
		})

		err := waitJobReady(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Schedule Multiple Jobs", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
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
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitJobReady(context, job2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitJobReady(context, job3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Gang scheduling", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, defaultNginxImage, oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		jobSpec := &jobSpec{
			name:      "gang-qj",
			namespace: "test",
			tasks: []taskSpec{
				{
					img:     defaultBusyBoxImage,
					req:     oneCPU,
					min:     rep,
					rep:     rep,
					command: "sleep 10s",
				},
			},
		}

		job := createJob(context, jobSpec)
		err = waitJobStateInqueue(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitJobUnschedulable(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitJobReady(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Gang scheduling: Full Occupied", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
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

		job.name = "gang-fq-qj1"
		job1 := createJob(context, job)
		err := waitJobReady(context, job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "gang-fq-qj2"
		job2 := createJob(context, job)
		err = waitJobPending(context, job2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitJobReady(context, job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		job.name = "preemptee-qj"
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "preemptor-qj"
		job2 := createJob(context, job)
		err = waitTasksReady(context, job1, int(rep)/2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Multiple Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		job.name = "multipreemptee-qj"
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "multipreemptor-qj1"
		job2 := createJob(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "multipreemptor-qj2"
		job3 := createJob(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitTasksReady(context, job1, int(rep)/3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitTasksReady(context, job3, int(rep)/3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Schedule BestEffort Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			name: "test",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 2,
					rep: rep,
				},
				{
					img: defaultNginxImage,
					min: 2,
					rep: rep / 2,
				},
			},
		}

		job := createJob(context, spec)

		err := waitJobReady(context, job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Statement", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			namespace: "test",
			tasks: []taskSpec{
				{
					img:     defaultNginxImage,
					req:     slot,
					min:     rep,
					rep:     rep,
					command: "sleep 10s",
				},
			},
		}

		spec.name = "st-qj-1"
		job1 := createJob(context, spec)
		err := waitJobReady(context, job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		now := time.Now()

		spec.name = "st-qj-2"
		job2 := createJob(context, spec)
		err = waitJobUnschedulable(context, job2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// No preemption event
		evicted, err := jobEvicted(context, job1, now)()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(evicted).NotTo(gomega.BeTrue())
	})
})
