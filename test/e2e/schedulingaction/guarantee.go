package schedulingaction

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Guarantee reserved resource E2E Test", func() {
	ginkgo.It("one queue reserve all cluster resource", func() {
		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi")}

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit:      1,
			NodesResourceLimit: slot,
		})
		defer e2eutil.CleanupTestContext(ctx)

		queue1 := &e2eutil.QueueSpec{
			Name:              "queue1",
			Weight:            1,
			GuaranteeResource: slot,
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue1)

		queue2 := &e2eutil.QueueSpec{
			Name:   "queue2",
			Weight: 1,
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue2)

		job := &e2eutil.JobSpec{
			Queue: "queue2",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi")},
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "queue2-job"
		lowReqJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobPending(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("one queue reserve some cluster resource, so it  is NOT enough to create job", func() {
		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi")}

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit:      1,
			NodesResourceLimit: slot,
		})
		defer e2eutil.CleanupTestContext(ctx)

		queue1 := &e2eutil.QueueSpec{
			Name:   "queue1",
			Weight: 1,
			GuaranteeResource: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1024Mi")},
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue1)

		queue2 := &e2eutil.QueueSpec{
			Name:   "queue2",
			Weight: 1,
		}
		e2eutil.CreateQueueWithQueueSpec(ctx, queue2)

		job := &e2eutil.JobSpec{
			Queue: "queue2",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi")},
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "queue2-job"
		lowReqJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobPending(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
