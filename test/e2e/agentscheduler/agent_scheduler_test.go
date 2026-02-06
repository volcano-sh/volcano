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

// TODO: Add tests for no-sharding mode scheduling
// TODO: Add tests for hard-sharding mode (strict shard boundaries)
// TODO: Add tests for soft-sharding mode (preferred in-shard, fallback out-of-shard)
// TODO: Add tests for shard reconfiguration handling
// TODO: Add tests for multi-worker scheduling across nodes

package agentscheduler

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// AgentSchedulerName is the scheduler name for agent workloads
	AgentSchedulerName  = "agent-scheduler"
	AgentSchedulerShard = "agent-scheduler"
	VolcanoShardName    = "volcano"

	// Polling intervals
	pollInterval = 500 * time.Millisecond

	// Wait time for sharding controller to create NodeShards after startup
	shardCreationTimeout = 3 * time.Minute
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

	// waitForNodeShardsCreated waits for NodeShards to be created by the ShardingController
	waitForNodeShardsCreated := func() {
		By("Waiting for ShardingController to create NodeShards")
		err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, shardCreationTimeout, true,
			func(c context.Context) (bool, error) {
				shards, err := e2eutil.ListNodeShards(ctx)
				if err != nil {
					GinkgoWriter.Printf("Error listing NodeShards: %v\n", err)
					return false, nil
				}
				// Check if both expected shards exist
				foundVolcano := false
				foundAgent := false
				for _, shard := range shards.Items {
					if shard.Name == VolcanoShardName {
						foundVolcano = true
					}
					if shard.Name == AgentSchedulerShard {
						foundAgent = true
					}
				}
				if foundVolcano && foundAgent {
					GinkgoWriter.Printf("Found NodeShards: volcano=%v, agent-scheduler=%v\n", foundVolcano, foundAgent)
					return true, nil
				}
				GinkgoWriter.Printf("Waiting for NodeShards... volcano=%v, agent-scheduler=%v, total=%d\n",
					foundVolcano, foundAgent, len(shards.Items))
				return false, nil
			})
		Expect(err).NotTo(HaveOccurred(), "NodeShards should be created by ShardingController")
	}

	Describe("Agent Scheduler Infrastructure", func() {
		It("NodeShard for agent-scheduler should exist", func() {
			waitForNodeShardsCreated()

			By("Getting the agent-scheduler NodeShard")
			agentShard, err := e2eutil.GetNodeShard(ctx, AgentSchedulerShard)
			Expect(err).NotTo(HaveOccurred(), "failed to get agent-scheduler NodeShard")

			GinkgoWriter.Printf("Agent-scheduler NodeShard exists with %d nodes desired\n",
				len(agentShard.Spec.NodesDesired))
			GinkgoWriter.Printf("NodesDesired: %v\n", agentShard.Spec.NodesDesired)
			GinkgoWriter.Printf("NodesInUse: %v\n", agentShard.Status.NodesInUse)
		})

		It("Agent pods can be created with agent-scheduler scheduler name", func() {
			By("Creating a pod with agent-scheduler")
			pod := createAgentPod(ctx.Namespace, "agent-pod-test", "100m")

			createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create agent pod")

			By("Verifying pod was created with correct scheduler name")
			Expect(createdPod.Spec.SchedulerName).To(Equal(AgentSchedulerName),
				"pod should have agent-scheduler as scheduler name")

			GinkgoWriter.Printf("Pod %s created with scheduler: %s\n",
				createdPod.Name, createdPod.Spec.SchedulerName)
		})

		It("Multiple agent pods can be created successfully", func() {
			By("Creating multiple agent pods")
			podCount := 3

			for i := 0; i < podCount; i++ {
				podName := "agent-pod-" + string(rune('a'+i))
				pod := createAgentPod(ctx.Namespace, podName, "50m")

				createdPod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
					context.TODO(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", podName)
				Expect(createdPod.Spec.SchedulerName).To(Equal(AgentSchedulerName))
			}

			By("Listing all pods in namespace")
			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(
				context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			agentPodCount := 0
			for _, pod := range pods.Items {
				if pod.Spec.SchedulerName == AgentSchedulerName {
					agentPodCount++
					GinkgoWriter.Printf("Found agent pod: %s\n", pod.Name)
				}
			}

			Expect(agentPodCount).To(Equal(podCount),
				"all agent pods should be created")
		})
	})

	Describe("Sharding Configuration", func() {
		It("Both volcano and agent-scheduler NodeShards should exist", func() {
			waitForNodeShardsCreated()

			By("Listing all NodeShards")
			shards, err := e2eutil.ListNodeShards(ctx)
			Expect(err).NotTo(HaveOccurred())

			shardNames := make([]string, 0, len(shards.Items))
			for _, shard := range shards.Items {
				shardNames = append(shardNames, shard.Name)
				GinkgoWriter.Printf("NodeShard: %s, NodesDesired: %d, NodesInUse: %d\n",
					shard.Name, len(shard.Spec.NodesDesired), len(shard.Status.NodesInUse))
			}

			Expect(shardNames).To(ContainElement(VolcanoShardName),
				"volcano NodeShard should exist")
			Expect(shardNames).To(ContainElement(AgentSchedulerShard),
				"agent-scheduler NodeShard should exist")
		})

		It("Cluster nodes should be visible to tests", func() {
			By("Getting cluster nodes")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			workerNodes := filterWorkerNodes(nodes.Items)
			GinkgoWriter.Printf("Total nodes: %d, Worker nodes: %d\n",
				len(nodes.Items), len(workerNodes))

			for _, node := range nodes.Items {
				GinkgoWriter.Printf("Node: %s, Schedulable: %v\n",
					node.Name, !node.Spec.Unschedulable)
			}

			Expect(len(nodes.Items)).To(BeNumerically(">=", 1),
				"cluster should have at least 1 node")
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

func filterWorkerNodes(nodes []corev1.Node) []corev1.Node {
	var workers []corev1.Node
	for _, node := range nodes {
		isControlPlane := false
		for key := range node.Labels {
			if key == "node-role.kubernetes.io/control-plane" ||
				key == "node-role.kubernetes.io/master" {
				isControlPlane = true
				break
			}
		}
		if !isControlPlane && node.Spec.Unschedulable == false {
			workers = append(workers, node)
		}
	}
	return workers
}
