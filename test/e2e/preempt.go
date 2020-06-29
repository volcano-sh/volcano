package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Job E2E Test", func() {
	It("schedule high priority job without preemption when resource is enough", func() {
		ctx := initTestContext(options{
			priorityClasses: map[string]int32{
				masterPriority: masterPriorityValue,
				workerPriority: workerPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "preemptee"
		job.pri = workerPriority
		preempteeJob := createJob(ctx, job)
		err := waitTasksReady(ctx, preempteeJob, 1)
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor"
		job.pri = masterPriority
		preemptorJob := createJob(ctx, job)
		err = waitTasksReady(ctx, preempteeJob, 1)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(ctx, preemptorJob, 1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("schedule high priority job with preemption when resource is NOT enough", func() {
		ctx := initTestContext(options{
			priorityClasses: map[string]int32{
				masterPriority: masterPriorityValue,
				workerPriority: workerPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

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

		job.name = "preemptee"
		job.pri = workerPriority
		preempteeJob := createJob(ctx, job)
		err := waitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor"
		job.pri = masterPriority
		job.min = rep / 2
		preemptorJob := createJob(ctx, job)
		err = waitTasksReady(ctx, preempteeJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(ctx, preemptorJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})
})
