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
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vchelpers "volcano.sh/apis/pkg/apis/helpers"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("NamespaceQueue", func() {
	It("creates a ready NamespaceQueue and schedules a workload through it", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)

		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-job"),
			Namespace: fixture.Ctx.Namespace,
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

		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueue(fixture.Ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return queue.Status.Running > 0 && e2eutil.HasAllocatedResource(
				queue.Status.Allocated, corev1.ResourceCPU, resource.MustParse("10m"),
			)
		})).NotTo(HaveOccurred())
	})

	It("uses the default Cluster Queue when parent is omitted", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		defaultQueue, err := fixture.Ctx.Vcclient.SchedulingV1beta1().Queues().Get(
			context.Background(), schedulingv1beta1.DefaultQueue, metav1.GetOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		originalAllowedNamespaces := append([]string(nil), defaultQueue.Spec.AllowedNamespaces...)
		allowedNamespaces := append([]string(nil), originalAllowedNamespaces...)
		if !e2eutil.ContainsString(allowedNamespaces, "*") && !e2eutil.ContainsString(allowedNamespaces, fixture.Ctx.Namespace) {
			allowedNamespaces = append(allowedNamespaces, fixture.Ctx.Namespace)
		}
		Expect(e2eutil.UpdateClusterQueueAllowedNamespaces(
			fixture.Ctx, schedulingv1beta1.DefaultQueue, allowedNamespaces,
		)).NotTo(HaveOccurred())
		DeferCleanup(func() {
			fixture.CleanupJobs()
			fixture.CleanupPodGroupsAndPods()
			fixture.CleanupNamespaceQueues()
			Expect(e2eutil.UpdateClusterQueueAllowedNamespaces(
				fixture.Ctx, schedulingv1beta1.DefaultQueue, originalAllowedNamespaces,
			)).NotTo(HaveOccurred())
		})

		name := e2eutil.UniqueName("nq-default-parent")
		queue := e2eutil.NewNamespaceQueue(fixture.Ctx.Namespace, name, "")
		err = e2eutil.RetryNamespaceQueueOperation(func(operationCtx context.Context) error {
			_, createErr := fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.Ctx.Namespace).Create(
				operationCtx, queue, metav1.CreateOptions{},
			)
			if apierrors.IsAlreadyExists(createErr) {
				return nil
			}
			return createErr
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(e2eutil.WaitNamespaceQueueReady(fixture.Ctx, name)).NotTo(HaveOccurred())
		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-default-parent-job"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + name,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 30",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
	})

	It("schedules a PodGroup that directly references a NamespaceQueue", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		podGroupName := e2eutil.UniqueName("nq-pg")
		podName := e2eutil.UniqueName("nq-pod")

		_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.Ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podGroupName,
					Namespace: fixture.Ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        "namespace/" + namespaceQueueName,
					MinMember:    1,
					MinResources: e2eutil.ResourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		pod := e2eutil.CreatePod(fixture.Ctx, e2eutil.PodSpec{
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
		Expect(e2eutil.WaitNamespaceQueuePodScheduled(fixture.Ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())

		Expect(fixture.Ctx.Kubeclient.CoreV1().Pods(fixture.Ctx.Namespace).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitPodDeleted(fixture.Ctx, pod.Name)).NotTo(HaveOccurred())
		Expect(fixture.Ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.Ctx.Namespace).Delete(
			context.Background(), podGroupName, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitPodGroupDeleted(fixture.Ctx, podGroupName)).NotTo(HaveOccurred())
	})

	It("rejects Job and PodGroup submissions to a NotReady NamespaceQueue", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		namespaceQueueName := e2eutil.UniqueName("nq-not-ready")
		queue := e2eutil.NewNamespaceQueue(fixture.Ctx.Namespace, namespaceQueueName, e2eutil.UniqueName("missing-parent"))
		_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.Ctx.Namespace).Create(
			context.Background(), queue, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueCondition(
			fixture.Ctx,
			namespaceQueueName,
			commonutil.NamespaceQueueReadyCondition,
			metav1.ConditionFalse,
			commonutil.NamespaceQueueReasonParentNotFound,
		)).NotTo(HaveOccurred())

		_, err = e2eutil.CreateJobInner(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-not-ready-job"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 30",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not ready"))

		_, err = fixture.Ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.Ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      e2eutil.UniqueName("nq-not-ready-pg"),
					Namespace: fixture.Ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        "namespace/" + namespaceQueueName,
					MinMember:    1,
					MinResources: e2eutil.ResourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not ready"))
	})

	It("resolves a NamespaceQueue from the namespace queue annotation", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		queueReference := "namespace/" + namespaceQueueName

		Expect(e2eutil.SetNamespaceQueueAnnotation(fixture.Ctx, queueReference)).NotTo(HaveOccurred())
		podGroupName := e2eutil.UniqueName("nq-annotation-pg")
		podName := e2eutil.UniqueName("nq-annotation-pod")
		_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.Ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podGroupName,
					Namespace: fixture.Ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        schedulingv1beta1.DefaultQueue,
					MinMember:    1,
					MinResources: e2eutil.ResourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueuePodGroupQueue(fixture.Ctx, podGroupName, queueReference)).NotTo(HaveOccurred())

		pod := e2eutil.CreatePod(fixture.Ctx, e2eutil.PodSpec{
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
		Expect(e2eutil.WaitNamespaceQueuePodScheduled(fixture.Ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())
	})

	It("resolves a NamespaceQueue from a Pod queue annotation", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		queueReference := "namespace/" + namespaceQueueName

		pod := e2eutil.CreatePod(fixture.Ctx, e2eutil.PodSpec{
			Name:          e2eutil.UniqueName("nq-pod-annotation"),
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
		Expect(e2eutil.WaitNamespaceQueuePodGroupQueue(fixture.Ctx, podGroupName, queueReference)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueuePodScheduled(fixture.Ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())
	})

	It("rejects a NamespaceQueue when its cluster Queue does not authorize the namespace", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{"another-namespace"})

		unauthorized := e2eutil.NewNamespaceQueue(
			fixture.Ctx.Namespace, e2eutil.UniqueName("unauthorized"), "cluster/"+queueName,
		)
		Expect(e2eutil.WaitNamespaceQueueRejected(fixture.Ctx, unauthorized, "not allowed")).NotTo(HaveOccurred())
	})

	It("propagates readiness through a NamespaceQueue hierarchy", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parentName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		childName := fixture.CreateNamespaceQueue(parentName)

		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-hierarchy-job"),
			Namespace: fixture.Ctx.Namespace,
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

		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
	})

	It("keeps same-named NamespaceQueues isolated across namespaces", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		otherNamespace := e2eutil.UniqueName("nq-isolation")
		_, err := fixture.Ctx.Kubeclient.CoreV1().Namespaces().Create(
			context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNamespace}}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = fixture.Ctx.Kubeclient.CoreV1().Namespaces().Delete(context.Background(), otherNamespace, metav1.DeleteOptions{})
		})

		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace, otherNamespace})
		name := e2eutil.UniqueName("shared-name")
		first := e2eutil.NewNamespaceQueue(fixture.Ctx.Namespace, name, "cluster/"+queueName)
		second := e2eutil.NewNamespaceQueue(otherNamespace, name, "cluster/"+queueName)
		_, err = fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(first.Namespace).Create(context.Background(), first, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(second.Namespace).Create(context.Background(), second, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(otherNamespace).Delete(context.Background(), name, metav1.DeleteOptions{})
		})

		Expect(e2eutil.WaitNamespaceQueueReady(fixture.Ctx, name)).NotTo(HaveOccurred())
		otherCtx := *fixture.Ctx
		otherCtx.Namespace = otherNamespace
		Expect(e2eutil.WaitNamespaceQueue(&otherCtx, name, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			condition := apiMeta.FindStatusCondition(queue.Status.Conditions, commonutil.NamespaceQueueReadyCondition)
			return condition != nil &&
				condition.ObservedGeneration == queue.Generation &&
				condition.Status == metav1.ConditionTrue
		})).NotTo(HaveOccurred())
	})

	It("propagates parent state across concurrent NamespaceQueue descendants", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parentName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		const childCount = 8

		childNames := make([]string, childCount)
		for i := range childNames {
			childNames[i] = e2eutil.UniqueName(fmt.Sprintf("nq-fanout-%d", i))
		}
		errorsCh := make(chan error, childCount)
		var group sync.WaitGroup
		for _, childName := range childNames {
			childName := childName
			group.Add(1)
			go func() {
				defer group.Done()
				errorsCh <- e2eutil.RetryNamespaceQueueOperation(func(operationCtx context.Context) error {
					_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.Ctx.Namespace).Create(
						operationCtx, e2eutil.NewNamespaceQueue(fixture.Ctx.Namespace, childName, parentName), metav1.CreateOptions{},
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
			Expect(e2eutil.WaitNamespaceQueueReady(fixture.Ctx, childName)).NotTo(HaveOccurred())
		}
	})

	It("enforces an ancestor Queue capability for NamespaceQueue workloads", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueueWithCapability(
			[]string{fixture.Ctx.Namespace},
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)

		firstJob := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-capacity-first"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-capacity-second"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobPending(fixture.Ctx, secondJob)).NotTo(HaveOccurred())
		Expect(e2eutil.DeleteNamespaceQueueJob(fixture.Ctx, firstJob)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("enforces a NamespaceQueue capability for directly referenced workloads", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueueWithCapability(
			"cluster/"+queueName,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)

		firstJob := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name: e2eutil.UniqueName("nq-direct-capacity-first"), Namespace: fixture.Ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name: e2eutil.UniqueName("nq-direct-capacity-second"), Namespace: fixture.Ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobPending(fixture.Ctx, secondJob)).NotTo(HaveOccurred())
		Expect(e2eutil.DeleteNamespaceQueueJob(fixture.Ctx, firstJob)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("enforces a parent NamespaceQueue capability for descendant workloads", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parentName := fixture.CreateNamespaceQueueWithCapability(
			"cluster/"+queueName,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)
		childName := fixture.CreateNamespaceQueue(parentName)

		firstJob := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-parent-capacity-first"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + childName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, firstJob)).NotTo(HaveOccurred())

		secondJob := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-parent-capacity-second"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + childName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 300",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("100m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobPending(fixture.Ctx, secondJob)).NotTo(HaveOccurred())
		Expect(e2eutil.DeleteNamespaceQueueJob(fixture.Ctx, firstJob)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, secondJob)).NotTo(HaveOccurred())
	})

	It("enforces aggregate child guarantees against a parent NamespaceQueue", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parentName := fixture.CreateNamespaceQueueWithResources(
			"cluster/"+queueName, nil,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		)
		firstChild := fixture.CreateNamespaceQueueWithResources(
			parentName, nil,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("60m")},
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("60m")},
		)
		Expect(e2eutil.WaitNamespaceQueueReady(fixture.Ctx, firstChild)).NotTo(HaveOccurred())

		secondChild := e2eutil.NewNamespaceQueue(fixture.Ctx.Namespace, e2eutil.UniqueName("nq-guarantee-second"), parentName)
		secondChild.Spec.Guarantee.Resource = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("60m"),
		}
		secondChild.Spec.Deserved = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("60m"),
		}
		_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.Ctx.Namespace).Create(
			context.Background(), secondChild, metav1.CreateOptions{},
		)
		if err != nil {
			Expect(err.Error()).To(Or(
				ContainSubstring("sum of child guarantees"),
				ContainSubstring(commonutil.NamespaceQueueReasonParentConstraintViolation),
			))
			return
		}
		Expect(e2eutil.WaitNamespaceQueueCondition(
			fixture.Ctx, parentName, commonutil.NamespaceQueueReadyCondition,
			metav1.ConditionFalse, commonutil.NamespaceQueueReasonParentConstraintViolation,
		)).NotTo(HaveOccurred())
	})

	It("requires manual workload draining before NamespaceQueue deletion", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-drain-job"),
			Namespace: fixture.Ctx.Namespace,
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
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueue(fixture.Ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return len(queue.Status.Allocated) > 0 || len(queue.Status.Reservation.Nodes) > 0
		})).NotTo(HaveOccurred())

		err := fixture.DeleteNamespaceQueue(namespaceQueueName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be drained"))

		Expect(e2eutil.DeleteNamespaceQueueJob(fixture.Ctx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueue(fixture.Ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return commonutil.IsNamespaceQueueDrained(queue.Status)
		})).NotTo(HaveOccurred())
		Expect(fixture.DeleteNamespaceQueueEventually(namespaceQueueName)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueDeleted(fixture.Ctx, namespaceQueueName)).NotTo(HaveOccurred())
	})

	It("does not block drain on a completed workload", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-completed-job"),
			Namespace: fixture.Ctx.Namespace,
			Queue:     "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 1",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})

		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueJobCompleted(fixture.Ctx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueue(fixture.Ctx, namespaceQueueName, func(queue *schedulingv1beta1.NamespaceQueue) bool {
			return commonutil.IsNamespaceQueueDrained(queue.Status)
		})).NotTo(HaveOccurred())
		Expect(fixture.DeleteNamespaceQueueEventually(namespaceQueueName)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueDeleted(fixture.Ctx, namespaceQueueName)).NotTo(HaveOccurred())
	})

	It("protects a parent NamespaceQueue from deletion while it has children", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parentName := fixture.CreateNamespaceQueue("cluster/" + queueName)
		childName := fixture.CreateNamespaceQueue(parentName)

		err := fixture.DeleteNamespaceQueue(parentName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("child NamespaceQueues"))

		Expect(fixture.DeleteNamespaceQueueEventually(childName)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueDeleted(fixture.Ctx, childName)).NotTo(HaveOccurred())
		Expect(fixture.DeleteNamespaceQueueEventually(parentName)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitNamespaceQueueDeleted(fixture.Ctx, parentName)).NotTo(HaveOccurred())
	})

	It("preserves default Queue authorization and supports explicit migration", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		defaultQueue, err := fixture.Ctx.Vcclient.SchedulingV1beta1().Queues().Get(
			context.Background(), schedulingv1beta1.DefaultQueue, metav1.GetOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		originalAllowedNamespaces := append([]string(nil), defaultQueue.Spec.AllowedNamespaces...)

		// Restore the cluster-scoped Queue only after attached NamespaceQueues are removed.
		DeferCleanup(func() {
			fixture.CleanupJobs()
			fixture.CleanupPodGroupsAndPods()
			fixture.CleanupNamespaceQueues()
			Expect(e2eutil.UpdateClusterQueueAllowedNamespaces(
				fixture.Ctx, schedulingv1beta1.DefaultQueue, originalAllowedNamespaces,
			)).NotTo(HaveOccurred())
		})

		Expect(e2eutil.UpdateClusterQueueAllowedNamespaces(
			fixture.Ctx, schedulingv1beta1.DefaultQueue, nil,
		)).NotTo(HaveOccurred())

		currentDefaultQueue, err := fixture.Ctx.Vcclient.SchedulingV1beta1().Queues().Get(
			context.Background(), schedulingv1beta1.DefaultQueue, metav1.GetOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(currentDefaultQueue.Spec.AllowedNamespaces).To(BeEmpty())

		unauthorized := e2eutil.NewNamespaceQueue(
			fixture.Ctx.Namespace, e2eutil.UniqueName("nq-upgrade-unauthorized"), "cluster/default",
		)
		Expect(e2eutil.WaitNamespaceQueueRejected(fixture.Ctx, unauthorized, "not allowed")).NotTo(HaveOccurred())

		Expect(e2eutil.UpdateClusterQueueAllowedNamespaces(
			fixture.Ctx, schedulingv1beta1.DefaultQueue, []string{"*"},
		)).NotTo(HaveOccurred())
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/default")

		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name: e2eutil.UniqueName("nq-upgrade-job"), Namespace: fixture.Ctx.Namespace,
			Queue: "namespace/" + namespaceQueueName,
			Tasks: []e2eutil.TaskSpec{{
				Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Command: "sleep 30",
				Min: 1, Rep: 1, Req: e2eutil.CPUResource("10m"),
			}},
		})
		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
	})

	It("supports wildcard NamespaceQueue authorization", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{"*"})
		namespaceQueueName := fixture.CreateNamespaceQueue("cluster/" + queueName)

		Expect(e2eutil.WaitNamespaceQueueCondition(
			fixture.Ctx,
			namespaceQueueName,
			commonutil.NamespaceQueueAuthorizedCondition,
			metav1.ConditionTrue,
			commonutil.NamespaceQueueReasonNamespaceAllowed,
		)).NotTo(HaveOccurred())
	})

	It("protects Cluster Queue deletion while a NamespaceQueue is attached", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		fixture.CreateNamespaceQueue("cluster/" + queueName)

		err := fixture.Ctx.Vcclient.SchedulingV1beta1().Queues().Delete(
			context.Background(), queueName, metav1.DeleteOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("NamespaceQueues"))
	})

	It("keeps legacy cluster Queue and queue annotation paths working", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		podGroupName := e2eutil.UniqueName("cluster-pg")
		podName := e2eutil.UniqueName("cluster-pod")

		_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.Ctx.Namespace).Create(
			context.Background(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podGroupName,
					Namespace: fixture.Ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue:        queueName,
					MinMember:    1,
					MinResources: e2eutil.ResourceListPointer(e2eutil.CPUResource("10m")),
				},
			}, metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		pod := e2eutil.CreatePod(fixture.Ctx, e2eutil.PodSpec{
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
		Expect(e2eutil.WaitNamespaceQueuePodScheduled(fixture.Ctx, podGroupName, pod.Name)).NotTo(HaveOccurred())
		Expect(fixture.Ctx.Kubeclient.CoreV1().Pods(fixture.Ctx.Namespace).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitPodDeleted(fixture.Ctx, pod.Name)).NotTo(HaveOccurred())
		Expect(fixture.Ctx.Vcclient.SchedulingV1beta1().PodGroups(fixture.Ctx.Namespace).Delete(
			context.Background(), podGroupName, metav1.DeleteOptions{},
		)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitPodGroupDeleted(fixture.Ctx, podGroupName)).NotTo(HaveOccurred())
	})

	It("rejects invalid NamespaceQueue parent relationships", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
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
			name := e2eutil.UniqueName(tc.name)
			parent := tc.parent
			if tc.name == "self-parent" {
				parent = name
			}
			queue := e2eutil.NewNamespaceQueue(fixture.Ctx.Namespace, name, parent)
			_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.Ctx.Namespace).Create(
				context.Background(), queue, metav1.CreateOptions{},
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(tc.expectedError))
		}
	})

	It("rejects NamespaceQueue hierarchies beyond the configured depth", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parent := "cluster/" + queueName
		for level := 0; level < commonutil.DefaultMaxNamespaceQueueDepth; level++ {
			parent = fixture.CreateNamespaceQueue(parent)
		}

		overLimit := e2eutil.NewNamespaceQueue(
			fixture.Ctx.Namespace,
			e2eutil.UniqueName("nq-depth-limit"),
			parent,
		)
		_, err := fixture.Ctx.Vcclient.SchedulingV1beta1().NamespaceQueues(fixture.Ctx.Namespace).Create(
			context.Background(), overLimit, metav1.CreateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("depth"))
	})

	It("schedules a workload at the maximum NamespaceQueue depth", func() {
		fixture := e2eutil.NewNamespaceQueueFixture()
		queueName := fixture.CreateClusterQueue([]string{fixture.Ctx.Namespace})
		parent := "cluster/" + queueName
		for level := 0; level < commonutil.DefaultMaxNamespaceQueueDepth; level++ {
			parent = fixture.CreateNamespaceQueue(parent)
		}

		job := e2eutil.CreateJob(fixture.Ctx, &e2eutil.JobSpec{
			Name:      e2eutil.UniqueName("nq-max-depth-job"),
			Namespace: fixture.Ctx.Namespace,
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

		Expect(e2eutil.WaitNamespaceQueueJobReady(fixture.Ctx, job)).NotTo(HaveOccurred())
	})
})
