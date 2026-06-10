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
	"sort"
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
	VolcanoShardName   = "volcano"

	pollInterval      = 500 * time.Millisecond
	deploymentTimeout = 5 * time.Minute
	volcanoSystemNS   = "volcano-system"

	agentSchedulerNodeLabel  = "volcano.sh/e2e-agent-scheduler-sharding"
	agentSchedulerNodeValue  = "preferred"
	shardingConfigMapName    = "integration-sharding-configmap"
	shardingConfigMapDataKey = "sharding.yaml"
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

	waitForNodeShardsCreated := func() {
		By("Waiting for ShardingController to create NodeShards")
		err := e2eutil.WaitForNodeShardsCreated(ctx, []string{AgentSchedulerName, VolcanoShardName})
		Expect(err).NotTo(HaveOccurred(), "NodeShards should be created by ShardingController")
	}

	getAgentShardPartition := func() ([]string, []string) {
		waitForNodeShardsCreated()

		agentShard, err := e2eutil.GetNodeShard(ctx, AgentSchedulerName)
		Expect(err).NotTo(HaveOccurred())
		volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
		Expect(err).NotTo(HaveOccurred())

		agentNodes := filterWorkerNodeNames(ctx, agentShard.Spec.NodesDesired)
		if len(agentNodes) == 0 {
			agentNodes = append([]string(nil), agentShard.Spec.NodesDesired...)
		}
		agentNodeSet := stringSet(agentNodes)
		volcanoOnlyNodes := make([]string, 0, len(volcanoShard.Spec.NodesDesired))
		for _, n := range volcanoShard.Spec.NodesDesired {
			if _, ok := agentNodeSet[n]; !ok && !isControlPlaneNode(ctx, n) {
				volcanoOnlyNodes = append(volcanoOnlyNodes, n)
			}
		}

		GinkgoWriter.Printf("Agent shard nodes: %v\n", agentNodes)
		GinkgoWriter.Printf("Volcano-only shard nodes: %v\n", volcanoOnlyNodes)
		Expect(agentNodes).NotTo(BeEmpty(), "agent-scheduler shard must have nodes")
		Expect(volcanoOnlyNodes).NotTo(BeEmpty(),
			"volcano shard must have nodes outside the agent shard for sharding verification")
		return agentNodes, volcanoOnlyNodes
	}

	prepareSingleAgentWorkerShard := func() (string, []string, func()) {
		waitForNodeShardsCreated()
		workers := workerNodeNames(ctx)
		Expect(len(workers)).To(BeNumerically(">=", 2), "agent scheduler sharding tests need at least two worker nodes")
		agentNode := workers[0]
		volcanoOnlyNodes := append([]string(nil), workers[1:]...)

		cleanupWarmupLabel := addNodeLabel(ctx, agentNode, agentSchedulerNodeLabel, agentSchedulerNodeValue)
		originalConfig := updateShardingConfigMap(ctx, preferredAgentNodeShardingConfig())
		restored := false
		restore := func() {
			if restored {
				return
			}
			restoreShardingConfigMap(ctx, originalConfig)
			cleanupWarmupLabel()
			restored = true
		}

		err := waitForNodeShardSpec(ctx, AgentSchedulerName, []string{agentNode})
		Expect(err).NotTo(HaveOccurred(), "agent NodeShard spec should use the pinned worker shard")
		if !currentSpecHasLabel("none") {
			err = waitForNodeShardStatus(ctx, AgentSchedulerName, []string{agentNode})
			Expect(err).NotTo(HaveOccurred(), "agent scheduler should observe the pinned worker shard")
		}
		return agentNode, volcanoOnlyNodes, restore
	}

	Describe("Agent Scheduler Deployment", Label("hard", "soft", "none"), func() {
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

	Describe("Pod Scheduling", Label("hard", "soft", "none"), func() {
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

		It("should schedule multiple pods", func() {
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

				By(fmt.Sprintf("Waiting for pod %s to be scheduled", podName))
				err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, podName)
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

	Describe("Agent Scheduler Sharding Modes", func() {
		It("hard and soft modes should schedule pods onto agent shard nodes while shard capacity is available", Label("hard", "soft"), func() {
			agentNode, volcanoOnlyNodes, restore := prepareSingleAgentWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, AgentSchedulerName, []string{agentNode})
			Expect(err).NotTo(HaveOccurred(), "agent scheduler should observe the test worker shard")
			agentNodes := []string{agentNode}
			volcanoOnlySet := stringSet(volcanoOnlyNodes)

			By("Creating an agent-scheduler pod")
			pod := createAgentPod(ctx.Namespace, "agent-shard-pod", "100m")
			createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create agent pod")

			By("Waiting for the pod to be scheduled")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdPod.Name)
			Expect(err).NotTo(HaveOccurred(), "pod should be scheduled by agent-scheduler")

			By("Verifying pod is scheduled onto an agent shard node")
			scheduledPod := getPod(ctx, createdPod.Name)
			Expect(volcanoOnlySet).NotTo(HaveKey(scheduledPod.Spec.NodeName),
				"pod should not use volcano-only nodes while agent shard capacity is available")
			expectPodOnNodes(scheduledPod, stringSet(agentNodes))
		})

		It("soft mode should prefer the agent shard when shard and outside nodes are both feasible", Label("soft"), func() {
			agentNode, _, restore := prepareSingleAgentWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, AgentSchedulerName, []string{agentNode})
			Expect(err).NotTo(HaveOccurred(), "agent scheduler should observe the test worker shard")

			By("Creating an agent-scheduler pod that can fit on either shard or outside-shard nodes")
			pod := createAgentPod(ctx.Namespace, "agent-soft-prefer-pod", "100m")
			createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create agent pod")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdPod.Name)
			Expect(err).NotTo(HaveOccurred(), "pod should be scheduled")
			expectPodOnNodes(getPod(ctx, createdPod.Name), stringSet([]string{agentNode}))
		})

		It("soft mode should schedule outside the agent shard when in-shard resources are exhausted", Label("soft"), func() {
			agentNode, volcanoOnlyNodes, restore := prepareSingleAgentWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, AgentSchedulerName, []string{agentNode})
			Expect(err).NotTo(HaveOccurred(), "agent scheduler should observe the test worker shard")
			createBoundPlaceholderPod(ctx, "agent-soft-fill-shard-node", agentNode, allocatableCPU(ctx, agentNode))

			By("Creating an agent-scheduler pod that should fall back outside the agent shard")
			secondPod := createAgentPod(ctx.Namespace, "agent-soft-fallback-pod", "100m")
			createdSecondPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), secondPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create second agent pod")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdSecondPod.Name)
			Expect(err).NotTo(HaveOccurred(), "second pod should fall back outside the agent shard")
			expectPodOnNodes(getPod(ctx, createdSecondPod.Name), stringSet(volcanoOnlyNodes))
		})

		It("none mode should not force placement onto the agent shard", Label("none"), func() {
			agentNode, volcanoOnlyNodes, restore := prepareSingleAgentWorkerShard()
			defer restore()
			createBoundPlaceholderPod(ctx, "agent-none-make-shard-less-preferred", agentNode, fractionOfAllocatableCPU(ctx, agentNode, 2))

			By("Creating an agent-scheduler pod that can fit on the shard but has better outside-shard scores")
			pod := createAgentPod(ctx.Namespace, "agent-none-score-outside-pod", "100m")
			createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create agent pod")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdPod.Name)
			Expect(err).NotTo(HaveOccurred(), "pod should be scheduled")
			expectPodOnNodes(getPod(ctx, createdPod.Name), stringSet(volcanoOnlyNodes))
		})

		It("none mode should schedule outside the agent shard when in-shard resources are exhausted", Label("none"), func() {
			agentNode, volcanoOnlyNodes, restore := prepareSingleAgentWorkerShard()
			defer restore()
			createBoundPlaceholderPod(ctx, "agent-none-fill-shard-node", agentNode, allocatableCPU(ctx, agentNode))

			By("Creating an agent-scheduler pod that should use outside-shard capacity")
			pod := createAgentPod(ctx.Namespace, "agent-none-fallback-pod", "100m")
			createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create agent pod")
			err = e2eutil.WaitPodScheduled(ctx, ctx.Namespace, createdPod.Name)
			Expect(err).NotTo(HaveOccurred(), "pod should fall back outside the agent shard")
			expectPodOnNodes(getPod(ctx, createdPod.Name), stringSet(volcanoOnlyNodes))
		})

		It("hard mode should not fall back outside the agent shard when in-shard resources are exhausted", Label("hard"), func() {
			agentNode, _, restore := prepareSingleAgentWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, AgentSchedulerName, []string{agentNode})
			Expect(err).NotTo(HaveOccurred(), "agent scheduler should observe the test worker shard")
			createBoundPlaceholderPod(ctx, "agent-hard-fill-shard-node", agentNode, allocatableCPU(ctx, agentNode))

			By("Creating an agent-scheduler pod that should stay unschedulable in hard mode")
			secondPod := createAgentPod(ctx.Namespace, "agent-hard-pod", "100m")
			createdSecondPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), secondPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create second agent pod")
			err = e2eutil.WaitPodUnschedulable(ctx, ctx.Namespace, createdSecondPod.Name, 1*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "hard mode should not fall back outside the agent shard")
			Expect(getPod(ctx, createdSecondPod.Name).Spec.NodeName).To(BeEmpty())
		})
	})

	Describe("Unschedulable Pods", Label("hard", "soft", "none"), func() {
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
			agentNodes, _ := getAgentShardPartition()
			targetNode := agentNodes[0]

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

	Describe("Batch Scheduling", Label("soft", "none"), func() {
		It("should schedule a batch of pods", func() {
			podCount := 10

			By(fmt.Sprintf("Creating %d pods with schedulerName=agent-scheduler", podCount))
			batchPods := make(map[string]bool, podCount)
			for i := 0; i < podCount; i++ {
				podName := fmt.Sprintf("agent-batch-pod-%d", i)
				batchPods[podName] = true
				pod := createAgentPod(ctx.Namespace, podName, "30m")
				_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
					context.TODO(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", podName)
			}

			// Binds to the same node are serialized, so wait on the batch as a
			// whole rather than per pod. List once per poll instead of getting
			// each pod to keep API-server load low.
			By("Waiting for every pod in the batch to be scheduled")
			Eventually(func(g Gomega) {
				pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(
					context.TODO(), metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred(), "failed to list pods")

				scheduled := 0
				for _, pod := range pods.Items {
					if batchPods[pod.Name] && pod.Spec.NodeName != "" {
						scheduled++
					}
				}
				g.Expect(scheduled).To(Equal(podCount), "all batch pods should be scheduled")
			}, 3*time.Minute, 2*time.Second).Should(Succeed(), "all pods should be scheduled")
		})
	})

	Describe("NodeShard Verification", Label("hard", "soft", "none"), func() {
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

func stringSet(nodes []string) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		out[n] = struct{}{}
	}
	return out
}

func currentSpecHasLabel(label string) bool {
	for _, l := range CurrentSpecReport().Labels() {
		if l == label {
			return true
		}
	}
	return false
}

func workerNodeNames(ctx *e2eutil.TestContext) []string {
	nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to list nodes")

	workers := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			continue
		}
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			continue
		}
		workers = append(workers, node.Name)
	}
	sort.Strings(workers)
	return workers
}

func filterWorkerNodeNames(ctx *e2eutil.TestContext, nodeNames []string) []string {
	workerSet := stringSet(workerNodeNames(ctx))
	workers := make([]string, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		if _, ok := workerSet[nodeName]; ok {
			workers = append(workers, nodeName)
		}
	}
	return workers
}

func isControlPlaneNode(ctx *e2eutil.TestContext, nodeName string) bool {
	node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get node %s", nodeName)
	_, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]
	_, isMaster := node.Labels["node-role.kubernetes.io/master"]
	return isControlPlane || isMaster
}

func addNodeLabel(ctx *e2eutil.TestContext, nodeName, key, value string) func() {
	node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get node %s", nodeName)

	originalValue, hadOriginalValue := node.Labels[key]
	nodeCopy := node.DeepCopy()
	if nodeCopy.Labels == nil {
		nodeCopy.Labels = make(map[string]string)
	}
	nodeCopy.Labels[key] = value
	_, err = ctx.Kubeclient.CoreV1().Nodes().Update(context.TODO(), nodeCopy, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to label node %s", nodeName)

	return func() {
		node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			GinkgoWriter.Printf("warning: failed to get node %s for label cleanup: %v\n", nodeName, err)
			return
		}
		nodeCopy := node.DeepCopy()
		if hadOriginalValue {
			nodeCopy.Labels[key] = originalValue
		} else {
			delete(nodeCopy.Labels, key)
		}
		if _, err := ctx.Kubeclient.CoreV1().Nodes().Update(context.TODO(), nodeCopy, metav1.UpdateOptions{}); err != nil {
			GinkgoWriter.Printf("warning: failed to clean up label %s on node %s: %v\n", key, nodeName, err)
		}
	}
}

func updateShardingConfigMap(ctx *e2eutil.TestContext, data string) string {
	cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNS).Get(
		context.TODO(), shardingConfigMapName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get sharding ConfigMap")

	originalData := cm.Data[shardingConfigMapDataKey]
	cmCopy := cm.DeepCopy()
	if cmCopy.Data == nil {
		cmCopy.Data = make(map[string]string)
	}
	cmCopy.Data[shardingConfigMapDataKey] = data
	_, err = ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNS).Update(
		context.TODO(), cmCopy, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to update sharding ConfigMap")
	return originalData
}

func restoreShardingConfigMap(ctx *e2eutil.TestContext, data string) {
	cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNS).Get(
		context.TODO(), shardingConfigMapName, metav1.GetOptions{})
	if err != nil {
		GinkgoWriter.Printf("warning: failed to get sharding ConfigMap for restore: %v\n", err)
		return
	}

	cmCopy := cm.DeepCopy()
	if cmCopy.Data == nil {
		cmCopy.Data = make(map[string]string)
	}
	cmCopy.Data[shardingConfigMapDataKey] = data
	if _, err := ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNS).Update(
		context.TODO(), cmCopy, metav1.UpdateOptions{}); err != nil {
		GinkgoWriter.Printf("warning: failed to restore sharding ConfigMap: %v\n", err)
	}
}

func preferredAgentNodeShardingConfig() string {
	return fmt.Sprintf(`schedulerConfigs:
  - name: agent-scheduler
    type: agent
    policies:
      - name: warmup
        weight: 100
        arguments:
          warmupLabel: %s
          warmupLabelValue: %s
      - name: node-limit
        arguments:
          minNodes: 1
          maxNodes: 1
  - name: volcano
    type: volcano
    policies:
      - name: node-limit
        arguments:
          minNodes: 1
          maxNodes: 100
shardSyncPeriod: "1s"
enableNodeEventTrigger: true
`, agentSchedulerNodeLabel, agentSchedulerNodeValue)
}

func createBoundPlaceholderPod(ctx *e2eutil.TestContext, name, nodeName string, req corev1.ResourceList) {
	pod := e2eutil.CreatePod(ctx, e2eutil.PodSpec{
		Name:          name,
		Node:          nodeName,
		Req:           req,
		Tolerations:   controlPlaneTolerations(),
		RestartPolicy: corev1.RestartPolicyNever,
	})
	err := e2eutil.WaitPodReady(ctx, pod)
	Expect(err).NotTo(HaveOccurred(), "placeholder pod %s should run on %s", name, nodeName)
}

func allocatableCPU(ctx *e2eutil.TestContext, nodeName string) corev1.ResourceList {
	return availableCPU(ctx, nodeName, 50)
}

func fractionOfAllocatableCPU(ctx *e2eutil.TestContext, nodeName string, divisor int64) corev1.ResourceList {
	node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get node %s", nodeName)
	milliCPU := node.Status.Allocatable.Cpu().MilliValue()
	Expect(milliCPU).To(BeNumerically(">", 0), "node %s should report positive allocatable CPU", nodeName)
	Expect(divisor).To(BeNumerically(">", 0), "CPU divisor should be positive")
	requestMilliCPU := milliCPU / divisor
	if requestMilliCPU < 1 {
		requestMilliCPU = 1
	}
	return corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse(fmt.Sprintf("%dm", requestMilliCPU)),
	}
}

func availableCPU(ctx *e2eutil.TestContext, nodeName string, reserveMilliCPU int64) corev1.ResourceList {
	node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get node %s", nodeName)
	allocatableMilliCPU := node.Status.Allocatable.Cpu().MilliValue()

	pods, err := ctx.Kubeclient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	Expect(err).NotTo(HaveOccurred(), "failed to list pods on node %s", nodeName)

	var requestedMilliCPU int64
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, container := range pod.Spec.Containers {
			requestedMilliCPU += container.Resources.Requests.Cpu().MilliValue()
		}
	}

	requestMilliCPU := allocatableMilliCPU - requestedMilliCPU - reserveMilliCPU
	Expect(requestMilliCPU).To(BeNumerically(">", 0),
		"node %s should have allocatable CPU after existing requests and reserve", nodeName)
	return corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse(fmt.Sprintf("%dm", requestMilliCPU)),
	}
}

func waitForNodeShardSpec(ctx *e2eutil.TestContext, shardName string, expectedNodesDesired []string) error {
	expected := stringSet(expectedNodesDesired)
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, 1*time.Minute, true,
		func(c context.Context) (bool, error) {
			shard, err := e2eutil.GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			return sameStringSet(stringSet(shard.Spec.NodesDesired), expected), nil
		})
}

func waitForNodeShardStatus(ctx *e2eutil.TestContext, shardName string, expectedNodesInUse []string) error {
	expected := stringSet(expectedNodesInUse)
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, 1*time.Minute, true,
		func(c context.Context) (bool, error) {
			shard, err := e2eutil.GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			return sameStringSet(stringSet(shard.Status.NodesInUse), expected), nil
		})
}

func sameStringSet(actual, expected map[string]struct{}) bool {
	if len(actual) != len(expected) {
		return false
	}
	for n := range expected {
		if _, ok := actual[n]; !ok {
			return false
		}
	}
	return true
}

func getPod(ctx *e2eutil.TestContext, name string) *corev1.Pod {
	pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get pod %s", name)
	return pod
}

func expectPodOnNodes(pod *corev1.Pod, allowedNodes map[string]struct{}) {
	Expect(pod.Spec.NodeName).NotTo(BeEmpty(), "pod %s should be scheduled", pod.Name)
	Expect(allowedNodes).To(HaveKey(pod.Spec.NodeName),
		fmt.Sprintf("pod %s scheduled to node %q outside expected nodes %v",
			pod.Name, pod.Spec.NodeName, allowedNodes))
}

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
			Tolerations:   controlPlaneTolerations(),
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

func controlPlaneTolerations() []corev1.Toleration {
	return []corev1.Toleration{
		{
			Key:      "node-role.kubernetes.io/control-plane",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "node-role.kubernetes.io/master",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
}
