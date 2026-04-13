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

package agentscheduler

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	AgentSchedulerName = "agent-scheduler"

	pollInterval      = 500 * time.Millisecond
	deploymentTimeout = 5 * time.Minute
	volcanoSystemNS   = "volcano-system"
)

var _ = Describe("Agent Scheduler E2E Test", func() {
	var ctx *e2eutil.TestContext

	BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			e2eutil.DumpTestContext(ctx)
		}
	})

	Describe("Agent Scheduler Deployment", func() {
		It("agent-scheduler deployment should be ready", func() {
			By("Checking agent-scheduler deployment in volcano-system namespace")
			var deploy *appsv1.Deployment
			err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, deploymentTimeout, true,
				func(c context.Context) (bool, error) {
					deployments, err := ctx.Kubeclient.AppsV1().Deployments(volcanoSystemNS).List(
						c, metav1.ListOptions{LabelSelector: "app=agent-scheduler"})
					if err != nil {
						GinkgoWriter.Printf("Error listing deployments: %v\n", err)
						return false, nil
					}
					if len(deployments.Items) == 0 {
						GinkgoWriter.Printf("No agent-scheduler deployment found yet\n")
						return false, nil
					}
					deploy = &deployments.Items[0]
					if deploy.Status.AvailableReplicas >= 1 {
						return true, nil
					}
					GinkgoWriter.Printf("Waiting for agent-scheduler deployment: available=%d, desired=%d\n",
						deploy.Status.AvailableReplicas, *deploy.Spec.Replicas)
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(), "agent-scheduler deployment should become ready")
			Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1))
			GinkgoWriter.Printf("Agent-scheduler deployment is ready with %d available replicas\n",
				deploy.Status.AvailableReplicas)
		})
	})

	Describe("Pod Scheduling", func() {
		It("should schedule a single pod with agent-scheduler", func() {
			By("Creating a pod with schedulerName=agent-scheduler")
			pod := createAgentPod(ctx.Namespace, "agent-single-pod", "100m")
			createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create agent pod")
			Expect(createdPod.Spec.SchedulerName).To(Equal(AgentSchedulerName))

			By("Waiting for the pod to be scheduled")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdPod.Name)
			Expect(err).NotTo(HaveOccurred(), "pod should be scheduled by agent-scheduler")

			By("Verifying pod is bound to a node")
			scheduledPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				context.TODO(), createdPod.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(scheduledPod.Spec.NodeName).NotTo(BeEmpty(),
				"pod should be bound to a node")
			GinkgoWriter.Printf("Pod %s scheduled on node %s\n",
				scheduledPod.Name, scheduledPod.Spec.NodeName)

			By("Waiting for the pod to reach Running phase")
			err = e2eutil.WaitPodReady(ctx, scheduledPod)
			Expect(err).NotTo(HaveOccurred(), "pod should reach Running phase")
		})

		It("should schedule multiple pods concurrently", func() {
			podCount := 5

			By(fmt.Sprintf("Creating %d pods with schedulerName=agent-scheduler", podCount))
			podNames := make([]string, podCount)
			for i := 0; i < podCount; i++ {
				podName := fmt.Sprintf("agent-multi-pod-%d", i)
				podNames[i] = podName
				pod := createAgentPod(ctx.Namespace, podName, "50m")
				_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
					context.TODO(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", podName)
			}

			By("Waiting for all pods to be scheduled and running")
			for _, podName := range podNames {
				err := e2eutil.WaitPodScheduled(ctx, ctx.Namespace, podName)
				Expect(err).NotTo(HaveOccurred(), "pod %s should be scheduled", podName)
			}

			By("Verifying all pods are bound to nodes")
			for _, podName := range podNames {
				pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
					context.TODO(), podName, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Spec.NodeName).NotTo(BeEmpty(),
					"pod %s should be bound to a node", podName)
				GinkgoWriter.Printf("Pod %s scheduled on node %s\n", podName, pod.Spec.NodeName)
			}
		})

		It("should not interfere with default volcano scheduler pods", func() {
			By("Creating a pod with default volcano scheduler")
			createdVolcanoPod := e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name:          "volcano-default-pod",
				SchedulerName: e2eutil.SchedulerName,
				Req: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("50m"),
				},
			})

			By("Creating a pod with agent-scheduler")
			agentPod := createAgentPod(ctx.Namespace, "agent-coexist-pod", "50m")
			createdAgentPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), agentPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for both pods to be scheduled")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdAgentPod.Name)
			Expect(err).NotTo(HaveOccurred(), "agent pod should be scheduled")

			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdVolcanoPod.Name)
			Expect(err).NotTo(HaveOccurred(), "volcano pod should be scheduled")

			By("Verifying scheduler names are preserved")
			agentResult, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				context.TODO(), createdAgentPod.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(agentResult.Spec.SchedulerName).To(Equal(AgentSchedulerName))
			Expect(agentResult.Spec.NodeName).NotTo(BeEmpty())

			volcanoResult, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				context.TODO(), createdVolcanoPod.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(volcanoResult.Spec.SchedulerName).To(Equal(e2eutil.SchedulerName))
			Expect(volcanoResult.Spec.NodeName).NotTo(BeEmpty())

			GinkgoWriter.Printf("Agent pod on node: %s, Volcano pod on node: %s\n",
				agentResult.Spec.NodeName, volcanoResult.Spec.NodeName)
		})
	})

	Describe("Unschedulable Pods", func() {
		It("should leave a pod Unschedulable when no node has enough CPU", func() {
			By("Creating a pod requesting more CPU than any node has")
			pod := createAgentPod(ctx.Namespace, "agent-insufficient-cpu", "1000")
			_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod stays PodScheduled=False with reason Unschedulable")
			err = e2eutil.WaitPodUnschedulable(ctx, ctx.Namespace, pod.Name, 1*time.Minute)
			Expect(err).NotTo(HaveOccurred(),
				"pod should be marked Unschedulable by agent-scheduler")

			result, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				context.TODO(), pod.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Spec.NodeName).To(BeEmpty(), "pod should not be bound to any node")
		})

		It("should leave a pod Unschedulable when nodeSelector matches no node", func() {
			By("Creating a pod with an unsatisfiable nodeSelector")
			pod := createAgentPod(ctx.Namespace, "agent-bad-selector", "50m")
			pod.Spec.NodeSelector = map[string]string{
				"volcano.sh/agent-e2e-nonexistent": "true",
			}
			_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod stays PodScheduled=False with reason Unschedulable")
			err = e2eutil.WaitPodUnschedulable(ctx, ctx.Namespace, pod.Name, 1*time.Minute)
			Expect(err).NotTo(HaveOccurred(),
				"pod should be marked Unschedulable when no node matches its selector")
		})

		It("should requeue an Unschedulable pod after a node becomes a match", func() {
			labelKey := "volcano.sh/agent-e2e-requeue"
			labelValue := "true"

			By("Creating a pod with a nodeSelector that matches no node")
			pod := createAgentPod(ctx.Namespace, "agent-requeue-pod", "50m")
			pod.Spec.NodeSelector = map[string]string{labelKey: labelValue}
			_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod is Unschedulable")
			err = e2eutil.WaitPodUnschedulable(ctx, ctx.Namespace, pod.Name, 1*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "pod should initially be Unschedulable")

			By("Labeling a worker node to satisfy the nodeSelector")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(
				context.TODO(), metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/control-plane!="})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes.Items).NotTo(BeEmpty(), "expected at least one worker node")
			targetNode := nodes.Items[0].Name

			patch := fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, labelKey, labelValue)
			_, err = ctx.Kubeclient.CoreV1().Nodes().Patch(
				context.TODO(), targetNode, types.StrategicMergePatchType,
				[]byte(patch), metav1.PatchOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to label node %s", targetNode)

			defer func() {
				removePatch := fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, labelKey)
				_, cleanupErr := ctx.Kubeclient.CoreV1().Nodes().Patch(
					context.TODO(), targetNode, types.StrategicMergePatchType,
					[]byte(removePatch), metav1.PatchOptions{})
				if cleanupErr != nil {
					GinkgoWriter.Printf("warning: failed to remove label from node %s: %v\n",
						targetNode, cleanupErr)
				}
			}()

			By("Verifying pod gets scheduled onto the newly-matching node")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, pod.Name)
			Expect(err).NotTo(HaveOccurred(),
				"pod should be requeued and scheduled after node label update")

			scheduled, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(
				context.TODO(), pod.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(scheduled.Spec.NodeName).To(Equal(targetNode),
				"pod should be bound to the node that was labeled")
		})
	})

	Describe("Batch Scheduling", func() {
		It("should schedule a batch of pods", func() {
			podCount := 10

			By(fmt.Sprintf("Creating %d pods to test scheduling throughput", podCount))
			podNames := make([]string, podCount)
			for i := 0; i < podCount; i++ {
				podName := fmt.Sprintf("agent-batch-pod-%d", i)
				podNames[i] = podName
				pod := createAgentPod(ctx.Namespace, podName, "30m")
				_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
					context.TODO(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", podName)
			}

			By("Waiting for all pods to be scheduled within timeout")
			scheduledCount := 0
			for _, podName := range podNames {
				err := e2eutil.WaitPodScheduled(ctx, ctx.Namespace, podName)
				if err == nil {
					scheduledCount++
				}
			}

			GinkgoWriter.Printf("Scheduled %d/%d pods\n", scheduledCount, podCount)
			Expect(scheduledCount).To(Equal(podCount),
				"all pods should be scheduled")
		})
	})

	Describe("NodeShard Verification", func() {
		It("NodeShards should exist when sharding controller is enabled", func() {
			By("Waiting for NodeShards to be created")
			err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, 3*time.Minute, true,
				func(c context.Context) (bool, error) {
					shards, err := e2eutil.ListNodeShards(ctx)
					if err != nil {
						GinkgoWriter.Printf("Error listing NodeShards: %v\n", err)
						return false, nil
					}
					if len(shards.Items) > 0 {
						return true, nil
					}
					GinkgoWriter.Printf("No NodeShards found yet, waiting...\n")
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(), "NodeShards should be created by ShardingController")

			By("Listing all NodeShards")
			shards, err := e2eutil.ListNodeShards(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(shards.Items)).To(BeNumerically(">", 0),
				"at least one NodeShard should exist")

			for _, shard := range shards.Items {
				GinkgoWriter.Printf("NodeShard: %s, NodesDesired: %d, NodesInUse: %d\n",
					shard.Name, len(shard.Spec.NodesDesired), len(shard.Status.NodesInUse))
			}
		})
	})
})

// Helper functions

func createAgentPod(namespace, name, cpuRequest string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":  "agent-test",
				"test": "e2e",
			},
		},
		Spec: corev1.PodSpec{
			SchedulerName: AgentSchedulerName,
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   e2eutil.DefaultBusyBoxImage,
					Command: []string{"sleep", "3600"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse(cpuRequest),
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}
