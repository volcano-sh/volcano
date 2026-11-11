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

package schedulinggates

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Scheduling Gates E2E Test", func() {
	ginkgo.It("Unschedulable pod with removed gate reserves queue capacity and blocks other pods", func() {
		const queueName = "capacity-test-queue"
		const pgName = "test-podgroup"

		// Switch to capacity plugin
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		_ = cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				for _, tier := range sc.Tiers {
					for i, plugin := range tier.Plugins {
						if plugin.Name == "proportion" {
							tier.Plugins[i] = e2eutil.PluginOption{Name: "capacity"}
							return true
						}
					}
				}
				return false
			})
		})
		defer cmc.UndoChanged()

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(),
			&schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: queueName},
				Spec: schedulingv1beta1.QueueSpec{
					Weight:     1,
					Capability: slot,
				},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer e2eutil.DeleteQueue(ctx, queueName)

		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(),
			&schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: pgName, Namespace: ctx.Namespace},
				Spec: schedulingv1beta1.PodGroupSpec{
					MinMember:    1,
					Queue:        queueName,
					MinResources: &slot,
				},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		createPod := func(name string, nodeSelector map[string]string) *corev1.Pod {
			return e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name: name,
				Req:  slot,
				Annotations: map[string]string{
					"scheduling.k8s.io/group-name":           pgName,
					schedulingv1beta1.QueueAllocationGateKey: "true",
					schedulingv1beta1.QueueNameAnnotationKey: queueName,
				},
				SchedulerName: "volcano",
				RestartPolicy: corev1.RestartPolicyNever,
				NodeSelector:  nodeSelector,
			})
		}

		ginkgo.By("Pod-1 is created with the scheduling gate, and runs")
		pod1 := createPod("pod-1", nil)

		// The webhook must have injected the gate synchronously during Create
		gomega.Expect(e2eutil.PodHasSchedulingGates(pod1, schedulingv1beta1.QueueAllocationGateKey)).
			To(gomega.BeTrue(), "webhook must inject the QueueAllocationGate")

		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-1", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())

		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-1", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod-2 unschedulable, reserves capacity and stays pending")
		createPod("pod-2", map[string]string{"kubernetes.io/fake-node": "fake"})
		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-2", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())
		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
			[]corev1.PodPhase{corev1.PodPending})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod-2 deleted, Pod-3 schedules and runs")
		createPod("pod-3", nil)
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-2", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-3", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())
		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Clean up remaining pods so DeleteQueue can close the queue
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-3", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Multiple scheduling gates: scheduler skips until only Volcano gate remains, then removes it", func() {
		const (
			queueName     = "capacity-multigate-queue"
			pgName        = "test-pg-multigate"
			otherGateName = "example.com/other-gate"
			podName       = "pod-multigate"
		)

		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		_ = cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				for _, tier := range sc.Tiers {
					for i, plugin := range tier.Plugins {
						if plugin.Name == "proportion" {
							tier.Plugins[i] = e2eutil.PluginOption{Name: "capacity"}
							return true
						}
					}
				}
				return false
			})
		})
		defer cmc.UndoChanged()

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(),
			&schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: queueName},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Capability: slot},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer e2eutil.DeleteQueue(ctx, queueName)

		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			q, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return q.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(),
			&schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: pgName, Namespace: ctx.Namespace},
				Spec:       schedulingv1beta1.PodGroupSpec{MinMember: 1, Queue: queueName, MinResources: &slot},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod with two gates, both gates remain")
		_ = e2eutil.CreatePod(ctx, e2eutil.PodSpec{
			Name: podName,
			Req:  slot,
			Annotations: map[string]string{
				"scheduling.k8s.io/group-name":           pgName,
				schedulingv1beta1.QueueAllocationGateKey: "true",
				schedulingv1beta1.QueueNameAnnotationKey: queueName,
			},
			SchedulerName:   "volcano",
			SchedulingGates: []corev1.PodSchedulingGate{{Name: otherGateName}},
		})

		gomega.Consistently(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil || pod.Status.Phase != corev1.PodPending {
				return false
			}
			return e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey, otherGateName)
		}, 10*time.Second, 1*time.Second).Should(gomega.BeTrue(), "both gates remain and pod stays Pending")

		ginkgo.By("Secondary gate removed, only volcano gate remains")
		patchOnlyVolcano := []byte(`[{"op":"replace","path":"/spec/schedulingGates","value":[{"name":"` + schedulingv1beta1.QueueAllocationGateKey + `"}]}]`)
		_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Patch(context.TODO(), podName, types.JSONPatchType, patchOnlyVolcano, metav1.PatchOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod schedules and runs")

		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			return err == nil && e2eutil.PodHasSchedulingGates(pod)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())

		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ctx.Namespace}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Clean up the pod so DeleteQueue can close the queue
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Hierarchical queues: sibling reservations are counted at the shared parent", func() {
		const (
			rootQueue = "root"
			parentQ   = "gates-hier-parent"
			leafQ1    = "gates-hier-l1"
			leafQ2    = "gates-hier-l2"
			pgName1   = "test-pg-hier-l1"
			pgName2   = "test-pg-hier-l2"
		)

		// Hierarchical capacity plugin.
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		_ = cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				trueValue := true
				for _, tier := range sc.Tiers {
					for i, plugin := range tier.Plugins {
						if plugin.Name == "proportion" {
							tier.Plugins[i] = e2eutil.PluginOption{
								Name:             "capacity",
								EnabledHierarchy: &trueValue,
							}
							return true
						}
					}
				}
				return false
			})
		})
		defer cmc.UndoChanged()

		// The parent caps the subtree at cpu 1000m. Each leaf is sized to the full parent
		// capability, which is the normal hierarchical setup. Only the parent can reject
		// a subtree that collectively exceeds it.
		parentCap := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi"),
		}
		bigSlot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("800m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}
		smallSlot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("400m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{parentQ, leafQ1, leafQ2},
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
			CapabilityResource: map[string]corev1.ResourceList{
				parentQ: parentCap,
				leafQ1:  parentCap,
				leafQ2:  parentCap,
			},
			QueueParent: map[string]string{
				parentQ: rootQueue,
				leafQ1:  parentQ,
				leafQ2:  parentQ,
			},
		})
		// Leaves must be torn down before the parent.
		ctx.Queues = []string{leafQ1, leafQ2, parentQ}
		defer e2eutil.CleanupTestContext(ctx)

		createPG := func(name, queue string) {
			_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(),
				&schedulingv1beta1.PodGroup{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ctx.Namespace},
					Spec: schedulingv1beta1.PodGroupSpec{
						MinMember: 1,
						Queue:     queue,
					},
				}, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
		createPG(pgName1, leafQ1)
		createPG(pgName2, leafQ2)

		createGatedPod := func(name, pgName, queue string, req corev1.ResourceList, nodeSelector map[string]string) *corev1.Pod {
			return e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name: name,
				Req:  req,
				Annotations: map[string]string{
					"scheduling.k8s.io/group-name":           pgName,
					schedulingv1beta1.QueueAllocationGateKey: "true",
					schedulingv1beta1.QueueNameAnnotationKey: queue,
				},
				SchedulerName: "volcano",
				RestartPolicy: corev1.RestartPolicyNever,
				NodeSelector:  nodeSelector,
			})
		}

		ginkgo.By("Pod in leaf-1 (cpu 800m) is ungated and reserves against the parent")
		// Pinned to a node that does not exist, so it stays Pending and holds the
		// reservation instead of allocating and converting it into allocated.
		pod1 := createGatedPod("pod-hier-1", pgName1, leafQ1, bigSlot,
			map[string]string{"kubernetes.io/fake-node": "fake"})
		gomega.Expect(e2eutil.PodHasSchedulingGates(pod1, schedulingv1beta1.QueueAllocationGateKey)).
			To(gomega.BeTrue(), "webhook must inject the QueueAllocationGate")

		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-hier-1", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue(),
			"leaf-1 has room under both its own and the parent's capability, so the gate is removed")

		ginkgo.By("Pod in sibling leaf-2 (cpu 400m) keeps its gate: 800m + 400m exceeds the parent's 1000m")
		// leaf-2's own capability (1000m) would admit this on its own. Only the parent,
		// counting leaf-1's reservation in its subtree, rejects it. Before subtree
		// aggregation the parent saw no reservations and the gate was wrongly removed.
		pod2 := createGatedPod("pod-hier-2", pgName2, leafQ2, smallSlot, nil)
		gomega.Expect(e2eutil.PodHasSchedulingGates(pod2, schedulingv1beta1.QueueAllocationGateKey)).
			To(gomega.BeTrue(), "webhook must inject the QueueAllocationGate")

		gomega.Consistently(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-hier-2", metav1.GetOptions{})
			if err != nil || pod.Status.Phase != corev1.PodPending {
				return false
			}
			return e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, 30*time.Second, 1*time.Second).Should(gomega.BeTrue(),
			"the sibling's reservation must be visible at the parent, so the gate must not be removed")

		ginkgo.By("Leaf-1 reservation released, leaf-2 is admitted and its gate removed")
		err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-hier-1", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-hier-2", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue(),
			"with the parent's budget free again the gate is removed")

		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-hier-2", Namespace: ctx.Namespace}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Clean up so the queues can be closed.
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-hier-2", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
