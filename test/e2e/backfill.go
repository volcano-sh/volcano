package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backfill Test", func() {
	It("backfill works", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)
		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep / 2,
					rep: rep / 2,
				},
			},
		}

		job.name = "j1-reference"
		referenceJob := createJob(ctx, job)
		err := waitTasksReady(ctx, referenceJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j2-deleted"
		deletedJob := createJob(ctx, job)
		err = waitTasksReady(ctx, deletedJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j3-backfill"
		backfillJob := createJob(ctx, job)
		err = waitTasksReady(ctx, backfillJob, int(rep)/2)
		Expect(err).To(HaveOccurred())
		err = deleteJob(ctx, deletedJob.Name, ctx.namespace)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, deletedJob, 0)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, backfillJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})
})
