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
	"sync"
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
	vchelpers "volcano.sh/apis/pkg/apis/helpers"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	queueReadyTimeout      = 2 * time.Minute
	workloadReadyTimeout   = 5 * time.Minute
	workloadPendingTimeout = 90 * time.Second
	stateStabilityWindow   = 15 * time.Second
	pollInterval           = 200 * time.Millisecond
	apiRequestTimeout      = 10 * time.Second
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
				Command: "sleep 300",
				Min:     1,
				Rep:     1,
				Req:     e2eutil.CPUResource("10m"),
			}},
		})

		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return queue.Status.Running > 0 && hasAllocatedResource(
				queue.Status.Allocated, corev1.ResourceCPU, resource.MustParse("10m"),
			)
		})).NotTo(HaveOccurred())
	})

	It("uses the default Cluster Queue when parent is omitted", func() {
		fixture := newNamespaceQueueFixture()
		name := uniqueName("nq-default-parent")
		queue := newNamespaceQueue(fixture.ctx.Namespace, name, "")
		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Create(
			context.Background(), queue, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		Expect(waitNamespaceQueueReady(fixture.ctx, name)).NotTo(HaveOccurred())
		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-default-parent-job"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + name,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 30",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
	})

	It("schedules a PodGroup that directly references a NamespaceQueue", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)
		podGroupName := uniqueName("nq-pg")
		podName := uniqueName("nq-pod")

		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podGroupName,
					Namespace: fixture.ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        "namespace/" + namespaceQueueName,
					MinMember:    1,
					MinResources: resourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		pod := e2eutil.CreatePod(fixture.ctx, e2eutil.PodSpec{
			Name:          podName,
			Req:           e2eutil.CPUResource("10m"),
			Image:         e2eutil.DefaultBusyBoxImage,
			Command:       []string{"sh", "-c", "sleep 30"},
			SchedulerName: e2eutil.SchedulerName,
			RestartPolicy: corev1.RestartPolicyNever,
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: podGroupName,
			},
		})
		Expect(waitNamespaceQueuePodScheduled(fixture.ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())

		Expect(fixture.ctx.Kubeclient.CoreV1().Pods(fixture.ctx.Namespace).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(waitPodDeleted(fixture.ctx, pod.Name)).NotTo(HaveOccurred())
		Expect(fixture.ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.ctx.Namespace).Delete(
			context.Background(), podGroupName, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(waitPodGroupDeleted(fixture.ctx, podGroupName)).NotTo(HaveOccurred())
	})

	It("resolves a NamespaceQueue from the namespace queue annotation", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)
		queueReference := "namespace/" + namespaceQueueName

		Expect(setNamespaceQueueAnnotation(fixture.ctx, queueReference)).NotTo(HaveOccurred())
		podGroupName := uniqueName("nq-annotation-pg")
		podName := uniqueName("nq-annotation-pod")
		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podGroupName,
					Namespace: fixture.ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        schedulingv1beta1.DefaultQueue,
					MinMember:    1,
					MinResources: resourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(waitNamespaceQueuePodGroupQueue(fixture.ctx, podGroupName, queueReference)).NotTo(HaveOccurred())

		pod := e2eutil.CreatePod(fixture.ctx, e2eutil.PodSpec{
			Name:          podName,
			Req:           e2eutil.CPUResource("10m"),
			Image:         e2eutil.DefaultBusyBoxImage,
			Command:       []string{"sh", "-c", "sleep 30"},
			SchedulerName: e2eutil.SchedulerName,
			RestartPolicy: corev1.RestartPolicyNever,
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: podGroupName,
			},
		})
		Expect(waitNamespaceQueuePodScheduled(fixture.ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())
	})

	It("resolves a NamespaceQueue from a Pod queue annotation", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)
		queueReference := "namespace/" + namespaceQueueName

		pod := e2eutil.CreatePod(fixture.ctx, e2eutil.PodSpec{
			Name:          uniqueName("nq-pod-annotation"),
			Req:           e2eutil.CPUResource("10m"),
			Image:         e2eutil.DefaultBusyBoxImage,
			Command:       []string{"sh", "-c", "sleep 30"},
			SchedulerName: e2eutil.SchedulerName,
			RestartPolicy: corev1.RestartPolicyNever,
			Annotations: map[string]string{
				schedulingv1beta1.QueueNameAnnotationKey: queueReference,
			},
		})
		podGroupName := vchelpers.GeneratePodgroupName(pod)
		Expect(waitNamespaceQueuePodGroupQueue(fixture.ctx, podGroupName, queueReference)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueuePodScheduled(fixture.ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())
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
				Command: "sleep 30",
				Min:     1,
				Rep:     1,
				Req:     e2eutil.CPUResource("10m"),
			}},
		})

		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
	})

	It("propagates parent state across concurrent NamespaceQueue descendants", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueue("cluster/" + queueName)
		const childCount = 8

		childNames := make([]string, childCount)
		for i := range childNames {
			childNames[i] = uniqueName(fmt.Sprintf("nq-fanout-%d", i))
		}
		errorsCh := make(chan error, childCount)
		var group sync.WaitGroup
		for _, childName := range childNames {
			childName := childName
			group.Add(1)
			go func() {
				defer group.Done()
				errorsCh <- retryNamespaceQueueOperation(func(operationCtx context.Context) error {
					_, err := fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Create(
						operationCtx, newNamespaceQueue(fixture.ctx.Namespace, childName, parentName), metav1.CreateOptions{},
					)
					if apierrors.IsAlreadyExists(err) {
						return nil
					}
					return err
				})
			}()
		}
		group.Wait()
		close(errorsCh)
		for err := range errorsCh {
			Expect(err).NotTo(HaveOccurred())
		}

		for _, childName := range childNames {
			Expect(waitNamespaceQueueReady(fixture.ctx, childName)).NotTo(HaveOccurred())
		}
	})

	It("enforces an ancestor Queue capability for NamespaceQueue workloads", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueueWithCapability(
			[]string{fixture.ctx.Namespace},
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)

		firstJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-capacity-first"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(waitNamespaceQueueJobReady(fixture.ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-capacity-second"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(waitNamespaceQueueJobPending(fixture.ctx, secondJob)).NotTo(HaveOccurred())
		Expect(deleteNamespaceQueueJob(fixture.ctx, firstJob)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueJobReady(fixture.ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("enforces a NamespaceQueue capability for directly referenced workloads", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueueWithCapability(
			"cluster/"+queueName,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)

		firstJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name: uniqueName("nq-direct-capacity-first"), Namespace: fixture.ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(waitNamespaceQueueJobReady(fixture.ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name: uniqueName("nq-direct-capacity-second"), Namespace: fixture.ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(waitNamespaceQueueJobPending(fixture.ctx, secondJob)).NotTo(HaveOccurred())
		Expect(deleteNamespaceQueueJob(fixture.ctx, firstJob)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueJobReady(fixture.ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("enforces a parent NamespaceQueue capability for descendant workloads", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueueWithCapability(
			"cluster/"+queueName,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)
		childName := fixture.createNamespaceQueue(parentName)

		firstJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-parent-capacity-first"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + childName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(waitNamespaceQueueJobReady(fixture.ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-parent-capacity-second"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + childName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(waitNamespaceQueueJobPending(fixture.ctx, secondJob)).NotTo(HaveOccurred())
		Expect(deleteNamespaceQueueJob(fixture.ctx, firstJob)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueJobReady(fixture.ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("enforces aggregate child guarantees against a parent NamespaceQueue", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueueWithResources(
			"cluster/"+queueName, nil,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}, nil,
		)
		firstChild := fixture.createNamespaceQueueWithResources(
			parentName, nil,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("60m")}, nil,
		)
		Expect(waitNamespaceQueueReady(fixture.ctx, firstChild)).NotTo(HaveOccurred())

		secondChild := newNamespaceQueue(fixture.ctx.Namespace, uniqueName("nq-guarantee-second"), parentName)
		secondChild.Spec.Guarantee.Resource = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("60m"),
		}
		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Create(
			context.Background(), secondChild, metav1.CreateOptions{},
		)
		if err != nil {
			Expect(err.Error()).To(Or(
				ContainSubstring("sum of child guarantees"),
				ContainSubstring(commonutil.NamespaceQueueReasonParentConstraintViolation),
			))
			return
		}
		Expect(waitNamespaceQueueCondition(
			fixture.ctx, secondChild.Name, commonutil.NamespaceQueueReadyCondition,
			metav1.ConditionFalse, commonutil.NamespaceQueueReasonParentConstraintViolation,
		)).NotTo(HaveOccurred())
	})

	It("requires manual workload draining before NamespaceQueue deletion", func() {
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
		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return len(queue.Status.Allocated) > 0 || len(queue.Status.Reservation.Nodes) > 0
		})).NotTo(HaveOccurred())

		err := fixture.deleteNamespaceQueue(namespaceQueueName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be drained"))

		Expect(deleteNamespaceQueueJob(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return commonutil.IsNamespaceQueueDrained(queue.Status)
		})).NotTo(HaveOccurred())
		Expect(fixture.deleteNamespaceQueueEventually(namespaceQueueName)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueDeleted(fixture.ctx, namespaceQueueName)).NotTo(HaveOccurred())
	})

	It("does not block drain on a completed workload", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)
		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-completed-job"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 1",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})

		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueJobCompleted(fixture.ctx, job)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueue(fixture.ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return commonutil.IsNamespaceQueueDrained(queue.Status)
		})).NotTo(HaveOccurred())
		Expect(fixture.deleteNamespaceQueueEventually(namespaceQueueName)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueDeleted(fixture.ctx, namespaceQueueName)).NotTo(HaveOccurred())
	})

	It("protects a parent NamespaceQueue from deletion while it has children", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueue("cluster/" + queueName)
		childName := fixture.createNamespaceQueue(parentName)

		err := fixture.deleteNamespaceQueue(parentName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("child NamespaceQueues"))

		Expect(fixture.deleteNamespaceQueueEventually(childName)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueDeleted(fixture.ctx, childName)).NotTo(HaveOccurred())
		Expect(fixture.deleteNamespaceQueueEventually(parentName)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueDeleted(fixture.ctx, parentName)).NotTo(HaveOccurred())
	})

	It("recovers NamespaceQueue reconciliation after controller restart", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parentName := fixture.createNamespaceQueue("cluster/" + queueName)
		childName := fixture.createNamespaceQueue(parentName)

		Expect(restartVolcanoComponent("app=volcano-controller")).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueReady(fixture.ctx, childName)).NotTo(HaveOccurred())
		err := fixture.deleteNamespaceQueue(parentName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("child NamespaceQueues"))
	})

	It("recovers pending NamespaceQueue workloads after scheduler restart", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueueWithCapability(
			[]string{fixture.ctx.Namespace},
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)
		firstJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name: uniqueName("nq-restart-first"), Namespace: fixture.ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(waitNamespaceQueueJobReady(fixture.ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name: uniqueName("nq-restart-second"), Namespace: fixture.ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(waitNamespaceQueueJobPending(fixture.ctx, secondJob)).NotTo(HaveOccurred())
		Expect(restartVolcanoComponent("app=volcano-scheduler")).NotTo(HaveOccurred())
		Expect(deleteNamespaceQueueJob(fixture.ctx, firstJob)).NotTo(HaveOccurred())
		Expect(waitNamespaceQueueJobReady(fixture.ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("preserves default Queue authorization across scheduler restart and supports explicit migration", func() {
		fixture := newNamespaceQueueFixture()
		defaultQueue, err := fixture.ctx.Vcclient.SchedulingV1beta1().Queues().Get(
			context.Background(), schedulingv1beta1.DefaultQueue, metav1.GetOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		originalAllowedNamespaces := append([]string(nil), defaultQueue.Spec.AllowedNamespaces...)

		// Restore the cluster-scoped Queue only after attached NamespaceQueues are removed.
		DeferCleanup(func() {
			fixture.cleanupJobs()
			fixture.cleanupPodGroupsAndPods()
			fixture.cleanupNamespaceQueues()
			Expect(updateClusterQueueAllowedNamespaces(
				fixture.ctx, schedulingv1beta1.DefaultQueue, originalAllowedNamespaces,
			)).NotTo(HaveOccurred())
		})

		Expect(updateClusterQueueAllowedNamespaces(
			fixture.ctx, schedulingv1beta1.DefaultQueue, nil,
		)).NotTo(HaveOccurred())
		Expect(restartVolcanoComponent("app=volcano-scheduler")).NotTo(HaveOccurred())

		currentDefaultQueue, err := fixture.ctx.Vcclient.SchedulingV1beta1().Queues().Get(
			context.Background(), schedulingv1beta1.DefaultQueue, metav1.GetOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(currentDefaultQueue.Spec.AllowedNamespaces).To(BeEmpty())

		unauthorized := newNamespaceQueue(
			fixture.ctx.Namespace, uniqueName("nq-upgrade-unauthorized"), "cluster/default",
		)
		Expect(waitNamespaceQueueRejected(fixture.ctx, unauthorized, "not allowed")).NotTo(HaveOccurred())

		Expect(updateClusterQueueAllowedNamespaces(
			fixture.ctx, schedulingv1beta1.DefaultQueue, []string{"*"},
		)).NotTo(HaveOccurred())
		namespaceQueueName := fixture.createNamespaceQueue("cluster/default")

		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name: uniqueName("nq-upgrade-job"), Namespace: fixture.ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 30",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
	})

	It("supports wildcard NamespaceQueue authorization", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{"*"})
		namespaceQueueName := fixture.createNamespaceQueue("cluster/" + queueName)

		Expect(waitNamespaceQueueCondition(
			fixture.ctx,
			namespaceQueueName,
			commonutil.NamespaceQueueAuthorizedCondition,
			metav1.ConditionTrue,
			commonutil.NamespaceQueueReasonNamespaceAllowed,
		)).NotTo(HaveOccurred())
	})

	It("protects Cluster Queue deletion while a NamespaceQueue is attached", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		fixture.createNamespaceQueue("cluster/" + queueName)

		err := fixture.ctx.Vcclient.SchedulingV1beta1().Queues().Delete(
			context.Background(), queueName, metav1.DeleteOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("NamespaceQueues"))
	})

	It("keeps legacy cluster Queue and queue annotation paths working", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		podGroupName := uniqueName("cluster-pg")
		podName := uniqueName("cluster-pod")

		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podGroupName,
					Namespace: fixture.ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        queueName,
					MinMember:    1,
					MinResources: resourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		pod := e2eutil.CreatePod(fixture.ctx, e2eutil.PodSpec{
			Name:          podName,
			Req:           e2eutil.CPUResource("10m"),
			Image:         e2eutil.DefaultBusyBoxImage,
			Command:       []string{"sh", "-c", "sleep 30"},
			SchedulerName: e2eutil.SchedulerName,
			RestartPolicy: corev1.RestartPolicyNever,
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: podGroupName,
				schedulingv1beta1.QueueNameAnnotationKey:     queueName,
			},
		})
		Expect(waitNamespaceQueuePodScheduled(fixture.ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())
		Expect(fixture.ctx.Kubeclient.CoreV1().Pods(fixture.ctx.Namespace).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(waitPodDeleted(fixture.ctx, pod.Name)).NotTo(HaveOccurred())
		Expect(fixture.ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.ctx.Namespace).Delete(
			context.Background(), podGroupName, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(waitPodGroupDeleted(fixture.ctx, podGroupName)).NotTo(HaveOccurred())
	})

	It("rejects invalid NamespaceQueue parent relationships", func() {
		fixture := newNamespaceQueueFixture()
		cases := []struct {
			name          string
			parent        string
			expectedError string
		}{
			{
				name:          "missing-parent",
				parent:        "cluster/does-not-exist",
				expectedError: "parent Queue",
			},
			{
				name:          "root-parent",
				parent:        "cluster/root",
				expectedError: "cannot be used as a NamespaceQueue parent",
			},
			{
				name:          "self-parent",
				parent:        "self-parent",
				expectedError: "cycle",
			},
		}

		for _, tc := range cases {
			tc := tc
			By("rejecting " + tc.name)
			name := uniqueName(tc.name)
			parent := tc.parent
			if tc.name == "self-parent" {
				parent = name
			}
			queue := newNamespaceQueue(fixture.ctx.Namespace, name, parent)
			_, err := fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Create(
				context.Background(), queue, metav1.CreateOptions{},
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(tc.expectedError))
		}
	})

	It("rejects NamespaceQueue hierarchies beyond the configured depth", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parent := "cluster/" + queueName
		for level := 0; level < commonutil.DefaultMaxNamespaceQueueDepth; level++ {
			parent = fixture.createNamespaceQueue(parent)
		}

		overLimit := newNamespaceQueue(
			fixture.ctx.Namespace,
			uniqueName("nq-depth-limit"),
			parent,
		)
		_, err := fixture.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.ctx.Namespace).Create(
			context.Background(), overLimit, metav1.CreateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("depth"))
	})

	It("schedules a workload at the maximum NamespaceQueue depth", func() {
		fixture := newNamespaceQueueFixture()
		queueName := fixture.createClusterQueue([]string{fixture.ctx.Namespace})
		parent := "cluster/" + queueName
		for level := 0; level < commonutil.DefaultMaxNamespaceQueueDepth; level++ {
			parent = fixture.createNamespaceQueue(parent)
		}

		job := e2eutil.CreateJob(fixture.ctx, &e2eutil.JobSpec{
			Name:      uniqueName("nq-max-depth-job"),
			Namespace: fixture.ctx.Namespace,
			Queue:     "namespace/" + parent,
			Tasks: []e2eutil.TaskSpec{{
				Name:    "worker",
				Img:     e2eutil.DefaultBusyBoxImage,
				Command: "sleep 30",
				Min:     1,
				Rep:     1,
				Req:     e2eutil.CPUResource("10m"),
			}},
		})

		Expect(waitNamespaceQueueJobReady(fixture.ctx, job)).NotTo(HaveOccurred())
	})
})

func newNamespaceQueueFixture() *namespaceQueueFixture {
	ctx := e2eutil.InitTestContext(e2eutil.Options{Namespace: uniqueName("nq-e2e")})
	fixture := &namespaceQueueFixture{ctx: ctx}
	DeferCleanup(func() {
		fixture.cleanupJobs()
		fixture.cleanupPodGroupsAndPods()
		fixture.cleanupNamespaceQueues()
		e2eutil.CleanupTestContext(ctx)
	})
	return fixture
}

func restartVolcanoComponent(labelSelector string) error {
	requestCtx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	pods, err := e2eutil.KubeClient.CoreV1().Pods(metav1.NamespaceAll).List(
		requestCtx, metav1.ListOptions{LabelSelector: labelSelector},
	)
	cancel()
	if err != nil {
		return fmt.Errorf("list Volcano component Pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no Volcano component Pod found with selector %q", labelSelector)
	}

	var target *corev1.Pod
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil {
			target = &pods.Items[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("all Volcano component Pods are terminating with selector %q", labelSelector)
	}
	oldUID := string(target.UID)
	targetNamespace := target.Namespace
	requestCtx, cancel = context.WithTimeout(context.Background(), apiRequestTimeout)
	err = e2eutil.KubeClient.CoreV1().Pods(targetNamespace).Delete(
		requestCtx, target.Name, metav1.DeleteOptions{},
	)
	cancel()
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Volcano component Pod %s/%s: %w", target.Namespace, target.Name, err)
	}

	return wait.PollUntilContextTimeout(context.Background(), pollInterval, workloadReadyTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			requestCtx, cancel := context.WithTimeout(pollCtx, apiRequestTimeout)
			defer cancel()
			current, err := e2eutil.KubeClient.CoreV1().Pods(metav1.NamespaceAll).List(
				requestCtx, metav1.ListOptions{LabelSelector: labelSelector},
			)
			if err != nil {
				return false, err
			}
			for i := range current.Items {
				pod := &current.Items[i]
				if string(pod.UID) != oldUID && pod.DeletionTimestamp == nil &&
					pod.Status.Phase == corev1.PodRunning && podReady(pod) {
					return true, nil
				}
			}
			return false, nil
		})
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (f *namespaceQueueFixture) createClusterQueue(allowedNamespaces []string) string {
	return f.createClusterQueueWithCapability(allowedNamespaces, nil)
}

func (f *namespaceQueueFixture) createClusterQueueWithCapability(
	allowedNamespaces []string, capability corev1.ResourceList,
) string {
	name := uniqueName("nq-parent")
	queue := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:            1,
			Parent:            "root",
			AllowedNamespaces: allowedNamespaces,
			Capability:        capability,
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
	return f.createNamespaceQueueWithCapability(parent, nil)
}

func (f *namespaceQueueFixture) createNamespaceQueueWithCapability(
	parent string, capability corev1.ResourceList,
) string {
	return f.createNamespaceQueueWithResources(parent, capability, nil, nil)
}

func (f *namespaceQueueFixture) createNamespaceQueueWithResources(
	parent string,
	capability, guarantee, deserved corev1.ResourceList,
) string {
	name := uniqueName("nq")
	err := retryNamespaceQueueOperation(func(operationCtx context.Context) error {
		queue := newNamespaceQueue(f.ctx.Namespace, name, parent)
		queue.Spec.Capability = capability
		queue.Spec.Guarantee.Resource = guarantee
		queue.Spec.Deserved = deserved
		_, err := f.ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(f.ctx.Namespace).Create(
			operationCtx, queue, metav1.CreateOptions{},
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
		Expect(deleteNamespaceQueueJob(f.ctx, job)).NotTo(HaveOccurred(),
			"failed to delete Job %s", job.Name)
	}
}

func (f *namespaceQueueFixture) cleanupPodGroupsAndPods() {
	pods, err := f.ctx.Kubeclient.CoreV1().Pods(f.ctx.Namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to list Pods during cleanup")
	for i := range pods.Items {
		err := f.ctx.Kubeclient.CoreV1().Pods(f.ctx.Namespace).Delete(
			context.Background(), pods.Items[i].Name, metav1.DeleteOptions{},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to delete Pod %s", pods.Items[i].Name)
		}
		Expect(waitPodDeleted(f.ctx, pods.Items[i].Name)).NotTo(HaveOccurred(),
			"failed to wait for Pod %s deletion", pods.Items[i].Name)
	}

	podGroups, err := f.ctx.Vcclient.SchedulingV1beta1().PodGroups(f.ctx.Namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to list PodGroups during cleanup")
	for i := range podGroups.Items {
		err := f.ctx.Vcclient.SchedulingV1beta1().PodGroups(f.ctx.Namespace).Delete(
			context.Background(), podGroups.Items[i].Name, metav1.DeleteOptions{},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to delete PodGroup %s", podGroups.Items[i].Name)
		}
		Expect(waitPodGroupDeleted(f.ctx, podGroups.Items[i].Name)).NotTo(HaveOccurred(),
			"failed to wait for PodGroup %s deletion", podGroups.Items[i].Name)
	}
}

func waitJobResourcesDeleted(ctx *e2eutil.TestContext, job *vcbatch.Job) error {
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

func waitNamespaceQueueJobCompleted(ctx *e2eutil.TestContext, job *vcbatch.Job) error {
	return waitNamespaceQueueJobPhase(ctx, job, vcbatch.Completed, workloadReadyTimeout)
}

func waitNamespaceQueueJobPhase(
	ctx *e2eutil.TestContext,
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
	snapshot, snapshotErr := namespaceQueueJobSnapshot(ctx, job)
	if snapshotErr != nil {
		return fmt.Errorf("wait for Job %s/%s phase %s: %w; snapshot unavailable: %v",
			job.Namespace, job.Name, want, err, snapshotErr)
	}
	return fmt.Errorf("wait for Job %s/%s phase %s: %w; %s",
		job.Namespace, job.Name, want, err, snapshot)
}

func waitNamespaceQueuePodGroupQueue(
	ctx *e2eutil.TestContext, name, queueReference string,
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

func setNamespaceQueueAnnotation(ctx *e2eutil.TestContext, queueReference string) error {
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

func waitNamespaceQueueJobReady(ctx *e2eutil.TestContext, job *vcbatch.Job) error {
	return waitNamespaceQueueJobPodsInPhase(
		ctx, job, []corev1.PodPhase{corev1.PodRunning, corev1.PodSucceeded},
		int(job.Spec.MinAvailable), workloadReadyTimeout, true,
	)
}

func waitNamespaceQueueJobPending(ctx *e2eutil.TestContext, job *vcbatch.Job) error {
	return waitNamespaceQueueJobPodsInPhase(
		ctx, job, []corev1.PodPhase{corev1.PodPending},
		int(job.Spec.MinAvailable), workloadPendingTimeout, false,
	)
}

func waitNamespaceQueueJobPodsInPhase(
	ctx *e2eutil.TestContext,
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

	snapshot, snapshotErr := namespaceQueueJobSnapshot(ctx, job)
	if snapshotErr != nil {
		return fmt.Errorf("wait for Job %s/%s pods in phases %v: %w; snapshot unavailable: %v",
			job.Namespace, job.Name, phases, err, snapshotErr)
	}
	return fmt.Errorf("wait for Job %s/%s pods in phases %v: %w; %s",
		job.Namespace, job.Name, phases, err, snapshot)
}

func waitNamespaceQueuePodScheduled(
	ctx *e2eutil.TestContext, podGroupName, podName string,
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
			return e2eutil.IsPodScheduled(pod), nil
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

func namespaceQueueJobSnapshot(ctx *e2eutil.TestContext, job *vcbatch.Job) (string, error) {
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

func hasAllocatedResource(
	resources corev1.ResourceList,
	name corev1.ResourceName,
	minimum resource.Quantity,
) bool {
	quantity, found := resources[name]
	return found && quantity.Cmp(minimum) >= 0
}

func deleteNamespaceQueueJob(ctx *e2eutil.TestContext, job *vcbatch.Job) error {
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

func waitNamespaceQueueDeleted(ctx *e2eutil.TestContext, name string) error {
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

func waitNamespaceQueueRejected(
	ctx *e2eutil.TestContext,
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

func retryNamespaceQueueOperation(operation func(context.Context) error) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, queueReadyTimeout, true,
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

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, strings.ToLower(string(uuid.NewUUID())[:8]))
}

func resourceListPointer(resources corev1.ResourceList) *corev1.ResourceList {
	return &resources
}

func waitPodDeleted(ctx *e2eutil.TestContext, name string) error {
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

func waitPodGroupDeleted(ctx *e2eutil.TestContext, name string) error {
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

func jobPodsUnbound(ctx *e2eutil.TestContext, job *vcbatch.Job) bool {
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

func waitNamespaceQueueCondition(
	ctx *e2eutil.TestContext,
	name, conditionType string,
	status metav1.ConditionStatus,
	reason string,
) error {
	return waitNamespaceQueue(ctx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
		condition := apiMeta.FindStatusCondition(queue.Status.Conditions, conditionType)
		return condition != nil &&
			condition.Status == status &&
			condition.ObservedGeneration == queue.Generation &&
			(reason == "" || condition.Reason == reason)
	})
}

func updateClusterQueueAllowedNamespaces(
	ctx *e2eutil.TestContext,
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
