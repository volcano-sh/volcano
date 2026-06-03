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

// DeleteQueue deletes a single Queue with the specified name. It cleans up any
// vcjobs in the queue, transitions the queue to Closed, then deletes it and
// waits for the object to actually be removed.
//
// For tearing down a hierarchy of queues from one test context, use
// deleteQueues, which closes from the topmost parent and deletes deepest-first
// so admission's "parent has children" rule is never violated.
func DeleteQueue(ctx *TestContext, q string) {
	deleteJobsInQueue(ctx, q)
	closeQueueStatus(ctx, q)
	deleteQueueAndWait(ctx, q)
}

// deleteQueues deletes Queues specified in the test context, respecting
// hierarchical parent/child relationships declared via ctx.QueueParent.
//
// Hierarchical queues need a specific teardown sequence:
//   - A child queue cannot be deleted while the parent still references it.
//   - Admission rejects an Open child whose parent is Closed.
//   - The controller cascade-closes children when a parent moves to Closed,
//     which races with anything that touches a child queue in the meantime.
//
// To stay robust regardless of the order queues appear in ctx.Queues, this
// helper:
//
//  1. Cleans up vcjobs in every queue (before any state change).
//  2. UpdateStatus(Closed) only on top-level queues (parent == "" or "root").
//     The queue controller cascades Closed to descendants.
//  3. Waits for every queue we own to reach Closed (or be already gone).
//  4. Deletes queues in deepest-first order, waiting for NotFound after each.
//
// Callers no longer need to hand-order ctx.Queues with children before
// parents; any order works.
func deleteQueues(ctx *TestContext) {
	if len(ctx.Queues) == 0 {
		return
	}

	// 1. Clean up jobs in every queue with a single list call.
	deleteJobsInQueues(ctx, ctx.Queues)

	// 2. Close only top-level queues; the controller cascades to descendants.
	for _, q := range topLevelQueues(ctx) {
		closeQueueStatus(ctx, q)
	}

	// 3. Wait for every queue in this test context to reach Closed.
	for _, q := range ctx.Queues {
		waitQueueClosed(ctx, q)
	}

	// 4. Delete deepest-first so a parent is never deleted before its children.
	for _, q := range queuesDeepestFirst(ctx) {
		deleteQueueAndWait(ctx, q)
	}
}

// SetQueueReclaimable sets the Queue to be reclaimable
func SetQueueReclaimable(ctx *TestContext, queues []string, reclaimable bool) {
	By("Setting Queue reclaimable")

	for _, q := range queues {
		var queue *schedulingv1beta1.Queue
		var err error
		retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			queue, err = ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
			if err != nil {
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

// deleteJobsInQueue deletes every vcjob in ctx.Namespace whose Spec.Queue == q,
// waiting for each to be gone before returning.
func deleteJobsInQueue(ctx *TestContext, q string) {
	deleteJobsInQueues(ctx, []string{q})
}

// deleteJobsInQueues deletes every vcjob in ctx.Namespace whose Spec.Queue is
// any of queues, listing once and waiting per job. Use this for hierarchical
// teardown to avoid N list calls.
func deleteJobsInQueues(ctx *TestContext, queues []string) {
	if len(queues) == 0 {
		return
	}
	queueSet := make(map[string]struct{}, len(queues))
	for _, q := range queues {
		queueSet[q] = struct{}{}
	}

	foreground := metav1.DeletePropagationForeground

	jobs, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		Expect(err).NotTo(HaveOccurred(), "failed to list vcjobs")
	}

	for _, job := range jobs.Items {
		if _, ok := queueSet[job.Spec.Queue]; !ok {
			continue
		}
		err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})
		Expect(err).NotTo(HaveOccurred(), "failed to delete vcjob %s in queue %s", job.Name, job.Spec.Queue)

		delErr := wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
			_, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(pollCtx, job.Name, metav1.GetOptions{})
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

// closeQueueStatus sets queue.Status.State to Closed with retry on conflict.
// Non-retryable errors propagate out as the retryErr the outer Expect handles.
func closeQueueStatus(ctx *TestContext, q string) {
	var queue *schedulingv1beta1.Queue
	var err error
	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		queue, err = ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		if err != nil {
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

// waitQueueClosed waits until queue q reports State == Closed. If the queue is
// already gone (NotFound), that counts as closed too — a deeper queue may have
// been cleaned up by a previous iteration.
func waitQueueClosed(ctx *TestContext, q string) {
	err := wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(pollCtx, q, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return queue.Status.State == schedulingv1beta1.QueueStateClosed, nil
	})
	Expect(err).NotTo(HaveOccurred(), "failed waiting for queue %s to be closed", q)
}

// deleteQueueAndWait issues a foreground Delete and waits for the queue to be
// NotFound. A queue that's already gone is treated as success.
func deleteQueueAndWait(ctx *TestContext, q string) {
	foreground := metav1.DeletePropagationForeground

	err := ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), q, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	if errors.IsNotFound(err) {
		return
	}
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

// topLevelQueues returns the subset of ctx.Queues whose declared parent is
// empty or "root" — i.e. the queues a user would close to trigger a cascade
// across the entire hierarchy owned by this test context.
func topLevelQueues(ctx *TestContext) []string {
	top := make([]string, 0, len(ctx.Queues))
	for _, q := range ctx.Queues {
		parent := ctx.QueueParent[q]
		if parent == "" || parent == "root" {
			top = append(top, q)
		}
	}
	return top
}

// queueDepth returns the chain length from q up to a root-equivalent ancestor.
// A queue with no parent (or parent "root") has depth 0; its direct children
// have depth 1; and so on. Parents that aren't tracked in ctx.QueueParent are
// treated as root, so a depth always exists.
//
// A cycle would loop forever; we cap iterations at len(ctx.QueueParent)+1 as
// a defensive measure — a real acyclic hierarchy can't have a chain longer
// than the number of parent edges.
func queueDepth(ctx *TestContext, q string) int {
	maxIter := len(ctx.QueueParent) + 1
	depth := 0
	cur := q
	for i := 0; i < maxIter; i++ {
		parent, ok := ctx.QueueParent[cur]
		if !ok || parent == "" || parent == "root" {
			return depth
		}
		cur = parent
		depth++
	}
	return depth
}

// queuesDeepestFirst returns ctx.Queues sorted by queueDepth descending. The
// sort is stable so queues at the same depth retain their declared order. The
// returned slice is always a fresh copy — callers may iterate it without
// affecting ctx.Queues. Depths are precomputed once so the comparator does not
// re-walk parent chains.
func queuesDeepestFirst(ctx *TestContext) []string {
	queues := make([]string, len(ctx.Queues))
	copy(queues, ctx.Queues)
	depths := make(map[string]int, len(queues))
	for _, q := range queues {
		depths[q] = queueDepth(ctx, q)
	}
	sort.SliceStable(queues, func(i, j int) bool {
		return depths[queues[i]] > depths[queues[j]]
	})
	return queues
}
