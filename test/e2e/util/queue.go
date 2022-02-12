/*
Copyright 2021 The Volcano Authors.

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

package util

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

type QueueSpec struct {
	Name              string
	Weight            int32
	GuaranteeResource v1.ResourceList
}

func CreateQueueWithQueueSpec(ctx *TestContext, queueSpec *QueueSpec) {
	_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
	if err != nil {
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: queueSpec.Name,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: queueSpec.Weight,
			},
		}
		if len(queueSpec.GuaranteeResource) != 0 {
			queue.Spec.Guarantee.Resource = queueSpec.GuaranteeResource
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create queue %s", queueSpec.Name)
	}
}

// CreateQueue creates Queue with the specified name
func CreateQueue(ctx *TestContext, q string) {
	_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
	if err != nil {
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: q,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create queue %s", q)
	}
}

// CreateQueues create Queues specified in the test context
func CreateQueues(ctx *TestContext) {
	By("Creating Queues")

	for _, queue := range ctx.Queues {
		CreateQueue(ctx, queue)
	}
}

// DeleteQueue deletes Queue with the specified name
func DeleteQueue(ctx *TestContext, q string) {
	foreground := metav1.DeletePropagationForeground
	queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get queue %s", q)

	queue.Status.State = schedulingv1beta1.QueueStateClosed
	_, err = ctx.Vcclient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), queue, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to update status of queue %s", q)
	err = wait.Poll(100*time.Millisecond, FiveMinute, queueClosed(ctx, q))
	Expect(err).NotTo(HaveOccurred(), "failed to wait queue %s closed", q)

	err = ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), q,
		metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})
	Expect(err).NotTo(HaveOccurred(), "failed to delete queue %s", q)
}

// deleteQueues deletes Queues specified in the test context
func deleteQueues(ctx *TestContext) {
	for _, q := range ctx.Queues {
		DeleteQueue(ctx, q)
	}
}

// SeyQueueReclaimable sets the Queue to be reclaimable
func SetQueueReclaimable(ctx *TestContext, queues []string, reclaimable bool) {
	By("Setting Queue reclaimable")

	for _, q := range queues {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get queue %s", q)

		queue.Spec.Reclaimable = &reclaimable
		_, err = ctx.Vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to update queue %s", q)
	}
}

func WaitQueueStatus(condition func() (bool, error)) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, condition)
}

// queueClosed returns whether the Queue is closed
func queueClosed(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if queue.Status.State != schedulingv1beta1.QueueStateClosed {
			return false, nil
		}

		return true, nil
	}
}
