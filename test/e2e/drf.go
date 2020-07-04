package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DRF Test", func() {
	It("drf works", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)
		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep,
					rep: rep,
				},
			},
		}

		job.name = "j1-reference"
		referenceJob := createJob(ctx, job)
		err := waitTasksReady(ctx, referenceJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "j2-drf"
		job.tasks[0].req = halfCPU
		backfillJob := createJob(ctx, job)
		err = waitTasksReady(ctx, backfillJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
	})
})
