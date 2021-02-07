package jobseq

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	busv1alpha1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

var _ = Describe("Hierarchy Queue E2E Test", func() {
	It("Hierarchy Queue Create And Update Check", func() {
		By("Init test ctx")
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		By("Create hierarchy root queue check")
		root := schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "root",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Hierarchy: []schedulingv1beta1.HierarchyAttr{
					{
						Name: "default",
						Parameters: schedulingv1beta1.Parameter{
							Weight: 5,
						},
					},
					{
						Name: "dev",
						Parameters: schedulingv1beta1.Parameter{
							Weight: 5,
						},
					},
					{
						Name: "dev.test1",
						Parameters: schedulingv1beta1.Parameter{
							Weight: 1,
						},
					},
					{
						Name: "dev.test2",
						Parameters: schedulingv1beta1.Parameter{
							Weight: 2,
						},
					},
				},
			},
		}
		_, err := ctx.vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &root, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		queueList, err := ctx.vcclient.SchedulingV1beta1().Queues().List(context.TODO(), metav1.ListOptions{})
		for _, queue := range queueList.Items {
			Expect(queue.Name).Should(SatisfyAny(Equal("default"), Equal("dev"), Equal("test1"), Equal("test2"), Equal("root")))
		}

		Eventually(
			func() error {
				return util.CreateQueueCommand(ctx.vcclient, defaultQueue, "test2", busv1alpha1.CloseQueueAction)
			},
			time.Second*60, time.Second*5).Should(BeNil())

		Eventually(
			func() error {
				queue, _ := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "test2", metav1.GetOptions{})
				if queue.Status.State == "Closed" {
					return nil
				}
				return fmt.Errorf("Error waiting for queue closed")
			},
			time.Second*60, time.Second*5).Should(BeNil())

		By("Update hierarchy queues check")
		updateRoot := root.DeepCopy()
		updateHierarchy := []schedulingv1beta1.HierarchyAttr{
			{
				Name: "default",
				Parameters: schedulingv1beta1.Parameter{
					Weight: 5,
				},
			},
			{
				Name: "dev",
				Parameters: schedulingv1beta1.Parameter{
					Weight: 5,
				},
			},
			{
				Name: "dev.test1",
				Parameters: schedulingv1beta1.Parameter{
					Weight: 2,
				},
			},
			{
				Name: "dev.test3",
				Parameters: schedulingv1beta1.Parameter{
					Weight: 2,
				},
			},
		}
		updateRoot.Spec.Hierarchy = updateHierarchy
		_, err = ctx.vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), updateRoot, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		test1, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "test1", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Get queue test1 failed")
		Expect(test1.Spec.Weight).Should(Equal(2))

		_, err = ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "test2", metav1.GetOptions{})
		Expect(errors.IsNotFound(err)).Should(Equal(true))

		_, err = ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "test3", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Get queue test3 failed")

		By("Hierarchy queue job status transition")
		job1 := createJob(ctx, &jobSpec{
			name: "job-name",
			tasks: []taskSpec{
				{
					name: "default",
					img:  defaultNginxImage,
					min:  2,
					rep:  2,
					req:  halfCPU,
				},
			},
			queue: "test1",
		})
		err = waitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "test1", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue test1 failed")
			return queue.Status.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "dev", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue dev failed")
			return queue.Status.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "root", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue root failed")
			return queue.Status.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
	})
})
