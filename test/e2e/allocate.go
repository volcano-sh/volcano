package e2e

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

var _ = Describe("Job E2E Test", func() {
	It("schedule job when resource is enough", func() {
		ctx := initTestContext(options{
			nodesNumLimit:      2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "low"
		lowReqJob := createJob(ctx, job)
		err := waitJobReady(ctx, lowReqJob)
		Expect(err).NotTo(HaveOccurred())

		job.name = "high"
		job.tasks[0].req = slot2
		highReqJob := createJob(ctx, job)
		err = waitJobReady(ctx, highReqJob)
		Expect(err).NotTo(HaveOccurred())

		err = compareNodename(ctx, lowReqJob, highReqJob)
		Expect(err).NotTo(HaveOccurred())
	})

	It("schedule job when resource is NOT enough", func() {
		ctx := initTestContext(options{
			nodesNumLimit:      2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot3 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot2,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "middle"
		midReqJob := createJob(ctx, job)
		err := waitJobReady(ctx, midReqJob)
		Expect(err).NotTo(HaveOccurred())

		job.name = "low"
		job.tasks[0].req = slot1
		lowReqJob := createJob(ctx, job)
		err = waitJobReady(ctx, lowReqJob)
		Expect(err).NotTo(HaveOccurred())

		job.name = "high"
		job.tasks[0].req = slot3
		highReqJob := createJob(ctx, job)
		err = waitJobPending(ctx, highReqJob)
		Expect(err).NotTo(HaveOccurred())

		err = compareNodename(ctx, lowReqJob, midReqJob)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allocate don't work when podgroup is pending", func() {
		ctx := initTestContext(options{
			nodesNumLimit:      2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		pgName := "pending-pg"
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ctx.namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &thirtyCPU,
			},
			Status: schedulingv1beta1.PodGroupStatus{
				Phase: schedulingv1beta1.PodGroupPending,
			},
		}

		_, err := ctx.vcclient.SchedulingV1beta1().PodGroups(ctx.namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "exist"
		existJob := createJob(ctx, job)
		err = waitJobReady(ctx, existJob)
		Expect(err).NotTo(HaveOccurred())

		job.name = "allocate"
		allocateJob := createJobWithCondition(ctx, job, pgName, nil)
		err = waitJobPending(ctx, allocateJob)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allocate don't work when queue NOT exist", func() {
		q1 := "q1"
		ctx := initTestContext(options{
			nodesNumLimit:      2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "j1-q1"
		job.queue = q1
		queue1Job := createJob(ctx, job)
		err := waitJobStatePending(ctx, queue1Job)
		Expect(err).NotTo(HaveOccurred(), "queue not found")
	})

	It("allocate don't work when job NOT satisify predicates", func() {
		ctx := initTestContext(options{
			nodesNumLimit:      2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "j1-n3"
		node3Job := createJobWithCondition(ctx, job, "", map[string]string{
			corev1.LabelHostname: "node3",
		})

		err := waitJobPending(ctx, node3Job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allocate don't work when queue is overused", func() {
		q1 := "q1"
		q2 := "q2"
		ctx := initTestContext(options{
			queues: []string{q1, q2},
			nodesNumLimit:      2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})

		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot3 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot4 := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "j1-q1-n1"
		job.queue = q1
		queue1Node1Job := createJob(ctx, job)
		err := waitJobStateReady(ctx, queue1Node1Job)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j2-q1-n2"
		job.queue = q1
		job.tasks[0].req = slot2
		queue1Node2Job := createJob(ctx, job)
		err = waitJobStateReady(ctx, queue1Node2Job)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j3-q2-n2"
		job.queue = q2
		job.tasks[0].req = slot3
		queue2Node2Job := createJob(ctx, job)
		err = waitJobStateReady(ctx, queue2Node2Job)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j4-q1"
		job.queue = q1
		job.tasks[0].req = slot4
		queue1Job := createJob(ctx, job)
		err = waitJobPending(ctx, queue1Job)
		Expect(err).NotTo(HaveOccurred())
	})
})
