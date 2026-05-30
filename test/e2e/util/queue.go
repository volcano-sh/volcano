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
	"sort"
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
	Name               string
	Weight             int32
	CapabilityResource v1.ResourceList
	GuaranteeResource  v1.ResourceList
	DeservedResource   v1.ResourceList
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
		if len(queueSpec.CapabilityResource) != 0 {
			queue.Spec.Capability = queueSpec.CapabilityResource
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
func CreateQueue(ctx *TestContext, q string, deservedResource, capabilityResource v1.ResourceList, parent string) {
	_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
	if err != nil {
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: q,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight:     1,
				Parent:     parent,
				Deserved:   deservedResource,
				Capability: capabilityResource,
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create queue %s", q)
	}
}

// CreateQueues create Queues specified in the test context
func CreateQueues(ctx *TestContext) {
	By("Creating Queues")

	for _, queue := range ctx.Queues {
		CreateQueue(ctx, queue, ctx.DeservedResource[queue], ctx.CapabilityResource[queue], ctx.QueueParent[queue])
	}

	// wait for all queues state open
	time.Sleep(3 * time.Second)
}

// DeleteQueue deletes a single queue. For hierarchical cleanup prefer deleteQueues, which
// closes from the topmost parent and deletes deepest-first.
func DeleteQueue(ctx *TestContext, q string) {
	deleteJobsInQueue(ctx, q)
	closeQueueStatus(ctx, q)
	deleteQueueObject(ctx, q)
}

// deleteQueues deletes all queues in ctx.Queues. For hierarchical queues it closes from
// the topmost parent first, waits for the controller cascade to close children, then
// deletes deepest-first so callers do not need to hand-order ctx.Queues.
func deleteQueues(ctx *TestContext) {
	for _, q := range ctx.Queues {
		deleteJobsInQueue(ctx, q)
	}

	for _, q := range topLevelQueues(ctx) {
		closeQueueStatus(ctx, q)
	}

	for _, q := range ctx.Queues {
		err := wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(pollCtx, q, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return queue.Status.State == schedulingv1beta1.QueueStateClosed, nil
		})
		Expect(err).NotTo(HaveOccurred(), "failed waiting for queue %s to be closed", q)
	}

	for _, q := range sortQueuesByDepthDesc(ctx) {
		deleteQueueObject(ctx, q)
	}
}

func deleteJobsInQueue(ctx *TestContext, q string) {
	foreground := metav1.DeletePropagationForeground

	jobs, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		Expect(err).NotTo(HaveOccurred(), "failed to list vcjobs")
	}

	for _, job := range jobs.Items {
		if job.Spec.Queue == q {
			err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})
			Expect(err).NotTo(HaveOccurred(), "failed to delete vcjob %s in queue %s", job.Name, q)

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
}

func closeQueueStatus(ctx *TestContext, q string) {
	var queue *schedulingv1beta1.Queue
	var err error
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
}

func deleteQueueObject(ctx *TestContext, q string) {
	foreground := metav1.DeletePropagationForeground

	err := ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), q,
		metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})
	Expect(err).NotTo(HaveOccurred(), "failed to delete queue %s", q)

	err = wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(pollCtx, q, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(), "failed waiting for queue %s to be deleted", q)
}

func topLevelQueues(ctx *TestContext) []string {
	queueSet := make(map[string]struct{}, len(ctx.Queues))
	for _, q := range ctx.Queues {
		queueSet[q] = struct{}{}
	}

	topLevel := make([]string, 0)
	for _, q := range ctx.Queues {
		parent := ctx.QueueParent[q]
		if parent == "" || parent == "root" {
			topLevel = append(topLevel, q)
			continue
		}
		if _, ok := queueSet[parent]; !ok {
			topLevel = append(topLevel, q)
		}
	}
	return topLevel
}

func queueDepthInContext(ctx *TestContext, q string) int {
	depth := 1
	parent := ctx.QueueParent[q]
	for parent != "" && parent != "root" {
		depth++
		parent = ctx.QueueParent[parent]
	}
	return depth
}

func sortQueuesByDepthDesc(ctx *TestContext) []string {
	queues := append([]string(nil), ctx.Queues...)
	sort.Slice(queues, func(i, j int) bool {
		return queueDepthInContext(ctx, queues[i]) > queueDepthInContext(ctx, queues[j])
	})
	return queues
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
