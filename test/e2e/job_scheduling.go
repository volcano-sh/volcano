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
	"math"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job E2E Test", func() {
	cleanupResources := CleanupResources{}
	var context *context

	BeforeEach(func() {
		context = gContext
	})

	AfterEach(func() {
		deleteResources(gContext, cleanupResources)
	})

	It("Schedule Job", func() {
		jobName := "qj-1"
		cleanupResources.Jobs = []string{jobName}
		rep := clusterSize(context, oneCPU)

		job := createJob(context, &jobSpec{
			name: jobName,
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
					req: oneCPU,
					min: int32(math.Min(2, float64(rep))),
					rep: rep,
				},
			},
		})

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		jobName1 := "mqj-1"
		jobName2 := "mqj-2"
		jobName3 := "mqj-3"
		cleanupResources.Jobs = []string{jobName1, jobName2, jobName3}
		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
					req: oneCPU,
					min: int32(math.Min(2, float64(rep))),
					rep: rep,
				},
			},
		}

		job.name = jobName1
		job1 := createJob(context, job)
		job.name = jobName2
		job2 := createJob(context, job)
		job.name = jobName3
		job3 := createJob(context, job)

		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		jobName := "gang-qj"
		cleanupResources.Jobs = []string{jobName}

		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, defaultNginxImage, oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &jobSpec{
			name:      "gang-qj",
			namespace: "test",
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
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
		jobName1 := "gang-fq-qj1"
		jobName2 := "gang-fq-qj2"
		cleanupResources.Jobs = []string{jobName1, jobName2}

		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		}

		job.name = jobName1
		job1 := createJob(context, job)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		job.name = jobName2
		job2 := createJob(context, job)
		err = waitJobPending(context, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Preemption", func() {
		jobName1 := "preemptee-qj"
		jobName2 := "preemptor-qj"
		cleanupResources.Jobs = []string{jobName1, jobName2}

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

		job.name = jobName1
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = jobName2
		job2 := createJob(context, job)
		err = waitTasksReady(context, job1, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Multiple Preemption", func() {
		jobName1 := "multipreemptee-qj"
		jobName2 := "multipreemptor-qj1"
		jobName3 := "multipreemptor-qj2"
		cleanupResources.Jobs = []string{jobName1, jobName2, jobName3}

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

		job.name = jobName1
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = jobName2
		job2 := createJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		job.name = jobName3
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
		jobName := "test-besteffort"
		cleanupResources.Jobs = []string{jobName}

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			name: jobName,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: int32(math.Min(2, float64(rep))),
					rep: rep,
				},
				{
					img: defaultNginxImage,
					min: int32(math.Min(2, float64(rep/2))),
					rep: rep / 2,
				},
			},
		}

		job := createJob(context, spec)

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Statement", func() {
		jobName1 := "st-qj-1"
		jobName2 := "st-qj-2"
		cleanupResources.Jobs = []string{jobName1, jobName2}

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep,
					rep: rep,
				},
			},
		}

		spec.name = jobName1
		job1 := createJob(context, spec)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		now := time.Now()

		spec.name = jobName2
		job2 := createJob(context, spec)
		err = waitJobUnschedulable(context, job2)
		Expect(err).NotTo(HaveOccurred())

		// No preemption event
		evicted, err := jobEvicted(context, job1, now)()
		Expect(err).NotTo(HaveOccurred())
		Expect(evicted).NotTo(BeTrue())
	})
})
