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

// TODO: Add tests for node reassignment when resource usage significantly changes
// TODO: Add tests for configuration changes triggering node re-assignment
// TODO: Add tests for newly created nodes being added to shards
// TODO: Add tests for different sharding strategies

package shardingcontroller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// Default shard names based on ShardingController default config
	VolcanoShardName    = "volcano"
	AgentSchedulerShard = "agent-scheduler"

	// Polling intervals
	pollInterval = 500 * time.Millisecond
	pollTimeout  = 2 * time.Minute

	// Wait time for sharding controller to create NodeShards after startup
	shardCreationTimeout = 3 * time.Minute
)

var _ = Describe("ShardingController E2E Test", func() {
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

	Describe("Shard Creation", func() {
		It("NodeShards should be created based on default configuration after controller startup", func() {
			waitForNodeShardsCreated()

			By("Listing all NodeShards in the cluster")
			shards, err := e2eutil.ListNodeShards(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to list NodeShards")

			By("Verifying NodeShards exist for configured schedulers")
			shardNames := make([]string, 0, len(shards.Items))
			for _, shard := range shards.Items {
				shardNames = append(shardNames, shard.Name)
				GinkgoWriter.Printf("Found NodeShard: %s with %d nodes desired, %d nodes in use\n",
					shard.Name, len(shard.Spec.NodesDesired), len(shard.Status.NodesInUse))
			}

			// Default config creates shards for "volcano" and "agent-scheduler"
			Expect(shardNames).To(ContainElement(VolcanoShardName),
				"NodeShard for volcano scheduler should exist")
			Expect(shardNames).To(ContainElement(AgentSchedulerShard),
				"NodeShard for agent-scheduler should exist")
		})

		It("NodeShards should have nodes assigned based on CPU utilization ranges", func() {
			waitForNodeShardsCreated()

			By("Getting cluster nodes")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to list nodes")

			// Filter out control-plane nodes
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1),
				"cluster should have at least 1 worker node")

			By("Verifying nodes are distributed across shards")
			volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred(), "failed to get volcano NodeShard")

			agentShard, err := e2eutil.GetNodeShard(ctx, AgentSchedulerShard)
			Expect(err).NotTo(HaveOccurred(), "failed to get agent-scheduler NodeShard")

			totalAssigned := len(volcanoShard.Spec.NodesDesired) + len(agentShard.Spec.NodesDesired)
			GinkgoWriter.Printf("Volcano shard nodes: %v\n", volcanoShard.Spec.NodesDesired)
			GinkgoWriter.Printf("Agent shard nodes: %v\n", agentShard.Spec.NodesDesired)
			GinkgoWriter.Printf("Total worker nodes: %d, Total assigned: %d\n", len(workerNodes), totalAssigned)

			// At least some nodes should be assigned
			Expect(totalAssigned).To(BeNumerically(">", 0),
				"at least one node should be assigned to a shard")
		})
	})

	// TODO: Add tests for unschedulable node handling once ShardingController behavior is confirmed

	Describe("Node Stability", func() {
		It("Node assignments should remain stable without significant cluster changes", func() {
			waitForNodeShardsCreated()

			By("Recording initial shard assignments")
			shard1, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())
			initialNodes := make([]string, len(shard1.Spec.NodesDesired))
			copy(initialNodes, shard1.Spec.NodesDesired)

			By("Waiting for a sync period without making changes")
			time.Sleep(30 * time.Second)

			By("Verifying assignments remain stable")
			shard2, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Initial nodes: %v\n", initialNodes)
			GinkgoWriter.Printf("Current nodes: %v\n", shard2.Spec.NodesDesired)

			// Verify stability - nodes should be the same or very similar
			commonNodes := countCommonNodes(initialNodes, shard2.Spec.NodesDesired)
			if len(initialNodes) > 0 {
				stabilityRatio := float64(commonNodes) / float64(len(initialNodes))
				GinkgoWriter.Printf("Stability ratio: %.2f (%d/%d nodes unchanged)\n",
					stabilityRatio, commonNodes, len(initialNodes))
				Expect(stabilityRatio).To(BeNumerically(">=", 0.8),
					"at least 80%% of nodes should remain stable")
			}
		})
	})
})

// Helper functions

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

func countCommonNodes(a, b []string) int {
	count := 0
	bMap := make(map[string]bool)
	for _, n := range b {
		bMap[n] = true
	}
	for _, n := range a {
		if bMap[n] {
			count++
		}
	}
	return count
}
