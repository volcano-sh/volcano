/*
Copyright 2026 The Volcano Authors.

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

package namespacequeue

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	namespaceQueueFinalizer = "scheduling.volcano.sh/namespacequeue-protection"
	queueReadyTimeout       = 2 * time.Minute
)

type namespaceQueueFixture struct {
	ctx *e2eutil.TestContext
}

var _ = Describe("NamespaceQueue", func() {
	It("creates a ready NamespaceQueue and schedules a workload through it", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)

		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-job"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name:    "worker",
				Img:     e2eutil.DefaultBusyBoxImage,
				Command: "sleep 2",
				Min:     1,
				Rep:     1,
				Req:     e2eutil.CPUResource("10m"),
			}},
		})

		Expect(e2eutil.WaitJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return queue.Status.Running > 0 || queue.Status.Completed > 0
		})).NotTo(HaveOccurred())
	})

	It("rejects a NamespaceQueue when its cluster Queue does not authorize the namespace", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{"another-namespace"})

		unauthorized := newNamespaceQueue(
			fixture.ctx.Namespace, uniqueName("unauthorized"), "cluster/"+queueName,
		)
		Expect(waitNamespaceQueueRejected(fixture.ctx, unauthorized, "not allowed")).NotTo(HaveOccurred())
	})

	It("propagates readiness through a NamespaceQueue hierarchy", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueue("cluster/" + queueName)
		childName := fixture.createNamespaceQueue(parentName)

		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-hierarchy-job"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + childName,
			Tasks: []e2eutil.TaskSpec{{
				Name:    "worker",
				Img:     e2eutil.DefaultBusyBoxImage,
				Command: "sleep 2",
				Min:     1,
				Rep:     1,
				Req:     e2eutil.CPUResource("10m"),
			}},
		})

		Expect(e2eutil.WaitJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
	})

	It("marks NamespaceQueue descendants not ready when the parent closes", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueue("cluster/" + queueName)
		childName := fixture.createNamespaceQueue(parentName)

		requestNamespaceQueueClose(fixture.ctx, parentName)
		Expect(waitNamespaceQueueState(
			fixture.ctx, parentName, schedulingv1beta1.QueueStateClosed,
		)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, childName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			ready := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueReadyCondition)
			return queue.Status.State == schedulingv1beta1.QueueStateOpen &&
				ready != nil &&
				ready.Status == metav1.ConditionFalse &&
				ready.Reason == commonutil.NamespaceQueueReasonParentNotReady
		})).NotTo(HaveOccurred())
	})

	It("protects Parent changes until the NamespaceQueue is closed and drained", func() {
		fixture := newNamespaceQueueFixture()
		firstQueue := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		secondQueue := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + firstQueue)

		current := getNamespaceQueue(fixture.ctx, namespaceQueueName)
		current.Spec.Parent = "cluster/" + secondQueue
		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Update(
			context.Background(), current, metav1.UpdateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(commonutil.NamespaceQueueReasonParentChangeRequiresDrain))

		closeNamespaceQueue(fixture.ctx, namespaceQueueName)
		var updated *schedulingv1beta1.NamespaceQueue
		Expect(retryNamespaceQueueOperation(func(operationCtx context.Context) error {
			current, err = fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Get(
				operationCtx, namespaceQueueName, metav1.GetOptions{},
			)
			if err != nil {
				return err
			}
			current.Spec.Parent = "cluster/" + secondQueue
			current.Spec.State = schedulingv1beta1.QueueStateOpen
			updated, err = fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Update(
				operationCtx, current, metav1.UpdateOptions{},
			)
			return err
		})).NotTo(HaveOccurred())
		Expect(updated.Spec.Parent).To(Equal("cluster/" + secondQueue))
		Expect(waitNamespaceQueueReady(fixture.ctx, namespaceQueueName)).NotTo(HaveOccurred())
	})

	It("closes, deletes, and removes the NamespaceQueue finalizer", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)
		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-drain-job"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name:    "worker",
				Img:     e2eutil.DefaultBusyBoxImage,
				Command: "sleep 300",
				Min:     1,
				Rep:     1,
				Req:     e2eutil.CPUResource("10m"),
			}},
		})
		Expect(e2eutil.WaitJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return len(queue.Status.Allocated) > 0 || len(queue.Status.Reservation.Nodes) > 0
		})).NotTo(HaveOccurred())

		namespaceQueue := getNamespaceQueue(fixture.ctx, namespaceQueueName)
		Expect(namespaceQueue.Finalizers).To(ContainElement(namespaceQueueFinalizer))

		requestNamespaceQueueClose(fixture.ctx, namespaceQueueName)
		Expect(waitNamespaceQueueState(
			fixture.ctx, namespaceQueueName, schedulingv1beta1.QueueStateClosing,
		)).NotTo(HaveOccurred())
		err := fixture.deleteNamespaceQueue(namespaceQueueName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be closed and drained"))

		Expect(fixture.ctx.Vcclient.BatchV1alpha1().Jobs(fixture.ctx.Namespace).Delete(
			context.Background(), job.Name, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueState(
			fixture.ctx, namespaceQueueName, schedulingv1beta1.QueueStateClosed,
		)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return commonutil.IsNamespaceQueueRuntimeDrained(queue.Status)
		})).NotTo(HaveOccurred())
		Expect(fixture.deleteNamespaceQueueEventually(namespaceQueueName)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueDeleted(fixture.ctx, namespaceQueueName)).NotTo(HaveOccurred())
	})
})

func newNamespaceQueueFixture() *namespaceQueueFixture {
	ctx := e2eutil.InitTestContext(e2eutil.Options{Namespace: uniqueName("nq-e2e")})
	fixture := &namespaceQueueFixture{ctx: ctx}
	DeferCleanup(func() {
		fixture.cleanupJobs()
		fixture.cleanupNamespaceQueues()
		e2eutil.CleanupTestContext(ctx)
	})
	return fixture
}

func (f *namespaceQueueFixture) createClusterQueue(allowedNamespaces []string) string {
	name := uniqueName("nq-parent")
	queue := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:            1,
			Parent:            "root",
			AllowedNamespaces: allowedNamespaces,
		},
	}
	_, err := f.ctx.Vcclient.SchedulingV1beta1().Queues().Create(
		context.Background(), queue, metav1.CreateOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to create Queue %s", name)
	f.ctx.Queues = append(f.ctx.Queues, name)
	Expect(waitClusterQueueOpen(f.ctx, name)).NotTo(HaveOccurred())
	return name
}

func (f *namespaceQueueFixture) createNamespaceQueue(parent string) string {
	name := uniqueName("nq")
	err := retryNamespaceQueueOperation(func(operationCtx context.Context) error {
		_, err := f.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.ctx.Namespace).Create(
			operationCtx, newNamespaceQueue(f.ctx.Namespace, name, parent), metav1.CreateOptions{},
		)
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	})
	Expect(err).NotTo(HaveOccurred(), "failed to create NamespaceQueue %s/%s", f.ctx.Namespace, name)
	Expect(waitNamespaceQueueReady(f.ctx, name)).NotTo(HaveOccurred())
	return name
}

func newNamespaceQueue(namespace, name, parent string) *schedulingv1beta1.NamespaceQueue {
	return &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"volcano.sh/e2e": "namespacequeue",
			},
		},
		Spec: schedulingv1beta1.NamespaceQueueSpec{
			Parent: parent,
			State:  schedulingv1beta1.QueueStateOpen,
		},
	}
}

func (f *namespaceQueueFixture) cleanupJobs() {
	jobs, err := f.ctx.Vcclient.BatchV1alpha1().Jobs(f.ctx.Namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to list Jobs during cleanup")
	for i := range jobs.Items {
		job := jobs.Items[i].DeepCopy()
		err = f.ctx.Vcclient.BatchV1alpha1().Jobs(f.ctx.Namespace).Delete(
			context.Background(), job.Name, metav1.DeleteOptions{},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to delete Job %s", job.Name)
		}
		Expect(waitJobResourcesDeleted(f.ctx, job)).NotTo(HaveOccurred())
	}
}

func waitJobResourcesDeleted(ctx *e2eutil.TestContext, job *vcbatch.Job) error {
	pgName := job.Name + "-" + string(job.UID)
	return wait.PollUntilContextTimeout(context.Background(), 200*time.Millisecond, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			_, jobErr := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(
				pollCtx, job.Name, metav1.GetOptions{},
			)
			if jobErr != nil && !apierrors.IsNotFound(jobErr) {
				return false, jobErr
			}
			if jobErr == nil {
				return false, nil
			}

			_, pgErr := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
				pollCtx, pgName, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(pgErr) {
				return true, nil
			}
			return false, pgErr
		})
}

func (f *namespaceQueueFixture) cleanupNamespaceQueues() {
	for {
		queues, err := f.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.ctx.Namespace).List(
			context.Background(), metav1.ListOptions{LabelSelector: "volcano.sh/e2e=namespacequeue"},
		)
		Expect(err).NotTo(HaveOccurred(), "failed to list NamespaceQueues during cleanup")
		if len(queues.Items) == 0 {
			return
		}

		sort.Slice(queues.Items, func(i, j int) bool {
			return queues.Items[i].Name > queues.Items[j].Name
		})
		progress := false
		for i := range queues.Items {
			queue := &queues.Items[i]
			if queue.DeletionTimestamp != nil {
				Expect(waitNamespaceQueueDeleted(f.ctx, queue.Name)).NotTo(HaveOccurred())
				progress = true
				continue
			}
			if hasChild(queues.Items, queue.Name) {
				continue
			}

			closeNamespaceQueue(f.ctx, queue.Name)
			err = f.deleteNamespaceQueueEventually(queue.Name)
			Expect(err).NotTo(HaveOccurred(), "failed to delete NamespaceQueue %s/%s", queue.Namespace, queue.Name)
			Expect(waitNamespaceQueueDeleted(f.ctx, queue.Name)).NotTo(HaveOccurred())
			progress = true
		}
		if !progress {
			Fail("NamespaceQueue cleanup made no progress")
		}
	}
}

func hasChild(queues []schedulingv1beta1.NamespaceQueue, parent string) bool {
	for i := range queues {
		if queues[i].Spec.Parent == parent && queues[i].Name != parent {
			return true
		}
	}
	return false
}

func (f *namespaceQueueFixture) deleteNamespaceQueue(name string) error {
	return f.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.ctx.Namespace).Delete(
		context.Background(), name, metav1.DeleteOptions{},
	)
}

func (f *namespaceQueueFixture) deleteNamespaceQueueEventually(name string) error {
	return retryNamespaceQueueOperation(func(operationCtx context.Context) error {
		err := f.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.ctx.Namespace).Delete(
			operationCtx, name, metav1.DeleteOptions{},
		)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
}

func closeNamespaceQueue(ctx *e2eutil.TestContext, name string) {
	requestNamespaceQueueClose(ctx, name)
	Expect(waitNamespaceQueueState(ctx, name, schedulingv1beta1.QueueStateClosed)).NotTo(HaveOccurred())
}

func requestNamespaceQueueClose(ctx *e2eutil.TestContext, name string) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		queue, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
			context.Background(), name, metav1.GetOptions{},
		)
		if err != nil {
			return err
		}
		if commonutil.EffectiveNamespaceQueueState(queue.Spec.State) == schedulingv1beta1.QueueStateClosed {
			return nil
		}
		queue.Spec.State = schedulingv1beta1.QueueStateClosed
		_, err = ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Update(
			context.Background(), queue, metav1.UpdateOptions{},
		)
		return err
	})
	Expect(err).NotTo(HaveOccurred(), "failed to close NamespaceQueue %s/%s", ctx.Namespace, name)
}

func getNamespaceQueue(ctx *e2eutil.TestContext, name string) *schedulingv1beta1.NamespaceQueue {
	queue, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to get NamespaceQueue %s/%s", ctx.Namespace, name)
	return queue
}

func waitNamespaceQueueReady(ctx *e2eutil.TestContext, name string) error {
	return waitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		authorized := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueAuthorizedCondition)
		ready := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueReadyCondition)
		return queue.Status.State == schedulingv1beta1.QueueStateOpen &&
			authorized != nil && authorized.Status == metav1.ConditionTrue &&
			authorized.ObservedGeneration == queue.Generation &&
			ready != nil && ready.Status == metav1.ConditionTrue &&
			ready.ObservedGeneration == queue.Generation
	})
}

func waitNamespaceQueueState(ctx *e2eutil.TestContext, name string, state schedulingv1beta1.QueueState) error {
	return waitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		return queue.Status.State == state
	})
}

func waitNamespaceQueue(ctx *e2eutil.TestContext, name string, predicate func(*schedulingv1beta1.NamespaceQueue) bool) error {
	return wait.PollUntilContextTimeout(context.Background(), 200*time.Millisecond, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
				pollCtx, name, metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}
			return predicate(queue), nil
		})
}

func waitNamespaceQueueDeleted(ctx *e2eutil.TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), 200*time.Millisecond, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			_, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
				pollCtx, name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
}

func waitNamespaceQueueRejected(
	ctx *e2eutil.TestContext,
	queue *schedulingv1beta1.NamespaceQueue,
	expectedMessage string,
) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), 200*time.Millisecond, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			_, createErr := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(queue.Namespace).Create(
				pollCtx, queue, metav1.CreateOptions{},
			)
			if createErr == nil || apierrors.IsAlreadyExists(createErr) {
				return false, fmt.Errorf("NamespaceQueue %s/%s was unexpectedly accepted", queue.Namespace, queue.Name)
			}
			lastErr = createErr
			return strings.Contains(createErr.Error(), expectedMessage), nil
		})
	if err != nil && lastErr != nil {
		return fmt.Errorf("%w: last admission error: %v", err, lastErr)
	}
	return err
}

func retryNamespaceQueueOperation(operation func(context.Context) error) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), 200*time.Millisecond, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			lastErr = operation(pollCtx)
			return lastErr == nil, nil
		})
	if err != nil && lastErr != nil {
		return fmt.Errorf("%w: last operation error: %v", err, lastErr)
	}
	return err
}

func waitClusterQueueOpen(ctx *e2eutil.TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), 200*time.Millisecond, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(
				pollCtx, name, metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, strings.ToLower(string(uuid.NewUUID())[:8]))
}
