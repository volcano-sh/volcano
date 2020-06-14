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

package e2e

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/cli/util"
)

var _ = Describe("Queue E2E Test", func() {
	It("Queue Command Close And Open With State Check", func() {
		q1 := "queue-command-close"
		ctx := initTestContext(options{
			queues: []string{q1},
		})
		defer cleanupTestContext(ctx)

		By("Close queue command check")
		err := util.CreateQueueCommand(ctx.vcclient, "default", q1, "CloseQueue")
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "Error send close queue command")
		}

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.State == "Closed", nil
		})
		Expect(err).NotTo(HaveOccurred(), "Wait for closed queue %s failed", q1)

		By("Open queue command check")
		err = util.CreateQueueCommand(ctx.vcclient, "default", q1, "OpenQueue")
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "Error send open queue command")
		}

		err = waitQueueStatus(func() (bool, error) {
			queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.State == "Open", nil
		})
		Expect(err).NotTo(HaveOccurred(), "Wait for reopen queue %s failed", q1)

	})

	Describe("Queue Job Status Transaction", func() {
		var ctx *testContext
		var q1 string
		var podNamespace string
		var rep int32
		By("Prepare 3 job")
		BeforeEach(func() {

			q1 = "queue-jobs-status-transaction"
			ctx = initTestContext(options{
				queues: []string{q1},
			})
			podNamespace = ctx.namespace
			slot := oneCPU
			rep = clusterSize(ctx, slot)

			if rep < 4 {
				err := fmt.Errorf("Total cpu is too small, you need at least 2 logical cpu for volcano node")
				Expect(err).NotTo(HaveOccurred())
			}

			for i := 0; i < 3; i++ {
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
				spec.name = "queue-job-status-transaction-test-job-" + strconv.Itoa(i)
				spec.queue = q1
				createJob(ctx, spec)
			}
		})

		Context("When transaction from Inqueue to Running", func() {
			It("Transate from inqueque to running should succeed", func() {
				By("Verify queue have pod groups inqueue")
				err := waitQueueStatus(func() (bool, error) {
					queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
					return queue.Status.Inqueue > 0, nil
				})
				Expect(err).NotTo(HaveOccurred(), "Error wait for queue inqueue")

				By("Verify queue have pod groups running")
				err = waitQueueStatus(func() (bool, error) {
					queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
					return queue.Status.Running > 0, nil
				})
				Expect(err).NotTo(HaveOccurred(), "Error wait for queue running")
			})
		})

		AfterEach(func() {
			cleanupTestContext(ctx)
		})

		Context("When all of pod deleted and lackof resources pod group status should transaction from Running to Pending", func() {
			It("Transate from running to pending should succeed", func() {
				By("Verify queue have pod groups running")
				err := waitQueueStatus(func() (bool, error) {
					queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
					return queue.Status.Running > 0, nil
				})
				Expect(err).NotTo(HaveOccurred(), "Error wait for queue running")

				By("Verify queue have pod groups Pending")
				err = waitQueueStatus(func() (bool, error) {
					queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
					return queue.Status.Pending > 0, nil
				})
				Expect(err).NotTo(HaveOccurred(), "Error wait for queue Pending")
			})
		})

		Context("When some of pod deleted and lackof resources pod group status should transaction from Running to Unknown", func() {
			It("Transate from running to unknown should succeed", func() {
				By("Verify queue have pod groups running")
				err := waitQueueStatus(func() (bool, error) {
					queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
					return queue.Status.Running > 0, nil
				})
				Expect(err).NotTo(HaveOccurred(), "Error wait for queue running")

				By("Delete some of pod which will case pod")
				podDeleteNum := 0

				err = waitPodPhaseRunningMoreThanNum(ctx, podNamespace, 2)
				Expect(err).NotTo(HaveOccurred(), "Failed waiting for pods")

				clusterPods, err := ctx.kubeclient.CoreV1().Pods(podNamespace).List(context.TODO(), metav1.ListOptions{})
				fmt.Println("Length of cluster pods are: %s" + strconv.Itoa(len(clusterPods.Items)))
				for _, pod := range clusterPods.Items {
					fmt.Println(pod.Status.Phase)
					if pod.Status.Phase == "Running" {
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
				Expect(err).NotTo(HaveOccurred(), "Error wait for queue unknown")
			})
		})
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
