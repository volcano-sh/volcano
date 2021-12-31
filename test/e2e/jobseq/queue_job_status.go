/*
Copyright 2022 The Volcano Authors.

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

package jobseq

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Queue Job Status Transition", func() {

	It("Transform from inqueque to running should succeed", func() {
		By("Prepare 2 job")
		var ctx *e2eutil.TestContext
		var q1 string
		var rep int32
		q1 = "queue-jobs-status-transition"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
		})
		slot := e2eutil.HalfCPU
		rep = e2eutil.ClusterSize(ctx, slot)

		if rep < 4 {
			err := fmt.Errorf("You need at least 2 logical cpu for this test case, please skip 'Queue Job Status Transition' when you see this message")
			Expect(err).NotTo(HaveOccurred())
		}

		for i := 0; i < 2; i++ {
			spec := &e2eutil.JobSpec{
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "queue-job",
						Img:  e2eutil.DefaultNginxImage,
						Req:  slot,
						Min:  rep,
						Rep:  rep,
					},
				},
			}
			spec.Name = "queue-job-status-transition-test-job-" + strconv.Itoa(i)
			spec.Queue = q1
			e2eutil.CreateJob(ctx, spec)
		}

		By("Verify queue have pod groups inqueue")
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Inqueue > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue inqueue")

		By("Verify queue have pod groups running")
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
		e2eutil.CleanupTestContext(ctx)
	})

	It("Transform from running to pending should succeed", func() {
		By("Prepare 2 job")
		var ctx *e2eutil.TestContext
		var q1 string
		var podNamespace string
		var rep int32
		var firstJobName string

		q1 = "queue-jobs-status-transition"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
		})
		podNamespace = ctx.Namespace
		slot := e2eutil.HalfCPU
		rep = e2eutil.ClusterSize(ctx, slot)

		if rep < 4 {
			err := fmt.Errorf("You need at least 2 logical cpu for this test case, please skip 'Queue Job Status Transition' when you see this message")
			Expect(err).NotTo(HaveOccurred())
		}

		for i := 0; i < 2; i++ {
			spec := &e2eutil.JobSpec{
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "queue-job",
						Img:  e2eutil.DefaultNginxImage,
						Req:  slot,
						Min:  rep,
						Rep:  rep,
					},
				},
			}
			spec.Name = "queue-job-status-transition-test-job-" + strconv.Itoa(i)
			if i == 0 {
				firstJobName = spec.Name
			}
			spec.Queue = q1
			e2eutil.CreateJob(ctx, spec)
		}

		By("Verify queue have pod groups running")
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		clusterPods, err := ctx.Kubeclient.CoreV1().Pods(podNamespace).List(context.TODO(), metav1.ListOptions{})
		for _, pod := range clusterPods.Items {
			if pod.Labels["volcano.sh/job-name"] == firstJobName {
				err = ctx.Kubeclient.CoreV1().Pods(podNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", pod.Name)
			}
		}

		By("Verify queue have pod groups Pending")
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Pending > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue Pending")
		e2eutil.CleanupTestContext(ctx)
	})

	It("Transform from running to unknown should succeed", func() {
		By("Prepare 2 job")
		var ctx *e2eutil.TestContext
		var q1 string
		var podNamespace string
		var rep int32

		q1 = "queue-jobs-status-transition"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
		})
		podNamespace = ctx.Namespace
		slot := e2eutil.HalfCPU
		rep = e2eutil.ClusterSize(ctx, slot)

		if rep < 4 {
			err := fmt.Errorf("You need at least 2 logical cpu for this test case, please skip 'Queue Job Status Transition' when you see this message")
			Expect(err).NotTo(HaveOccurred())
		}

		for i := 0; i < 2; i++ {
			spec := &e2eutil.JobSpec{
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "queue-job",
						Img:  e2eutil.DefaultNginxImage,
						Req:  slot,
						Min:  rep,
						Rep:  rep,
					},
				},
			}
			spec.Name = "queue-job-status-transition-test-job-" + strconv.Itoa(i)
			spec.Queue = q1
			e2eutil.CreateJob(ctx, spec)
		}

		By("Verify queue have pod groups running")
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		By("Delete some of pod which will case pod group status transform from running to unknown.")
		podDeleteNum := 0

		err = e2eutil.WaitPodPhaseRunningMoreThanNum(ctx, podNamespace, 2)
		Expect(err).NotTo(HaveOccurred(), "Failed waiting for pods")

		clusterPods, err := ctx.Kubeclient.CoreV1().Pods(podNamespace).List(context.TODO(), metav1.ListOptions{})
		for _, pod := range clusterPods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				err = ctx.Kubeclient.CoreV1().Pods(podNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", pod.Name)
				podDeleteNum = podDeleteNum + 1
			}
			if podDeleteNum >= int(rep/2) {
				break
			}
		}

		By("Verify queue have pod groups unknown")
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", q1)
			return queue.Status.Unknown > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue unknown")
		e2eutil.CleanupTestContext(ctx)
	})
})
