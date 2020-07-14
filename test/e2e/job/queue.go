/*
Copyright 2018 The Volcano Authors.

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

package job

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	busv1alpha1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/cli/util"
)

var _ = Describe("Queue E2E Test", func() {
	It("Queue Command Close And Open With State Check", func() {
		q1 := "queue-command-close"
		defaultQueue := "default"
		ctx := initTestContext(options{
			queues: []string{q1},
		})
		defer cleanupTestContext(ctx)

		By("Close queue command check")
		err := util.CreateQueueCommand(ctx.vcclient, defaultQueue, q1, busv1alpha1.CloseQueueAction)
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "Error send close queue command")
		}

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.State == schedulingv1beta1.QueueStateClosed, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Wait for closed queue %s failed", q1)

		By("Open queue command check")
		err = util.CreateQueueCommand(ctx.vcclient, defaultQueue, q1, busv1alpha1.OpenQueueAction)
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "Error send open queue command")
		}

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Wait for reopen queue %s failed", q1)

	})

	Describe("Queue Job Status Transition", func() {
		var ctx *testContext
		var q1 string
		var podNamespace string
		var rep int32
		var firstJobName string
		By("Prepare 2 job")
		BeforeEach(func() {

			q1 = "queue-jobs-status-transition"
			ctx = initTestContext(options{
				queues: []string{q1},
			})
			podNamespace = ctx.namespace
			slot := halfCPU
			rep = clusterSize(ctx, slot)

			if rep < 4 {
				err := fmt.Errorf("You need at least 2 logical cpu for this test case, please skip 'Queue Job Status Transition' when you see this message")
				Expect(err).NotTo(HaveOccurred())
			}

			for i := 0; i < 2; i++ {
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
				spec.name = "queue-job-status-transition-test-job-" + strconv.Itoa(i)
				if i == 0 {
					firstJobName = spec.name
				}
				spec.queue = q1
				createJob(ctx, spec)
			}
		})

		AfterEach(func() {
			cleanupTestContext(ctx)
		})

		It("Transform from inqueque to running should succeed", func() {
			By("Verify queue have pod groups inqueue")
			err := waitQueueStatus(func() (bool, error) {
				queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
				return queue.Status.Inqueue > 0, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Error waiting for queue inqueue")

			By("Verify queue have pod groups running")
			err = waitQueueStatus(func() (bool, error) {
				queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
				return queue.Status.Running > 0, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
		})

		It("Transform from running to pending should succeed", func() {
			By("Verify queue have pod groups running")
			err := waitQueueStatus(func() (bool, error) {
				queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
				return queue.Status.Running > 0, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

			clusterPods, err := ctx.kubeclient.CoreV1().Pods(podNamespace).List(context.TODO(), metav1.ListOptions{})
			for _, pod := range clusterPods.Items {
				if pod.Labels["volcano.sh/job-name"] == firstJobName {
					err = ctx.kubeclient.CoreV1().Pods(podNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
					Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", pod.Name)
				}
			}

			By("Verify queue have pod groups Pending")
			err = waitQueueStatus(func() (bool, error) {
				queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
				return queue.Status.Pending > 0, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Error waiting for queue Pending")
		})

		It("Transform from running to unknown should succeed", func() {
			By("Verify queue have pod groups running")
			err := waitQueueStatus(func() (bool, error) {
				queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
				return queue.Status.Running > 0, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

			By("Delete some of pod which will case pod group status transform from running to unknown.")
			podDeleteNum := 0

			err = waitPodPhaseRunningMoreThanNum(ctx, podNamespace, 2)
			Expect(err).NotTo(HaveOccurred(), "Failed waiting for pods")

			clusterPods, err := ctx.kubeclient.CoreV1().Pods(podNamespace).List(context.TODO(), metav1.ListOptions{})
			for _, pod := range clusterPods.Items {
				if pod.Status.Phase == corev1.PodRunning {
					err = ctx.kubeclient.CoreV1().Pods(podNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
					Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", pod.Name)
					podDeleteNum = podDeleteNum + 1
				}
				if podDeleteNum >= int(rep/2) {
					break
				}
			}

			By("Verify queue have pod groups unknown")
			err = waitQueueStatus(func() (bool, error) {
				queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
				return queue.Status.Unknown > 0, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Error waiting for queue unknown")
		})
	})
})
