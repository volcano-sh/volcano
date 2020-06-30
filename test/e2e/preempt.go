package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
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

	It("preemption don't work when podgroup is pending", func() {
		ctx := initTestContext(options{
			priorityClasses: map[string]int32{
				masterPriority: masterPriorityValue,
				workerPriority: workerPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		pgName := "pending-pg"
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
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
		_, err := ctx.vcclient.SchedulingV1beta1().PodGroups(ctx.namespace).Create(context.TODO(), pg, v1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

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

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace:   ctx.namespace,
				Name:        podName,
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName:     "volcano",
				Containers:        createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
				PriorityClassName: masterPriority,
			},
		}
		_, err = ctx.kubeclient.CoreV1().Pods(ctx.namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		Expect(err).To(HaveOccurred())
	})

	It("preemption only works in the same queue", func() {
		ctx := initTestContext(options{
			queues: []string{"q1, q2"},
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
					rep: int(rep) / 2,
				},
			},
		}

		job.name = "j1-q1"
		job.pri = workerPriority
		job.queue = "q1"
		queue1Job := createJob(ctx, job)
		err := waitTasksReady(ctx, queue1Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j2-q2"
		job.pri = workerPriority
		job.queue = "q2"
		queue2Job := createJob(ctx, job)
		err = waitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j3-q1"
		job.pri = masterPriority
		job.queue = "q1"
		job.tasks[0].rep = int(rep)
		queue1Job3 := createJob(ctx, job)
		err = waitTasksReady(ctx, queue1Job3, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, queue1Job, int(rep)/2)
		Expect(err).Should(ContainSubstring(`actual got`))
		err = waitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})
})
