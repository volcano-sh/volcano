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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

type QueueSpec struct {
	Name              string
	Weight            int32
	GuaranteeResource v1.ResourceList
	DeservedResource  v1.ResourceList
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
			// When guarantee is set, deserved must also be set
			if len(queueSpec.DeservedResource) != 0 {
				queue.Spec.Deserved = queueSpec.DeservedResource
			} else {
				// Default: deserved = guarantee if not explicitly set
				queue.Spec.Deserved = queueSpec.GuaranteeResource
			}
		} else if len(queueSpec.DeservedResource) != 0 {
			queue.Spec.Deserved = queueSpec.DeservedResource
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create queue %s", queueSpec.Name)
	}

	// wait for queue state turns to be open
	time.Sleep(3 * time.Second)
}

// CreateQueue creates Queue with the specified name
func CreateQueue(ctx *TestContext, q string, deservedResource v1.ResourceList, parent string) {
	_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
	if err != nil {
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: q,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight:   1,
				Parent:   parent,
				Deserved: deservedResource,
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create queue %s", q)
	}
}

// CreateQueues create Queues specified in the test context
func CreateQueues(ctx *TestContext) {
	By("Creating Queues")

	for _, queue := range ctx.Queues {
		CreateQueue(ctx, queue, ctx.DeservedResource[queue], ctx.QueueParent[queue])
	}

	// wait for all queues state open
	time.Sleep(3 * time.Second)
}

// DeleteQueue deletes Queue with the specified name
func DeleteQueue(ctx *TestContext, q string) {
	foreground := metav1.DeletePropagationForeground

	jobs, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		Expect(err).NotTo(HaveOccurred(), "failed to list vcjobs")
	}

	for _, job := range jobs.Items {
		if job.Spec.Queue == q {
			//Delete all VcJobs in the queue
			err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})
			Expect(err).NotTo(HaveOccurred(), "failed to delete vcjob %s in queue %s", job.Name, q)

			// Wait until the job is deleted
			delErr := wait.Poll(100*time.Millisecond, TwoMinute, func() (bool, error) {
				_, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return true, nil
				}
				if err != nil {
					return false, err
				}
				return false, nil
			})
			Expect(delErr).NotTo(HaveOccurred(), "failed waiting vcjob %s deleted", job.Name)
		}
	}

	var queue *schedulingv1beta1.Queue
	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		queue, err = ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "failed to get queue %s", q)
			return err
		}
		queue.Status.State = schedulingv1beta1.QueueStateClosed
		if _, err = ctx.Vcclient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), queue, metav1.UpdateOptions{}); err != nil {
			return err
		}
		return nil
	})
	Expect(retryErr).NotTo(HaveOccurred(), "failed to update status of queue %s", q)
	err = wait.Poll(100*time.Millisecond, FiveMinute, queueClosedAndNoPod(ctx, q))
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
		var queue *schedulingv1beta1.Queue
		var err error
		retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			queue, err = ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), "failed to get queue %s", q)
				return err
			}
			queue.Spec.Reclaimable = &reclaimable
			if _, err = ctx.Vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{}); err != nil {
				return err
			}
			return nil
		})
		Expect(retryErr).NotTo(HaveOccurred(), "failed to update queue %s", q)
	}
}

func WaitQueueStatus(condition func() (bool, error)) error {
	return wait.Poll(100*time.Millisecond, TenMinute, condition)
}

// queueClosedAndNoPod returns whether the Queue is closed
func queueClosedAndNoPod(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if queue.Status.State != schedulingv1beta1.QueueStateClosed {
			return false, nil
		}

		if allocated, ok := queue.Status.Allocated[v1.ResourcePods]; ok && !allocated.IsZero() {
			return false, nil
		}

		return true, nil
	}
}
