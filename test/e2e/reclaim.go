package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

var _ = Describe("Queue E2E Test", func() {
	It("Reclaim: New queue with job created no reclaim when resource is enough", func() {
		q1 := "default"
		q2 := "reclaim-q2"
		ctx := initTestContext(options{
			queues:             []string{q2},
			nodesNumLimit:      4,
			nodesResourceLimit: v1.ResourceList{"cpu": resource.MustParse("1000m"), "memory": resource.MustParse("1024Mi")},
			priorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer cleanupTestContext(ctx)

		slot := v1.ResourceList{"cpu": resource.MustParse("1000m"), "memory": resource.MustParse("1024Mi")}
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

		job.name = "reclaim-j1"
		job.queue = q1
		job.pri = "low-priority"
		job1 := createJob(ctx, job)

		job.name = "reclaim-j2"
		job.queue = q2
		job.pri = "low-priority"
		job2 := createJob(ctx, job)

		err := waitTasksReady(ctx, job1, 1)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		err = waitTasksReady(ctx, job2, 1)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		// create queue3 & append to context queue list
		q3 := "reclaim-q3"
		ctx.queues = append(ctx.queues, q3)
		createQueues(ctx)

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		job.name = "reclaim-j3"
		job.queue = q3
		job.pri = "high-priority"
		createJob(ctx, job)

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q2, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q2)
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q3, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q3)
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

	})

	It("Reclaim: New queue with job created reclaim when lack of resource", func() {
		q1 := "default"
		q2 := "reclaim-q2"
		ctx := initTestContext(options{
			queues:             []string{q2},
			nodesNumLimit:      3,
			nodesResourceLimit: v1.ResourceList{"cpu": resource.MustParse("2000m"), "memory": resource.MustParse("2048Mi")},
			priorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer cleanupTestContext(ctx)

		slot1 := v1.ResourceList{"cpu": resource.MustParse("2000m"), "memory": resource.MustParse("2048Mi")}
		slot2 := v1.ResourceList{"cpu": resource.MustParse("1000m"), "memory": resource.MustParse("1024Mi")}
		jobSpec1 := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}
		jobSpec2 := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot2,
					min: 1,
					rep: 1,
				},
			},
		}

		jobList := []*vcbatch.Job{}
		jobSpec1.name = "reclaim-j1"
		jobSpec1.queue = q1
		jobSpec1.pri = "low-priority"
		jobList = append(jobList, createJob(ctx, jobSpec1))

		jobSpec1.name = "reclaim-j2"
		jobSpec1.queue = q1
		jobSpec1.pri = "low-priority"
		jobList = append(jobList, createJob(ctx, jobSpec1))

		jobSpec2.name = "reclaim-j3"
		jobSpec2.queue = q2
		jobSpec2.pri = "low-priority"
		jobList = append(jobList, createJob(ctx, jobSpec2))

		jobSpec2.name = "reclaim-j4"
		jobSpec2.queue = q2
		jobSpec2.pri = "low-priority"
		jobList = append(jobList, createJob(ctx, jobSpec2))

		for _, jobCreated := range jobList {
			err := waitTasksReady(ctx, jobCreated, 1)
			Expect(err).NotTo(HaveOccurred(), "Wait for job "+jobCreated.Name+" failed")
		}

		// Create queue3 & append to context queue list
		q3 := "reclaim-q3"
		ctx.queues = append(ctx.queues, q3)
		createQueues(ctx)

		jobSpec2.name = "reclaim-j5"
		jobSpec2.queue = q3
		jobSpec2.pri = "high-priority"
		createJob(ctx, jobSpec2)

		// Ensure job is running in q3
		err := waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q3, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q3)
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
	})

	It("Reclaim", func() {
		q1, q2 := "reclaim-q1", "reclaim-q2"
		ctx := initTestContext(options{
			queues: []string{q1, q2},
			priorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

		spec := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		spec.name = "q1-qj-1"
		spec.queue = q1
		spec.pri = "low-priority"
		job1 := createJob(ctx, spec)
		err := waitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			Expect(err).NotTo(HaveOccurred())
		}

		spec.name = "q2-qj-2"
		spec.queue = q2
		spec.pri = "high-priority"
		job2 := createJob(ctx, spec)
		err = waitTasksReady(ctx, job2, expected)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(ctx, job1, expected)
		Expect(err).NotTo(HaveOccurred())

		// Test Queue status
		spec = &jobSpec{
			name:  "q1-qj-2",
			queue: q1,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep * 2,
					rep: rep * 2,
				},
			},
		}
		job3 := createJob(ctx, spec)
		err = waitJobStatePending(ctx, job3)
		Expect(err).NotTo(HaveOccurred())
		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Status.Pending == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
