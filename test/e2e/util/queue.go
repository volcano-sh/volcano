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

func CreateQueues(ctx *TestContext) {
	By("Creating Queues")

	for _, queue := range ctx.Queues {
		CreateQueue(ctx, queue, ctx.DeservedResource[queue], ctx.CapabilityResource[queue], ctx.QueueParent[queue])
	}

	// wait for all queues state open
	time.Sleep(3 * time.Second)
}

// DeleteQueue is the single-queue path. For hierarchical teardown use
// deleteQueues, which closes from the topmost parent and deletes deepest-first.
func DeleteQueue(ctx *TestContext, q string) {
	deleteJobsInQueue(ctx, q)
	closeQueueStatus(ctx, q)
	deleteQueueAndWait(ctx, q)
}

// deleteQueues tears down ctx.Queues respecting hierarchical parent/child
// relationships declared via ctx.QueueParent:
//
//  1. Clean vcjobs in every queue (single List call).
//  2. UpdateStatus(Closed) only on top-level queues; the controller
//     cascade-closes descendants.
//  3. Wait for every queue we own to reach Closed.
//  4. Delete deepest-first so a parent is never deleted before its children.
//
// Callers no longer need to hand-order ctx.Queues children-before-parents.
func deleteQueues(ctx *TestContext) {
	if len(ctx.Queues) == 0 {
		return
	}

	deleteJobsInQueues(ctx, ctx.Queues)

	for _, q := range topLevelQueues(ctx) {
		closeQueueStatus(ctx, q)
	}

	for _, q := range ctx.Queues {
		waitQueueClosed(ctx, q)
	}

	for _, q := range queuesDeepestFirst(ctx) {
		deleteQueueAndWait(ctx, q)
	}
}

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

func deleteJobsInQueue(ctx *TestContext, q string) {
	deleteJobsInQueues(ctx, []string{q})
}

// deleteJobsInQueues lists vcjobs once and deletes those whose Spec.Queue is in
// queues, so hierarchical teardown does not pay an extra List per queue.
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

		delErr := wait.PollUntilContextTimeout(context.TODO(), time.Second, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
			_, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(pollCtx, job.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return true, nil
			}
			// Any other error (including transient client-side rate limiting
			// while a whole hierarchy is torn down at once) is not fatal here;
			// keep polling until the job is gone or the timeout fires.
			return false, nil
		})
		Expect(delErr).NotTo(HaveOccurred(), "failed waiting vcjob %s deleted", job.Name)
	}
}

// closeQueueStatus retries on conflict; non-retryable errors propagate out as
// retryErr so the outer Expect surfaces them.
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

// waitQueueClosed treats NotFound as success — a queue further along than
// Closed (already deleted by an earlier iteration, an overlapping cleanup, or
// manual intervention) still satisfies the wait's intent.
func waitQueueClosed(ctx *TestContext, q string) {
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(pollCtx, q, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			// Transient client/API errors (notably client-side rate limiting
			// when a whole hierarchy is torn down at once) must not abort
			// cleanup; keep polling until the queue is Closed/gone or timeout.
			return false, nil
		}
		return queue.Status.State == schedulingv1beta1.QueueStateClosed, nil
	})
	Expect(err).NotTo(HaveOccurred(), "failed waiting for queue %s to be closed", q)
}

// deleteQueueAndWait treats an already-gone queue as success so partial-cleanup
// re-runs don't fail spuriously.
func deleteQueueAndWait(ctx *TestContext, q string) {
	foreground := metav1.DeletePropagationForeground

	err := ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), q, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred(), "failed to delete queue %s", q)

	err = wait.PollUntilContextTimeout(context.TODO(), time.Second, TwoMinute, true, func(pollCtx context.Context) (bool, error) {
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(pollCtx, q, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		// Any other error (including transient client-side rate limiting while
		// a whole hierarchy is torn down at once) is not fatal here; keep
		// polling until the queue is gone or the timeout fires.
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(), "failed waiting for queue %s to be deleted", q)
}

// topLevelQueues returns the queues a caller would close to trigger a cascade
// across the whole sub-hierarchy this test owns: parent is "" / "root", or
// parent is some external queue that lives outside ctx.Queues.
func topLevelQueues(ctx *TestContext) []string {
	queueSet := make(map[string]struct{}, len(ctx.Queues))
	for _, q := range ctx.Queues {
		queueSet[q] = struct{}{}
	}

	top := make([]string, 0, len(ctx.Queues))
	for _, q := range ctx.Queues {
		parent := ctx.QueueParent[q]
		if parent == "" || parent == "root" {
			top = append(top, q)
			continue
		}
		if _, ok := queueSet[parent]; !ok {
			top = append(top, q)
		}
	}
	return top
}

// queueDepth walks parent links up to a root-equivalent ancestor. We cap
// iterations at len(ctx.QueueParent)+1 so a misconfigured ctx.QueueParent
// (cycle) can't hang the helper.
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

func queuesDeepestFirst(ctx *TestContext) []string {
	queues := make([]string, len(ctx.Queues))
	copy(queues, ctx.Queues)
	sort.Slice(queues, func(i, j int) bool {
		return queueDepth(ctx, queues[i]) > queueDepth(ctx, queues[j])
	})
	return queues
}
