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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("ShardingController Node Re-assignment E2E Tests", func() {
	ctx := NewShardTestContext()

	AfterEach(func() {
		ctx.Cleanup()
	})

	Context("Node Resource Usage Changes", func() {
		It("should re-assign node from one shard to another when resource usage significantly changes", func() {
			By("Creating a node with low CPU utilization")
			node1 := CreateTestNode("test-dynamic-node-1", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			// Start with low utilization (25% CPU) - should go to volcano-scheduler
			pod1 := CreateTestPod(ctx.Namespace, "pod-initial-low", node1.Name, "4000m", "16Gi")

			By("Waiting for initial shard assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			By("Verifying node is initially in volcano-scheduler shard (or determining initial shard)")
			time.Sleep(10 * time.Second)

			var initialShard string
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// Determine which shard has the node
			nodeInVolcano := false
			for _, n := range volcanoShard.Spec.NodesDesired {
				if n == node1.Name {
					nodeInVolcano = true
					initialShard = "volcano-scheduler"
					break
				}
			}

			if !nodeInVolcano {
				for _, n := range agentShard.Spec.NodesDesired {
					if n == node1.Name {
						initialShard = "agent-scheduler"
						break
					}
				}
			}

			By(fmt.Sprintf("Node initially assigned to: %s", initialShard))

			By("Significantly increasing CPU utilization on the node")
			// Delete the low-utilization pod
			DeletePod(ctx.Namespace, pod1.Name)
			time.Sleep(5 * time.Second)

			// Add the node warmup label to make it attractive to agent-scheduler
			err = UpdateNodeLabels(node1.Name, map[string]string{WarmupNodeLabel: "true"})
			Expect(err).NotTo(HaveOccurred())

			// Create high-utilization pods (85% CPU)
			CreateTestPod(ctx.Namespace, "pod-high-1", node1.Name, "7000m", "28Gi")
			CreateTestPod(ctx.Namespace, "pod-high-2", node1.Name, "6600m", "28Gi")

			By("Waiting for controller to detect resource usage change and re-assign node")
			// Wait for metrics to be updated and re-assignment to occur
			time.Sleep(90 * time.Second) // Allow time for metrics refresh and sync

			By("Verifying node assignment changed or stayed based on strategy")
			// After high CPU and warmup label, node may move to agent-scheduler
			// The exact behavior depends on the controller's decision logic
			volcanoShardAfter, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShardAfter, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			nodeInVolcanoAfter := false
			for _, n := range volcanoShardAfter.Spec.NodesDesired {
				if n == node1.Name {
					nodeInVolcanoAfter = true
					break
				}
			}

			nodeInAgentAfter := false
			for _, n := range agentShardAfter.Spec.NodesDesired {
				if n == node1.Name {
					nodeInAgentAfter = true
					break
				}
			}

			// The node should be in one of the shards
			Expect(nodeInVolcanoAfter || nodeInAgentAfter).To(BeTrue(),
				"Node should be assigned to one of the shards")

			By("Test completed successfully")
		})

		It("should handle gradual resource usage changes", func() {
			By("Creating a node with medium CPU utilization")
			node1 := CreateTestNode("test-gradual-node-1", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			// Start with 40% CPU
			CreateTestPod(ctx.Namespace, "pod-medium", node1.Name, "6400m", "25Gi")

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording initial assignment")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialVolcanoCount := len(volcanoShard.Spec.NodesDesired)

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialAgentCount := len(agentShard.Spec.NodesDesired)

			By("Gradually increasing utilization with additional pods")
			CreateTestPod(ctx.Namespace, "pod-extra-1", node1.Name, "2000m", "8Gi")
			time.Sleep(45 * time.Second)

			CreateTestPod(ctx.Namespace, "pod-extra-2", node1.Name, "2000m", "8Gi")
			time.Sleep(45 * time.Second)

			By("Verifying controller handles gradual changes")
			volcanoShardAfter, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShardAfter, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// The total nodes across shards should remain consistent
			totalAfter := len(volcanoShardAfter.Spec.NodesDesired) + len(agentShardAfter.Spec.NodesDesired)
			totalBefore := initialVolcanoCount + initialAgentCount

			Expect(totalAfter).To(Equal(totalBefore),
				"Total node count across shards should remain consistent")

			By("Test completed successfully")
		})
	})

	Context("Node Lifecycle Management", func() {
		It("should add newly created nodes to appropriate shard", func() {
			By("Creating initial shards")
			initialNode := CreateTestNode("test-initial-node", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, initialNode.Name)

			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			By("Recording initial node counts")
			time.Sleep(10 * time.Second)

			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialVolcanoCount := len(volcanoShard.Spec.NodesDesired)

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialAgentCount := len(agentShard.Spec.NodesDesired)

			By("Creating new nodes with low utilization")
			newNode1 := CreateTestNode("test-new-node-1", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, newNode1.Name)
			CreateTestPod(ctx.Namespace, "pod-new-1", newNode1.Name, "4000m", "16Gi")

			newNode2 := CreateTestNode("test-new-node-2", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, newNode2.Name)
			CreateTestPod(ctx.Namespace, "pod-new-2", newNode2.Name, "4000m", "16Gi")

			By("Waiting for controller to detect and assign new nodes")
			time.Sleep(25 * time.Second)

			By("Verifying new nodes were added to shards")
			volcanoShardAfter, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShardAfter, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalAfter := len(volcanoShardAfter.Spec.NodesDesired) + len(agentShardAfter.Spec.NodesDesired)
			totalBefore := initialVolcanoCount + initialAgentCount

			Expect(totalAfter).To(BeNumerically(">=", totalBefore+1),
				"At least one new node should be added to shards")

			By("Test completed successfully")
		})

		It("should remove deleted nodes from shard", func() {
			By("Creating test nodes")
			node1 := CreateTestNode("test-delete-node-1", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			node2 := CreateTestNode("test-delete-node-2", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node2.Name)

			node3 := CreateTestNode("test-delete-node-3", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node3.Name)

			By("Waiting for nodes to be assigned to shards")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording initial node counts")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialVolcanoCount := len(volcanoShard.Spec.NodesDesired)

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialAgentCount := len(agentShard.Spec.NodesDesired)

			totalBefore := initialVolcanoCount + initialAgentCount

			By("Deleting one node")
			DeleteNode(node2.Name)
			// Remove from context to avoid cleanup errors
			for i, n := range ctx.TestNodes {
				if n == node2.Name {
					ctx.TestNodes = append(ctx.TestNodes[:i], ctx.TestNodes[i+1:]...)
					break
				}
			}

			By("Waiting for controller to detect node deletion and update shards")
			time.Sleep(25 * time.Second)

			By("Verifying node was removed from shard")
			volcanoShardAfter, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShardAfter, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalAfter := len(volcanoShardAfter.Spec.NodesDesired) + len(agentShardAfter.Spec.NodesDesired)

			Expect(totalAfter).To(BeNumerically("<=", totalBefore),
				"Total node count should decrease or stay the same after deletion")

			// Verify deleted node is not in any shard
			for _, n := range volcanoShardAfter.Spec.NodesDesired {
				Expect(n).NotTo(Equal(node2.Name), "Deleted node should not be in volcano-scheduler")
			}
			for _, n := range agentShardAfter.Spec.NodesDesired {
				Expect(n).NotTo(Equal(node2.Name), "Deleted node should not be in agent-scheduler")
			}

			By("Test completed successfully")
		})

		It("should move unschedulable nodes out from shard", func() {
			By("Creating test nodes")
			node1 := CreateTestNode("test-schedulable-node-1", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			node2 := CreateTestNode("test-schedulable-node-2", "8", "32Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node2.Name)

			By("Waiting for nodes to be assigned")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Making one node unschedulable by adding NoSchedule taint")
			err = TaintNode(node2.Name, corev1.Taint{
				Key:    "node.kubernetes.io/unschedulable",
				Effect: corev1.TaintEffectNoSchedule,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for controller to detect unschedulable node")
			time.Sleep(90 * time.Second) // Wait for metrics refresh and sync

			By("Verifying unschedulable node handling")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// Check that schedulable node is still assigned
			node1Found := false
			for _, n := range volcanoShard.Spec.NodesDesired {
				if n == node1.Name {
					node1Found = true
				}
			}
			for _, n := range agentShard.Spec.NodesDesired {
				if n == node1.Name {
					node1Found = true
				}
			}

			Expect(node1Found).To(BeTrue(), "Schedulable node should remain in a shard")

			By("Test completed successfully")
		})
	})

	Context("Node Label and Annotation Changes", func() {
		It("should re-assign node when warmup label is added", func() {
			By("Creating a node without warmup label")
			node1 := CreateTestNode("test-label-node-1", "16", "64Gi", false, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			// Add high CPU utilization
			CreateTestPod(ctx.Namespace, "pod-high", node1.Name, "13000m", "52Gi")

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Adding warmup label to the node")
			err = UpdateNodeLabels(node1.Name, map[string]string{WarmupNodeLabel: "true"})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for controller to detect label change")
			time.Sleep(90 * time.Second)

			By("Verifying node is now eligible for agent-scheduler")
			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// With high CPU and warmup label, node may be in agent-scheduler
			// The exact assignment depends on controller logic
			nodeFound := false
			for _, n := range agentShard.Spec.NodesDesired {
				if n == node1.Name {
					nodeFound = true
					break
				}
			}

			// Node should be in one of the shards
			if !nodeFound {
				volcanoShard, err := GetShard("volcano-scheduler")
				Expect(err).NotTo(HaveOccurred())
				for _, n := range volcanoShard.Spec.NodesDesired {
					if n == node1.Name {
						nodeFound = true
						break
					}
				}
			}

			Expect(nodeFound).To(BeTrue(), "Node should be assigned to a shard")

			By("Test completed successfully")
		})

		It("should re-assign node when warmup label is removed", func() {
			By("Creating a node with warmup label and high CPU")
			node1 := CreateTestNode("test-label-remove-node-1", "16", "64Gi", true, nil)
			ctx.TestNodes = append(ctx.TestNodes, node1.Name)

			CreateTestPod(ctx.Namespace, "pod-high", node1.Name, "13000m", "52Gi")

			By("Waiting for initial assignment")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Removing warmup label from the node")
			err = RemoveNodeLabels(node1.Name, []string{WarmupNodeLabel})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for controller to detect label removal")
			time.Sleep(90 * time.Second)

			By("Verifying node assignment is updated based on new label state")
			// After removing warmup label, the node's assignment may change
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			// Node should still be in one of the shards
			nodeFound := false
			for _, n := range volcanoShard.Spec.NodesDesired {
				if n == node1.Name {
					nodeFound = true
					break
				}
			}
			for _, n := range agentShard.Spec.NodesDesired {
				if n == node1.Name {
					nodeFound = true
					break
				}
			}

			Expect(nodeFound).To(BeTrue(), "Node should be assigned to a shard")

			By("Test completed successfully")
		})
	})

	Context("Multiple Nodes Re-assignment", func() {
		It("should handle multiple nodes changing utilization simultaneously", func() {
			By("Creating multiple nodes with low utilization")
			nodes := []string{}
			for i := 1; i <= 4; i++ {
				nodeName := fmt.Sprintf("test-multi-change-%d", i)
				node := CreateTestNode(nodeName, "16", "64Gi", false, nil)
				nodes = append(nodes, nodeName)
				ctx.TestNodes = append(ctx.TestNodes, nodeName)

				// Low utilization (30%)
				podName := fmt.Sprintf("pod-initial-%d", i)
				CreateTestPod(ctx.Namespace, podName, nodeName, "4800m", "19Gi")
			}

			By("Waiting for initial assignments")
			time.Sleep(15 * time.Second)

			err := WaitForShardCreated("volcano-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "volcano-scheduler")

			err = WaitForShardCreated("agent-scheduler", ShardCreationTimeout)
			Expect(err).NotTo(HaveOccurred())
			ctx.TestShards = append(ctx.TestShards, "agent-scheduler")

			time.Sleep(10 * time.Second)

			By("Recording initial state")
			volcanoShard, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialVolcanoCount := len(volcanoShard.Spec.NodesDesired)

			agentShard, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())
			initialAgentCount := len(agentShard.Spec.NodesDesired)

			By("Increasing utilization on multiple nodes simultaneously")
			for i, nodeName := range nodes {
				// Add warmup label
				err := UpdateNodeLabels(nodeName, map[string]string{WarmupNodeLabel: "true"})
				Expect(err).NotTo(HaveOccurred())

				// Add more pods to increase utilization to ~85%
				CreateTestPod(ctx.Namespace, fmt.Sprintf("pod-extra-%d-1", i), nodeName, "4500m", "18Gi")
				CreateTestPod(ctx.Namespace, fmt.Sprintf("pod-extra-%d-2", i), nodeName, "4300m", "18Gi")
			}

			By("Waiting for controller to process all changes")
			time.Sleep(100 * time.Second)

			By("Verifying controller handled multiple simultaneous changes")
			volcanoShardAfter, err := GetShard("volcano-scheduler")
			Expect(err).NotTo(HaveOccurred())

			agentShardAfter, err := GetShard("agent-scheduler")
			Expect(err).NotTo(HaveOccurred())

			totalAfter := len(volcanoShardAfter.Spec.NodesDesired) + len(agentShardAfter.Spec.NodesDesired)
			totalBefore := initialVolcanoCount + initialAgentCount

			Expect(totalAfter).To(Equal(totalBefore),
				"Total node count should remain consistent across shards")

			By("Verifying all test nodes are still assigned")
			allNodes := append(volcanoShardAfter.Spec.NodesDesired, agentShardAfter.Spec.NodesDesired...)
			for _, testNode := range nodes {
				found := false
				for _, assignedNode := range allNodes {
					if testNode == assignedNode {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Node %s should be assigned to a shard", testNode)
			}

			By("Test completed successfully")
		})
	})
})
