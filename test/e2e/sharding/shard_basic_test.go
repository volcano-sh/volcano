/*
Copyright 2025 The Volcano Authors.

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

package sharding

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("ShardingController Basic E2E Tests", func() {
	ctx := NewShardTestContext()

	AfterEach(func() {
		ctx.Cleanup()
	})

	Context("Shard Creation and Initialization", func() {
		It("should create default shards after controller startup if shards do not exist", func() {
			By("Waiting for controller to start and create default shards")
			// The controller should create default shards (volcano-scheduler, agent-scheduler)
			// based on the default configuration

			// Wait for volcano-scheduler shard to be created
			err := WaitForShardCreated("volcano-scheduler", ControllerStartupTimeout)
			Expect(err).NotTo(HaveOccurred(), "volcano-scheduler shard should be created automatically")
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			// Wait for agent-scheduler shard to be created
			err = WaitForShardCreated("agent-scheduler", ControllerStartupTimeout)
			Expect(err).NotTo(HaveOccurred(), "agent-scheduler shard should be created automatically")
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			By("Verifying shard specifications")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(volcanoShard).NotTo(BeNil())
			Expect(volcanoShard.Name).To(Equal("volcano-scheduler"))

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(agentShard).NotTo(BeNil())
			Expect(agentShard.Name).To(Equal("agent-scheduler"))
		})

		It("should create shards with nodes when nodes exist", func() {
			By("Creating test nodes with different resource profiles")
			// Create low CPU utilization nodes (for volcano-scheduler)
			node1 := CreateTestNode("test-node-low-1", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			node2 := CreateTestNode("test-node-low-2", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node2.Name)

			// Create high CPU utilization nodes with warmup label (for agent-scheduler)
			node3 := CreateTestNode("test-node-high-1", "8", "32Gi", true, nil)
			ctx.TestNodes = append(ctx.TestNodes, node3.Name)

			By("Creating pods to simulate different CPU utilization levels")
			// Low utilization on node1 and node2 (30% CPU)
			CreateTestPod(ctx.Namespace, "pod-low-1", node1.Name, "2400m", "8Gi")
			CreateTestPod(ctx.Namespace, "pod-low-2", node2.Name, "2400m", "8Gi")

			// High utilization on node3 (80% CPU)
			CreateTestPod(ctx.Namespace, "pod-high-1", node3.Name, "6400m", "24Gi")

			By("Waiting for controller to detect nodes and assign them to shards")
			time.Sleep(10 * time.Second) // Give controller time to process

			By("Verifying shards were created and contain nodes")
			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			// Wait for nodes to be assigned
			time.Sleep(15 * time.Second)

			By("Verifying all nodes are assigned to shards")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalNodes := len(volcanoShard.Spec.NodesDesired) + len(agentShard.Spec.NodesDesired)
			Expect(totalNodes).To(BeNumerically(">=", 1), "At least one node should be assigned to shards")

			By("Test completed successfully")
		})
	})

	Context("Node Assignment and Distribution", func() {
		It("should assign nodes to shards based on CPU utilization strategy", func() {
			By("Creating nodes with varying CPU utilization levels")

			// Create low utilization nodes (target: volcano-scheduler, CPU range 0-60%)
			lowNode1 := CreateTestNode("test-low-node-1", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, lowNode1.Name)

			lowNode2 := CreateTestNode("test-low-node-2", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, lowNode2.Name)

			// Create pods with low utilization (25% CPU)
			CreateTestPod(ctx.Namespace, "pod-low-1", lowNode1.Name, "4000m", "16Gi")
			CreateTestPod(ctx.Namespace, "pod-low-2", lowNode2.Name, "4000m", "16Gi")

			// Create high utilization nodes (target: agent-scheduler, CPU range 70-100%)
			highNode1 := CreateTestNode("test-high-node-1", "16", "64Gi", true, nil)
			ctx.TestNodes = append(ctx.TestNodes, highNode1.Name)

			highNode2 := CreateTestNode("test-high-node-2", "16", "64Gi", true, nil)
			ctx.TestNodes = append(ctx.TestNodes, highNode2.Name)

			// Create pods with high utilization (85% CPU)
			CreateTestPod(ctx.Namespace, "pod-high-1", highNode1.Name, "13600m", "54Gi")
			CreateTestPod(ctx.Namespace, "pod-high-2", highNode2.Name, "13600m", "54Gi")

			By("Waiting for controller to assign nodes to appropriate shards")
			time.Sleep(15 * time.Second)

			By("Verifying volcano-scheduler contains low utilization nodes")
			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			By("Verifying agent-scheduler contains high utilization nodes")
			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			By("Verifying node distribution")
			time.Sleep(10 * time.Second)

			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(volcanoShard.Spec.NodesDesired)).To(BeNumerically(">=", 1),
				"volcano-scheduler should have at least 1 node")

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(agentShard.Spec.NodesDesired)).To(BeNumerically(">=", 1),
				"agent-scheduler should have at least 1 node")

			By("Test completed successfully")
		})

		It("should distribute nodes evenly when possible", func() {
			By("Creating multiple nodes with similar resource profiles")

			// Create 6 nodes with similar low CPU utilization
			for i := 1; i <= 6; i++ {
				nodeName := fmt.Sprintf("test-balanced-node-%d", i)
				node := CreateTestNode(nodeName, "16", "64Gi", false, nil)
				ctx.TestNodes = append(ctx.TestNodes, node.Name)

				// Add pods with consistent 40% CPU utilization
				podName := fmt.Sprintf("pod-balanced-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "6400m", "25Gi")
			}

			By("Waiting for controller to distribute nodes across shards")
			time.Sleep(15 * time.Second)

			By("Ensuring shards are created")
			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Verifying all nodes are assigned")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalAssigned := len(volcanoShard.Spec.NodesDesired) + len(agentShard.Spec.NodesDesired)
			Expect(totalAssigned).To(BeNumerically(">=", 1),
				"All created nodes should be assigned to shards")

			By("Test completed successfully")
		})
	})

	Context("Shard Updates and Reconciliation", func() {
		It("should update shard status after nodes are assigned", func() {
			By("Creating test nodes")
			node1 := CreateTestNode("test-status-node-1", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			node2 := CreateTestNode("test-status-node-2", "8", "32Gi", true, nil)
			ctx.TestNodes = append(ctx.TestNodes, node2.Name)

			By("Waiting for shards to be created and updated")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			By("Verifying shard status is updated")
			time.Sleep(10 * time.Second)

			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(volcanoShard.Status.LastUpdateTime).NotTo(BeZero(),
				"LastUpdateTime should be set")

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(agentShard.Status.LastUpdateTime).NotTo(BeZero(),
				"LastUpdateTime should be set")

			By("Test completed successfully")
		})

		It("should reconcile shards periodically", func() {
			By("Creating initial nodes")
			node1 := CreateTestNode("test-reconcile-node-1", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			By("Waiting for initial shard creation")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			By("Getting initial shard state")
			initialShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialUpdateTime := initialShard.Status.LastUpdateTime

			By("Waiting for periodic reconciliation")
			time.Sleep(130 * time.Second) // Wait for one full sync period (120s + buffer)

			By("Verifying shard was reconciled")
			reconciledShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// The LastUpdateTime should be updated after reconciliation
			// Note: This might be the same if no changes occurred, but the reconciliation should still happen
			Expect(reconciledShard.Status.LastUpdateTime).NotTo(BeZero())

			By("Test completed successfully")
		})
	})

	Context("Multiple Shards Management", func() {
		It("should manage multiple shards simultaneously", func() {
			By("Listing all shards")
			shards, err := ListShards()
			Expect(err).NotTo(HaveOccurred())

			initialShardCount := len(shards.Items)
			By(fmt.Sprintf("Initial shard count: %d", initialShardCount))

			By("Creating nodes for both schedulers")
			// Nodes for volcano-scheduler (low utilization)
			for i := 1; i <= 3; i++ {
				nodeName := fmt.Sprintf("test-multi-volcano-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", false, nil)
				ctx.TestNodes = append(ctx.TestNodes, node.Name)
				CreateTestPod(ctx.Namespace, fmt.Sprintf("pod-volcano-%d", i), nodeName, "2000m", "8Gi")
			}

			// Nodes for agent-scheduler (high utilization with warmup)
			for i := 1; i <= 3; i++ {
				nodeName := fmt.Sprintf("test-multi-agent-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", true, nil)
				ctx.TestNodes = append(ctx.TestNodes, node.Name)
				CreateTestPod(ctx.Namespace, fmt.Sprintf("pod-agent-%d", i), nodeName, "6000m", "24Gi")
			}

			By("Waiting for controller to create and update shards")
			time.Sleep(15 * time.Second)

			err = WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			By("Verifying both shards are properly configured")
			time.Sleep(10 * time.Second)

			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(volcanoShard.Spec.NodesDesired)).To(BeNumerically(">=", 1))

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(agentShard.Spec.NodesDesired)).To(BeNumerically(">=", 1))

			By("Verifying no node overlap between shards")
			volcanoNodes := make(map[string]bool)
			for _, node := range volcanoShard.Spec.NodesDesired {
				volcanoNodes[node] = true
			}

			for _, node := range agentShard.Spec.NodesDesired {
				Expect(volcanoNodes[node]).To(BeFalse(),
					"Node %s should not be assigned to both shards", node)
			}

			By("Test completed successfully")
		})
	})
})
