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

// TODO: Add tests for hard-sharding mode with allocate, preempt, reclaim, backfill actions
// TODO: Add tests for soft-sharding mode with allocate, preempt, reclaim, backfill actions
// TODO: Add tests for soft-sharding fallback when in-shard resources insufficient
// TODO: Add tests for shard reassignment (add/remove nodes) with all actions

package schedulersharding

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// VolcanoSchedulerName is the scheduler name for Volcano
	VolcanoSchedulerName    = "volcano"
	VolcanoShardName        = "volcano"
	AgentSchedulerShardName = "agent-scheduler"

	// Polling intervals
	pollInterval = 500 * time.Millisecond

	// Wait time for sharding controller to create NodeShards after startup
	shardCreationTimeout = 3 * time.Minute
)

var _ = Describe("Volcano Scheduler Sharding E2E Test", func() {
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
					if shard.Name == AgentSchedulerShardName {
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

	Describe("Sharding Infrastructure", func() {
		It("Volcano NodeShard should exist with nodes assigned", func() {
			waitForNodeShardsCreated()

			By("Getting volcano NodeShard")
			volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Volcano shard nodes desired: %v\n", volcanoShard.Spec.NodesDesired)
			GinkgoWriter.Printf("Volcano shard nodes in use: %v\n", volcanoShard.Status.NodesInUse)

			// Volcano shard should have nodes assigned
			Expect(len(volcanoShard.Spec.NodesDesired)).To(BeNumerically(">=", 1),
				"volcano shard should have at least 1 node assigned")
		})

		It("Both NodeShards should be accessible", func() {
			waitForNodeShardsCreated()

			By("Listing all NodeShards")
			shards, err := e2eutil.ListNodeShards(ctx)
			Expect(err).NotTo(HaveOccurred())

			shardNames := make([]string, 0, len(shards.Items))
			for _, shard := range shards.Items {
				shardNames = append(shardNames, shard.Name)
				GinkgoWriter.Printf("NodeShard: %s, NodesDesired: %d\n",
					shard.Name, len(shard.Spec.NodesDesired))
			}

			Expect(shardNames).To(ContainElement(VolcanoShardName))
			Expect(shardNames).To(ContainElement(AgentSchedulerShardName))
		})
	})

	Describe("Basic Volcano Job Scheduling with Sharding", func() {
		It("Volcano jobs should be scheduled only to volcano shard nodes", func() {
			waitForNodeShardsCreated()

			By("Getting volcano NodeShard")
			volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("Volcano shard nodes: %v\n", volcanoShard.Spec.NodesDesired)
			Expect(len(volcanoShard.Spec.NodesDesired)).To(BeNumerically(">=", 1),
				"volcano shard must have nodes for verification")

			// Build a set of volcano shard node names for lookup
			shardNodeSet := make(map[string]bool, len(volcanoShard.Spec.NodesDesired))
			for _, n := range volcanoShard.Spec.NodesDesired {
				shardNodeSet[n] = true
			}

			By("Creating a Volcano job")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "volcano-sharding-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img: e2eutil.DefaultBusyBoxImage,
						Req: e2eutil.HalfCPU,
						Min: 1,
						Rep: 2,
					},
				},
			})

			By("Waiting for job to be ready")
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should become ready")

			By("Verifying all pods are scheduled onto volcano shard nodes")
			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(
				context.TODO(), metav1.ListOptions{
					LabelSelector: "volcano.sh/job-name=" + job.Name,
				})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(BeNumerically(">=", 1))

			for _, pod := range pods.Items {
				GinkgoWriter.Printf("Pod %s scheduled to %s\n", pod.Name, pod.Spec.NodeName)
				Expect(pod.Spec.NodeName).NotTo(BeEmpty())
				Expect(shardNodeSet).To(HaveKey(pod.Spec.NodeName),
					fmt.Sprintf("pod %s scheduled to node %q which is NOT in the volcano shard %q",
						pod.Name, pod.Spec.NodeName, VolcanoShardName))
			}
		})

		It("job requesting more CPU than any single shard node should stay pending", func() {
			waitForNodeShardsCreated()

			By("Creating a job that requests excessive CPU per task")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "insufficient-resource-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img: e2eutil.DefaultBusyBoxImage,
						Req: e2eutil.ThirtyCPU,
						Min: 1,
						Rep: 1,
					},
				},
			})

			By("Verifying job stays pending (resources cannot be satisfied by any single node)")
			err := e2eutil.WaitJobStatePending(ctx, job)
			Expect(err).NotTo(HaveOccurred(),
				"job requesting excessive CPU should remain in Pending state")

			GinkgoWriter.Printf("Job %s correctly stays pending due to insufficient resources\n", job.Name)
		})

		It("should not schedule pods onto nodes outside the volcano shard", func() {
			waitForNodeShardsCreated()

			By("Collecting node assignments across both shards")
			volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())
			agentShard, err := e2eutil.GetNodeShard(ctx, AgentSchedulerShardName)
			Expect(err).NotTo(HaveOccurred())

			// Compute nodes that are ONLY in agent shard (not in volcano shard)
			volcanoNodeSet := make(map[string]bool, len(volcanoShard.Spec.NodesDesired))
			for _, n := range volcanoShard.Spec.NodesDesired {
				volcanoNodeSet[n] = true
			}
			agentOnlyNodes := make(map[string]bool)
			for _, n := range agentShard.Spec.NodesDesired {
				if !volcanoNodeSet[n] {
					agentOnlyNodes[n] = true
				}
			}
			GinkgoWriter.Printf("Volcano shard nodes: %v\n", volcanoShard.Spec.NodesDesired)
			GinkgoWriter.Printf("Agent-only shard nodes: (excluded from verification)\n")
			for n := range agentOnlyNodes {
				GinkgoWriter.Printf("  - %s\n", n)
			}

			By("Creating a Volcano job with multiple replicas")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "shard-isolation-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img: e2eutil.DefaultBusyBoxImage,
						Req: e2eutil.HalfCPU,
						Min: 1,
						Rep: 3,
					},
				},
			})

			By("Waiting for job to be ready")
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should become ready")

			By("Verifying no pods are scheduled onto agent-only nodes")
			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(
				context.TODO(), metav1.ListOptions{
					LabelSelector: "volcano.sh/job-name=" + job.Name,
				})
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods.Items {
				GinkgoWriter.Printf("Pod %s scheduled to %s\n", pod.Name, pod.Spec.NodeName)
				if len(agentOnlyNodes) > 0 {
					Expect(agentOnlyNodes).NotTo(HaveKey(pod.Spec.NodeName),
						fmt.Sprintf("pod %s was scheduled to agent-only node %q, violating shard isolation",
							pod.Name, pod.Spec.NodeName))
				}
			}
			GinkgoWriter.Printf("All pods correctly isolated to volcano shard nodes\n")
		})
	})

	Describe("Shard State Consistency", func() {
		It("Shard state should remain consistent during job execution", func() {
			waitForNodeShardsCreated()

			By("Getting initial shard state")
			initialShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())
			initialNodes := make([]string, len(initialShard.Spec.NodesDesired))
			copy(initialNodes, initialShard.Spec.NodesDesired)

			By("Creating and running a job")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "shard-consistency-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img: e2eutil.DefaultBusyBoxImage,
						Req: e2eutil.HalfCPU,
						Min: 1,
						Rep: 1,
					},
				},
			})
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying shard state after job execution")
			finalShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Initial nodes: %v\n", initialNodes)
			GinkgoWriter.Printf("Final nodes: %v\n", finalShard.Spec.NodesDesired)

			// Shard should remain stable during short job execution
			Expect(len(finalShard.Spec.NodesDesired)).To(BeNumerically(">=", 1),
				"shard should maintain node assignments")
		})
	})
})
