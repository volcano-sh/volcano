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

package util

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
)

const (
	queueReadyTimeout      = 2 * time.Minute
	workloadReadyTimeout   = 5 * time.Minute
	workloadPendingTimeout = 90 * time.Second
	pollInterval           = 200 * time.Millisecond
	operationPollInterval  = time.Second
	apiRequestTimeout      = 10 * time.Second
)

// NamespaceQueueFixture owns the namespace and resources shared by one
// NamespaceQueue E2E scenario.
type NamespaceQueueFixture struct {
	Ctx *TestContext
}

func NewNamespaceQueueFixture() *NamespaceQueueFixture {
	ctx := InitTestContext(Options{Namespace: UniqueName("nq-e2e")})
	fixture := &NamespaceQueueFixture{Ctx: ctx}
	DeferCleanup(func() {
		fixture.CleanupJobs()
		fixture.CleanupPodGroupsAndPods()
		fixture.CleanupNamespaceQueues()
		CleanupTestContext(ctx)
	})
	return fixture
}

// VolcanoNamespace returns the namespace used by the E2E harness for Volcano
// components. The harness defaults to volcano-system but allows overrides.
func VolcanoNamespace() string {
	if namespace := os.Getenv("NAMESPACE"); namespace != "" {
		return namespace
	}
	return "volcano-system"
}

func (f *NamespaceQueueFixture) CreateClusterQueue(allowedNamespaces []string) string {
	return f.CreateClusterQueueWithCapability(allowedNamespaces, nil)
}

func (f *NamespaceQueueFixture) CreateClusterQueueWithCapability(
	allowedNamespaces []string, capability corev1.ResourceList,
) string {
	name := UniqueName("nq-parent")
	queue := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:            1,
			Parent:            "root",
			AllowedNamespaces: allowedNamespaces,
			Capability:        capability,
		},
	}
	_, err := f.Ctx.Vcclient.SchedulingV1beta1().Queues().Create(
		context.Background(), queue, metav1.CreateOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to create Queue %s", name)
	f.Ctx.Queues = append(f.Ctx.Queues, name)
	Expect(WaitClusterQueueOpen(f.Ctx, name)).NotTo(HaveOccurred())
	return name
}

func (f *NamespaceQueueFixture) CreateNamespaceQueue(parent string) string {
	return f.CreateNamespaceQueueWithCapability(parent, nil)
}

func (f *NamespaceQueueFixture) CreateNamespaceQueueWithCapability(
	parent string, capability corev1.ResourceList,
) string {
	return f.CreateNamespaceQueueWithResources(parent, capability, nil, nil)
}

func (f *NamespaceQueueFixture) CreateNamespaceQueueWithResources(
	parent string,
	capability, guarantee, deserved corev1.ResourceList,
) string {
	name := UniqueName("nq")
	err := RetryNamespaceQueueOperation(func(operationCtx context.Context) error {
		queue := NewNamespaceQueue(f.Ctx.Namespace, name, parent)
		queue.Spec.Capability = capability
		queue.Spec.Guarantee.Resource = guarantee
		queue.Spec.Deserved = deserved
		_, err := f.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.Ctx.Namespace).Create(
			operationCtx, queue, metav1.CreateOptions{},
		)
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	})
	Expect(err).NotTo(HaveOccurred(), "failed to create NamespaceQueue %s/%s", f.Ctx.Namespace, name)
	Expect(waitNamespaceQueueReady(f.Ctx, name)).NotTo(HaveOccurred())
	return name
}

func NewNamespaceQueue(namespace, name, parent string) *schedulingv1beta1.NamespaceQueue {
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
		},
	}
}

func (f *NamespaceQueueFixture) CleanupJobs() {
	jobs, err := f.Ctx.Vcclient.BatchV1alpha1().Jobs(f.Ctx.Namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to list Jobs during cleanup")
	for i := range jobs.Items {
		job := jobs.Items[i].DeepCopy()
		Expect(DeleteNamespaceQueueJob(f.Ctx, job)).NotTo(HaveOccurred(),
			"failed to delete Job %s", job.Name)
	}
}

func (f *NamespaceQueueFixture) CleanupPodGroupsAndPods() {
	pods, err := f.Ctx.Kubeclient.CoreV1().Pods(f.Ctx.Namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to list Pods during cleanup")
	for i := range pods.Items {
		err := f.Ctx.Kubeclient.CoreV1().Pods(f.Ctx.Namespace).Delete(
			context.Background(), pods.Items[i].Name, metav1.DeleteOptions{},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to delete Pod %s", pods.Items[i].Name)
		}
		Expect(WaitPodDeleted(f.Ctx, pods.Items[i].Name)).NotTo(HaveOccurred(),
			"failed to wait for Pod %s deletion", pods.Items[i].Name)
	}

	podGroups, err := f.Ctx.Vcclient.SchedulingV1beta1().PodGroups(f.Ctx.Namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to list PodGroups during cleanup")
	for i := range podGroups.Items {
		err := f.Ctx.Vcclient.SchedulingV1beta1().PodGroups(f.Ctx.Namespace).Delete(
			context.Background(), podGroups.Items[i].Name, metav1.DeleteOptions{},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to delete PodGroup %s", podGroups.Items[i].Name)
		}
		Expect(WaitPodGroupDeleted(f.Ctx, podGroups.Items[i].Name)).NotTo(HaveOccurred(),
			"failed to wait for PodGroup %s deletion", podGroups.Items[i].Name)
	}
}

func waitJobResourcesDeleted(ctx *TestContext, job *vcbatch.Job) error {
	pgName := job.Name + "-" + string(job.UID)
	return wait.PollUntilContextTimeout(context.Background(), pollInterval, workloadReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			_, jobErr := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(
				requestCtx, job.Name, metav1.GetOptions{},
			)
			if jobErr != nil && !apierrors.IsNotFound(jobErr) {
				return false, jobErr
			}
			if jobErr == nil {
				return false, nil
			}

			pods, podsErr := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(
				requestCtx, metav1.ListOptions{},
			)
			if podsErr != nil {
				return false, podsErr
			}
			for i := range pods.Items {
				if metav1.IsControlledBy(&pods.Items[i], job) {
					return false, nil
				}
			}

			_, pgErr := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
				requestCtx, pgName, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(pgErr) {
				return true, nil
			}
			return false, pgErr
		})
}

func WaitNamespaceQueueJobCompleted(ctx *TestContext, job *vcbatch.Job) error {
	return WaitNamespaceQueueJobPhase(ctx, job, vcbatch.Completed, workloadReadyTimeout)
}

func WaitNamespaceQueueJobPhase(
	ctx *TestContext,
	job *vcbatch.Job,
	want vcbatch.JobPhase,
	timeout time.Duration,
) error {
	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, timeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			current, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(
				requestCtx, job.Name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return current.Status.State.Phase == want, nil
		})
	if err == nil {
		return nil
	}
	snapshot, snapshotErr := NamespaceQueueJobSnapshot(ctx, job)
	if snapshotErr != nil {
		return fmt.Errorf("wait for Job %s/%s phase %s: %w; snapshot unavailable: %v",
			job.Namespace, job.Name, want, err, snapshotErr)
	}
	return fmt.Errorf("wait for Job %s/%s phase %s: %w; %s",
		job.Namespace, job.Name, want, err, snapshot)
}

func WaitNamespaceQueuePodGroupQueue(
	ctx *TestContext, name, queueReference string,
) error {
	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(
				requestCtx, name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return podGroup.Spec.Queue == queueReference, nil
		})
	if err == nil {
		return nil
	}
	return fmt.Errorf("wait for PodGroup %s/%s queue %q: %w",
		ctx.Namespace, name, queueReference, err)
}

func SetNamespaceQueueAnnotation(ctx *TestContext, queueReference string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
		defer cancel()
		namespace, err := ctx.Kubeclient.CoreV1().Namespaces().Get(
			requestCtx, ctx.Namespace, metav1.GetOptions{},
		)
		if err != nil {
			return err
		}
		if namespace.Annotations == nil {
			namespace.Annotations = make(map[string]string)
		}
		namespace.Annotations[schedulingv1beta1.QueueNameAnnotationKey] = queueReference
		_, err = ctx.Kubeclient.CoreV1().Namespaces().Update(
			requestCtx, namespace, metav1.UpdateOptions{},
		)
		return err
	})
}

func WaitNamespaceQueueJobReady(ctx *TestContext, job *vcbatch.Job) error {
	return WaitNamespaceQueueJobPodsInPhase(
		ctx, job, []corev1.PodPhase{corev1.PodRunning, corev1.PodSucceeded},
		int(job.Spec.MinAvailable), workloadReadyTimeout, true,
	)
}

func WaitNamespaceQueueJobPending(ctx *TestContext, job *vcbatch.Job) error {
	err := wait.PollUntilContextTimeout(context.Background(), operationPollInterval, workloadPendingTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()

			currentJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(
				requestCtx, job.Name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				if isTransientAPIError(err) {
					return false, nil
				}
				return false, err
			}

			podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
				requestCtx, job.Name+"-"+string(job.UID), metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				if isTransientAPIError(err) {
					return false, nil
				}
				return false, err
			}

			// A queue-capacity rejection can keep both objects Pending without
			// creating any Pod. The Job and PodGroup phases are the workload state.
			return currentJob.Status.State.Phase == vcbatch.Pending &&
				podGroup.Status.Phase == schedulingv1beta1.PodGroupPending, nil
		})
	if err == nil {
		return nil
	}
	snapshot, snapshotErr := NamespaceQueueJobSnapshot(ctx, job)
	if snapshotErr != nil {
		return fmt.Errorf("wait for Job %s/%s pending: %w; snapshot unavailable: %v",
			job.Namespace, job.Name, err, snapshotErr)
	}
	return fmt.Errorf("wait for Job %s/%s pending: %w; %s", job.Namespace, job.Name, err, snapshot)
}

func WaitNamespaceQueueJobPodsInPhase(
	ctx *TestContext,
	job *vcbatch.Job,
	phases []corev1.PodPhase,
	expected int,
	timeout time.Duration,
	ready bool,
) error {
	if expected <= 0 {
		return nil
	}

	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, timeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(
				requestCtx, metav1.ListOptions{},
			)
			if err != nil {
				return false, err
			}
			currentJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(
				requestCtx, job.Name, metav1.GetOptions{},
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
				requestCtx, job.Name+"-"+string(job.UID), metav1.GetOptions{},
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}

			matched := 0
			unschedulable := false
			for i := range pods.Items {
				pod := &pods.Items[i]
				if !metav1.IsControlledBy(pod, job) || !podMatchesPhase(pod, phases) {
					continue
				}
				matched++
				if !ready && podIsUnschedulable(pod) {
					unschedulable = true
				}
			}
			if matched < expected {
				return false, nil
			}
			if ready {
				return (currentJob.Status.State.Phase == vcbatch.Running ||
					currentJob.Status.State.Phase == vcbatch.Completed) &&
					(podGroup.Status.Phase == schedulingv1beta1.PodGroupRunning ||
						podGroup.Status.Phase == schedulingv1beta1.PodGroupCompleted), nil
			}
			return currentJob.Status.State.Phase != vcbatch.Running &&
				currentJob.Status.State.Phase != vcbatch.Completed &&
				podGroup.Status.Phase != schedulingv1beta1.PodGroupRunning &&
				podGroup.Status.Phase != schedulingv1beta1.PodGroupCompleted &&
				unschedulable, nil
		})
	if err == nil {
		return nil
	}

	snapshot, snapshotErr := NamespaceQueueJobSnapshot(ctx, job)
	if snapshotErr != nil {
		return fmt.Errorf("wait for Job %s/%s pods in phases %v: %w; snapshot unavailable: %v",
			job.Namespace, job.Name, phases, err, snapshotErr)
	}
	return fmt.Errorf("wait for Job %s/%s pods in phases %v: %w; %s",
		job.Namespace, job.Name, phases, err, snapshot)
}

func WaitNamespaceQueuePodScheduled(
	ctx *TestContext, podGroupName, podName string,
) error {
	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, workloadReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				requestCtx, podName, metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}
			return IsPodScheduled(pod), nil
		})
	if err == nil {
		return nil
	}

	requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	pod, podErr := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
		requestCtx, podName, metav1.GetOptions{},
	)
	if podErr != nil {
		return fmt.Errorf("wait for Pod %s/%s scheduled: %w; pod snapshot unavailable: %v",
			ctx.Namespace, podName, err, podErr)
	}
	return fmt.Errorf("wait for Pod %s/%s in PodGroup %s scheduled: %w; phase=%s node=%q conditions=%v",
		ctx.Namespace, podName, podGroupName, err, pod.Status.Phase, pod.Spec.NodeName, pod.Status.Conditions)
}

func NamespaceQueueJobSnapshot(ctx *TestContext, job *vcbatch.Job) (string, error) {
	requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()

	currentJob, jobErr := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(
		requestCtx, job.Name, metav1.GetOptions{},
	)
	pods, podsErr := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(
		requestCtx, metav1.ListOptions{},
	)
	pgName := job.Name + "-" + string(job.UID)
	podGroup, pgErr := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
		requestCtx, pgName, metav1.GetOptions{},
	)
	queueState := "unavailable"
	if namespaceQueueName := strings.TrimPrefix(job.Spec.Queue, "namespace/"); namespaceQueueName != job.Spec.Queue {
		queue, queueErr := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(job.Namespace).Get(
			requestCtx, namespaceQueueName, metav1.GetOptions{},
		)
		if queueErr != nil {
			queueState = fmt.Sprintf("error=%v", queueErr)
		} else {
			queueState = fmt.Sprintf("state=%s generation=%d conditions=%v allocated=%v reservation=%v",
				queue.Status.State, queue.Generation, queue.Status.Conditions,
				queue.Status.Allocated, queue.Status.Reservation)
		}
	}

	var jobState string
	if jobErr != nil {
		jobState = fmt.Sprintf("error=%v", jobErr)
	} else {
		jobState = fmt.Sprintf("phase=%s pending=%d running=%d",
			currentJob.Status.State.Phase, currentJob.Status.Pending, currentJob.Status.Running)
	}

	podStates := make([]string, 0)
	if podsErr != nil {
		podStates = append(podStates, fmt.Sprintf("error=%v", podsErr))
	} else {
		for i := range pods.Items {
			pod := &pods.Items[i]
			if metav1.IsControlledBy(pod, job) {
				podStates = append(podStates, fmt.Sprintf("%s:%s(node=%q)",
					pod.Name, pod.Status.Phase, pod.Spec.NodeName))
			}
		}
		sort.Strings(podStates)
	}

	var podGroupState string
	if pgErr != nil {
		podGroupState = fmt.Sprintf("error=%v", pgErr)
	} else {
		podGroupState = fmt.Sprintf("phase=%s running=%d succeeded=%d failed=%d conditions=%v",
			podGroup.Status.Phase, podGroup.Status.Running, podGroup.Status.Succeeded,
			podGroup.Status.Failed, podGroup.Status.Conditions)
	}

	return fmt.Sprintf("job=%s {%s}; queue={%s}; pods=[%s]; podGroup=%s {%s}",
		job.Name, jobState, queueState, strings.Join(podStates, ", "), pgName, podGroupState), nil
}

func podPhaseIn(phase corev1.PodPhase, phases []corev1.PodPhase) bool {
	for _, candidate := range phases {
		if phase == candidate {
			return true
		}
	}
	return false
}

func podMatchesPhase(pod *corev1.Pod, phases []corev1.PodPhase) bool {
	if !podPhaseIn(pod.Status.Phase, phases) {
		return false
	}
	return pod.Status.Phase != corev1.PodPending || pod.Spec.NodeName == ""
}

func podIsUnschedulable(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == corev1.PodReasonUnschedulable {
			return true
		}
	}
	return false
}

func HasAllocatedResource(
	resources corev1.ResourceList,
	name corev1.ResourceName,
	minimum resource.Quantity,
) bool {
	quantity, found := resources[name]
	return found && quantity.Cmp(minimum) >= 0
}

func DeleteNamespaceQueueJob(ctx *TestContext, job *vcbatch.Job) error {
	requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(
		requestCtx, job.Name, metav1.DeleteOptions{},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return waitJobResourcesDeleted(ctx, job)
}

func (f *NamespaceQueueFixture) CleanupNamespaceQueues() {
	for {
		queues, err := f.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.Ctx.Namespace).List(
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
				Expect(WaitNamespaceQueueDeleted(f.Ctx, queue.Name)).NotTo(HaveOccurred())
				progress = true
				continue
			}
			if hasChild(queues.Items, queue.Name) {
				continue
			}

			Expect(WaitNamespaceQueue(f.Ctx, queue.Name, func(namespaceQueue *schedulingv1beta1.NamespaceQueue) bool {
				return commonutil.IsNamespaceQueueDrained(namespaceQueue.Status)
			})).NotTo(HaveOccurred(), "NamespaceQueue %s/%s did not become drained", queue.Namespace, queue.Name)
			err = f.DeleteNamespaceQueueEventually(queue.Name)
			Expect(err).NotTo(HaveOccurred(), "failed to delete NamespaceQueue %s/%s", queue.Namespace, queue.Name)
			Expect(WaitNamespaceQueueDeleted(f.Ctx, queue.Name)).NotTo(HaveOccurred())
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

func (f *NamespaceQueueFixture) DeleteNamespaceQueue(name string) error {
	return f.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.Ctx.Namespace).Delete(
		context.Background(), name, metav1.DeleteOptions{},
	)
}

func (f *NamespaceQueueFixture) DeleteNamespaceQueueEventually(name string) error {
	return RetryNamespaceQueueOperation(func(operationCtx context.Context) error {
		err := f.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.Ctx.Namespace).Delete(
			operationCtx, name, metav1.DeleteOptions{},
		)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
}

func getNamespaceQueue(ctx *TestContext, name string) *schedulingv1beta1.NamespaceQueue {
	queue, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to get NamespaceQueue %s/%s", ctx.Namespace, name)
	return queue
}

func waitNamespaceQueueReady(ctx *TestContext, name string) error {
	return WaitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		authorized := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueAuthorizedCondition)
		ready := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueReadyCondition)
		return queue.Status.State == schedulingv1beta1.QueueStateOpen &&
			authorized != nil && authorized.Status == metav1.ConditionTrue &&
			authorized.ObservedGeneration == queue.Generation &&
			ready != nil && ready.Status == metav1.ConditionTrue &&
			ready.ObservedGeneration == queue.Generation
	})
}

func WaitNamespaceQueueState(ctx *TestContext, name string, state schedulingv1beta1.QueueState) error {
	return WaitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		return queue.Status.State == state
	})
}

func WaitNamespaceQueue(ctx *TestContext, name string, predicate func(*schedulingv1beta1.NamespaceQueue) bool) error {
	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			queue, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
				requestCtx, name, metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}
			return predicate(queue), nil
		})
	if err == nil {
		return nil
	}

	requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	queue, snapshotErr := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
		requestCtx, name, metav1.GetOptions{},
	)
	if snapshotErr != nil {
		return fmt.Errorf("wait for NamespaceQueue %s/%s: %w; snapshot unavailable: %v",
			ctx.Namespace, name, err, snapshotErr)
	}
	return fmt.Errorf("wait for NamespaceQueue %s/%s: %w; state=%s generation=%d conditions=%v allocated=%v reservation=%v",
		ctx.Namespace, name, err, queue.Status.State, queue.Generation, queue.Status.Conditions,
		queue.Status.Allocated, queue.Status.Reservation)
}

func WaitNamespaceQueueReady(ctx *TestContext, name string) error {
	return WaitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		authorized := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueAuthorizedCondition)
		ready := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueReadyCondition)
		return queue.Status.State == schedulingv1beta1.QueueStateOpen &&
			authorized != nil && authorized.Status == metav1.ConditionTrue &&
			authorized.ObservedGeneration == queue.Generation &&
			ready != nil && ready.Status == metav1.ConditionTrue &&
			ready.ObservedGeneration == queue.Generation
	})
}

func WaitNamespaceQueueDeleted(ctx *TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			_, err := ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(ctx.Namespace).Get(
				requestCtx, name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
}

func WaitNamespaceQueueRejected(
	ctx *TestContext,
	queue *schedulingv1beta1.NamespaceQueue,
	expectedMessage string,
) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
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

func RetryNamespaceQueueOperation(operation func(context.Context) error) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), operationPollInterval, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			attemptCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			lastErr = operation(attemptCtx)
			cancel()
			if lastErr == nil {
				return true, nil
			}
			if !isTransientAPIError(lastErr) {
				return false, lastErr
			}
			return false, nil
		})
	if err != nil && lastErr != nil {
		if !isTransientAPIError(lastErr) {
			return lastErr
		}
		return fmt.Errorf("%w: last operation error: %v", err, lastErr)
	}
	return err
}

func isTransientAPIError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err)
}

func WaitClusterQueueOpen(ctx *TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
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

func UniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, strings.ToLower(string(uuid.NewUUID())[:8]))
}

func ContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ResourceListPointer(resources corev1.ResourceList) *corev1.ResourceList {
	return &resources
}

func WaitPodDeleted(ctx *TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				requestCtx, name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
}

func WaitPodGroupDeleted(ctx *TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(
				requestCtx, name, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
}

func JobPodsUnbound(ctx *TestContext, job *vcbatch.Job) bool {
	requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()
	pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(
		requestCtx, metav1.ListOptions{},
	)
	if err != nil {
		return false
	}

	ownedPods := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !metav1.IsControlledBy(pod, job) {
			continue
		}
		ownedPods++
		if pod.Spec.NodeName != "" {
			return false
		}
	}
	return ownedPods > 0
}

func WaitNamespaceQueueCondition(
	ctx *TestContext,
	name, conditionType string,
	status metav1.ConditionStatus,
	reason string,
) error {
	return WaitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		condition := apiMeta.FindStatusCondition(queue.Status.Conditions, conditionType)
		return condition != nil &&
			condition.Status == status &&
			condition.ObservedGeneration == queue.Generation &&
			(reason == "" || condition.Reason == reason)
	})
}

func UpdateClusterQueueAllowedNamespaces(
	ctx *TestContext,
	name string,
	allowedNamespaces []string,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(
			context.Background(), name, metav1.GetOptions{},
		)
		if err != nil {
			return err
		}
		queue.Spec.AllowedNamespaces = append([]string(nil), allowedNamespaces...)
		_, err = ctx.Vcclient.SchedulingV1beta1().Queues().Update(
			context.Background(), queue, metav1.UpdateOptions{},
		)
		return err
	})
}
