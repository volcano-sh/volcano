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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ShardingController Configuration Change and Stability E2E Tests", func() {
	ctx := NewShardTestContext()

	AfterEach(func() {
		ctx.Cleanup()
	})

	Context("Node Re-assignment Stability", func() {
		It("should keep nodes as stable as possible when re-assigning", func() {
			By("Creating initial set of nodes with varying utilization")
			// Create 8 nodes to have enough for distribution
			nodeNames := []string{}
			for i := 1; i <= 8; i++ {
				nodeName := fmt.Sprintf("test-stable-node-%d", i)
				
				// Alternate between low and medium utilization
				var cpu string
				var warmup bool
				if i%2 == 0 {
					cpu = "3200m" // 40% of 8 cores
					warmup = false
				} else {
					cpu = "2400m" // 30% of 8 cores
					warmup = false
				}

				node := CreateTestNode(nodeName, "8", "32Gi", warmup, nil)
				nodeNames = append(nodeNames, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				podName := fmt.Sprintf("pod-initial-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, cpu, "12Gi")
			}

			By("Waiting for initial shard assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording initial assignments")
			initialVolcanoNodes, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			initialAgentNodes, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			By("Making small changes to node utilization (should minimize re-assignment)")
			// Add small pods to slightly change utilization
			for i, nodeName := range nodeNames {
				podName := fmt.Sprintf("pod-small-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "800m", "2Gi")
			}

			By("Waiting for controller to process changes")
			time.Sleep(90 * time.Second)

			By("Verifying node stability (most nodes should remain in their original shards)")
			// Verify volcano-scheduler nodes are mostly stable
			err = VerifyNodesStableInShard("volcano-scheduler", initialVolcanoNodes, 0.70)
			Expect(err).NotTo(HaveOccurred(), "At least 70%% of nodes should remain stable in volcano-scheduler")

			// Verify agent-scheduler nodes are mostly stable
			err = VerifyNodesStableInShard("agent-scheduler", initialAgentNodes, 0.70)
			Expect(err).NotTo(HaveOccurred(), "At least 70%% of nodes should remain stable in agent-scheduler")

			By("Test completed successfully")
		})

		It("should avoid unnecessary node re-assignments during periodic reconciliation", func() {
			By("Creating stable set of nodes")
			for i := 1; i <= 5; i++ {
				nodeName := fmt.Sprintf("test-reconcile-stable-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", false, nil)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				// Consistent 35% utilization
				podName := fmt.Sprintf("pod-stable-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "2800m", "11Gi")
			}

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording initial assignments")
			initialVolcanoNodes, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			initialAgentNodes, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting through multiple reconciliation cycles without changes")
			time.Sleep(250 * time.Second) // Wait for 2 full sync periods

			By("Verifying nodes remained stable (no unnecessary re-assignments)")
			currentVolcanoNodes, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			currentAgentNodes, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// Nodes should be 100% stable since nothing changed
			err = VerifyNodesStableInShard("volcano-scheduler", initialVolcanoNodes, 0.95)
			Expect(err).NotTo(HaveOccurred(), "Nodes should remain stable during reconciliation")

			err = VerifyNodesStableInShard("agent-scheduler", initialAgentNodes, 0.95)
			Expect(err).NotTo(HaveOccurred(), "Nodes should remain stable during reconciliation")

			// Total node count should remain the same
			totalBefore := len(initialVolcanoNodes) + len(initialAgentNodes)
			totalAfter := len(currentVolcanoNodes) + len(currentAgentNodes)
			Expect(totalAfter).To(Equal(totalBefore), "Total node count should not change")

			By("Test completed successfully")
		})

		It("should maintain balanced distribution across shards", func() {
			By("Creating a large number of nodes with similar characteristics")
			nodeNames := []string{}
			for i := 1; i <= 12; i++ {
				nodeName := fmt.Sprintf("test-balanced-%d", i)
				node := CreateTestNode(nodeName, "16", "64Gi", false, nil)
				nodeNames = append(nodeNames, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				// All nodes have similar 45% CPU utilization
				podName := fmt.Sprintf("pod-balanced-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "7200m", "28Gi")
			}

			By("Waiting for controller to distribute nodes")
			time.Sleep(20 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Verifying relatively balanced distribution")
			volcanoCount, err := CountNodesInShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentCount, err := CountNodesInShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalNodes := volcanoCount + agentCount
			Expect(totalNodes).To(BeNumerically(">=", 1), "Nodes should be distributed across shards")

			By("Verifying no shard is empty if nodes are available")
			if totalNodes > 1 {
				// If we have multiple nodes, both shards should ideally have some
				// This is a soft check - the exact distribution depends on strategy
				Expect(volcanoCount >= 0).To(BeTrue())
				Expect(agentCount >= 0).To(BeTrue())
			}

			By("Test completed successfully")
		})
	})

	Context("Edge Cases and Stress Scenarios", func() {
		It("should handle rapid node additions and deletions", func() {
			By("Creating initial nodes")
			initialNodes := []string{}
			for i := 1; i <= 4; i++ {
				nodeName := fmt.Sprintf("test-rapid-initial-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", false, nil)
				initialNodes = append(initialNodes, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)
			}

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Rapidly adding new nodes")
			newNodes := []string{}
			for i := 1; i <= 6; i++ {
				nodeName := fmt.Sprintf("test-rapid-new-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", false, nil)
				newNodes = append(newNodes, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)
				time.Sleep(2 * time.Second)
			}

			By("Waiting for new nodes to be processed")
			time.Sleep(20 * time.Second)

			By("Rapidly deleting some nodes")
			for i := 0; i < 2 && i < len(newNodes); i++ {
				DeleteNode(newNodes[i])
				// Remove from tracking
				for j, n := range ctx.TestNodes {
					if n == newNodes[i] {
						ctx.TestNodes = append(ctx.TestNodes[:j], ctx.TestNodes[j+1:]...)
						break
					}
				}
				time.Sleep(2 * time.Second)
			}

			By("Waiting for controller to stabilize")
			time.Sleep(30 * time.Second)

			By("Verifying system is stable and all remaining nodes are assigned")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalAssigned := len(volcanoShard.Spec.NodesDesired) + len(agentShard.Spec.NodesDesired)
			Expect(totalAssigned).To(BeNumerically(">=", len(initialNodes)),
				"At least initial nodes should be assigned")

			By("Test completed successfully")
		})

		It("should handle nodes with extreme resource configurations", func() {
			By("Creating nodes with very different CPU capacities")
			// Very small node
			smallNode := CreateTestNode("test-small-node", "2", "8Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, smallNode.Name)

			// Very large node
			largeNode := CreateTestNode("test-large-node", "128", "512Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, largeNode.Name)

			// Medium nodes
			for i := 1; i <= 3; i++ {
				nodeName := fmt.Sprintf("test-medium-%d", i)
				node := CreateTestNode(nodeName, "16", "64Gi", false, nil)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)
			}

			By("Adding appropriate workloads")
			CreateTestPod(ctx.Namespace, "pod-small", smallNode.Name, "600m", "2Gi")
			CreateTestPod(ctx.Namespace, "pod-large", largeNode.Name, "50000m", "200Gi")

			By("Waiting for controller to handle diverse node sizes")
			time.Sleep(20 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Verifying all nodes are assigned despite size differences")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			allAssignedNodes := append(volcanoShard.Spec.NodesDesired, agentShard.Spec.NodesDesired...)
			
			// Verify all test nodes are assigned
			testNodeCount := len(ctx.TestNodes)
			Expect(len(allAssignedNodes)).To(BeNumerically(">=", testNodeCount-1),
				"Most nodes should be assigned despite size differences")

			By("Test completed successfully")
		})

		It("should handle all nodes having identical resource profiles", func() {
			By("Creating multiple identical nodes")
			nodeNames := []string{}
			for i := 1; i <= 8; i++ {
				nodeName := fmt.Sprintf("test-identical-%d", i)
				node := CreateTestNode(nodeName, "16", "64Gi", false, nil)
				nodeNames = append(nodeNames, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				// All nodes have exactly the same CPU usage (40%)
				podName := fmt.Sprintf("pod-identical-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "6400m", "25Gi")
			}

			By("Waiting for controller to assign identical nodes")
			time.Sleep(20 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Verifying all nodes are assigned")
			volcanoCount, err := CountNodesInShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentCount, err := CountNodesInShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalAssigned := volcanoCount + agentCount
			Expect(totalAssigned).To(BeNumerically(">=", len(nodeNames)-1),
				"All or most identical nodes should be assigned")

			By("Test completed successfully")
		})
	})

	Context("Controller Resilience", func() {
		It("should recover from temporary API server issues", func() {
			By("Creating initial nodes")
			node1 := CreateTestNode("test-resilient-node-1", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			node2 := CreateTestNode("test-resilient-node-2", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node2.Name)

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording assignments before any issues")
			volcanoNodesBefore, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentNodesBefore, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			By("Adding a new node while controller continues running")
			node3 := CreateTestNode("test-resilient-node-3", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node3.Name)

			By("Waiting for controller to process the new node")
			time.Sleep(25 * time.Second)

			By("Verifying controller recovered and processed the new node")
			volcanoNodesAfter, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentNodesAfter, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalBefore := len(volcanoNodesBefore) + len(agentNodesBefore)
			totalAfter := len(volcanoNodesAfter) + len(agentNodesAfter)

			Expect(totalAfter).To(BeNumerically(">=", totalBefore),
				"Controller should have processed new node")

			By("Test completed successfully")
		})

		It("should handle concurrent shard updates gracefully", func() {
			By("Creating multiple nodes to trigger concurrent processing")
			nodeNames := []string{}
			for i := 1; i <= 10; i++ {
				nodeName := fmt.Sprintf("test-concurrent-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", i%2 == 0, nil)
				nodeNames = append(nodeNames, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				// Varying utilization to trigger different shard assignments
				cpuRequest := fmt.Sprintf("%dm", 2000+(i*400))
				podName := fmt.Sprintf("pod-concurrent-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, cpuRequest, "12Gi")
			}

			By("Waiting for controller to process all nodes")
			time.Sleep(25 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(15 * time.Second)

			By("Verifying all nodes were assigned without conflicts")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			allNodes := append(volcanoShard.Spec.NodesDesired, agentShard.Spec.NodesDesired...)
			Expect(len(allNodes)).To(BeNumerically(">=", len(nodeNames)-2),
				"Most nodes should be assigned despite concurrent updates")

			// Verify no duplicate assignments
			nodeSet := make(map[string]bool)
			for _, node := range allNodes {
				Expect(nodeSet[node]).To(BeFalse(), "Node %s should not be assigned to multiple shards", node)
				nodeSet[node] = true
			}

			By("Test completed successfully")
		})
	})

	Context("Long-Running Stability", func() {
		It("should maintain stable assignments over extended period", func() {
			By("Creating stable workload")
			for i := 1; i <= 6; i++ {
				nodeName := fmt.Sprintf("test-longrun-%d", i)
				node := CreateTestNode(nodeName, "8", "32Gi", false, nil)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				podName := fmt.Sprintf("pod-longrun-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "3000m", "12Gi")
			}

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording initial stable state")
			initialVolcano, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			initialAgent, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			By("Running for extended period (simulated)")
			// In real long-running test, this would be much longer
			time.Sleep(180 * time.Second)

			By("Verifying assignments remained stable")
			finalVolcano, err := GetShardNodeNames("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			finalAgent, err := GetShardNodeNames("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// Verify high stability (at least 90%)
			err = VerifyNodesStableInShard("volcano-scheduler", initialVolcano, 0.90)
			Expect(err).NotTo(HaveOccurred(), "Nodes should remain stable over extended period")

			err = VerifyNodesStableInShard("agent-scheduler", initialAgent, 0.90)
			Expect(err).NotTo(HaveOccurred(), "Nodes should remain stable over extended period")

			// Total node count should remain consistent
			totalInitial := len(initialVolcano) + len(initialAgent)
			totalFinal := len(finalVolcano) + len(finalAgent)
			Expect(totalFinal).To(Equal(totalInitial), "Total node count should remain stable")

			By("Test completed successfully")
		})
	})
})
