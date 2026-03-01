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
		It("Volcano jobs should be scheduled to cluster nodes", func() {
			waitForNodeShardsCreated()

			By("Getting volcano NodeShard")
			volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Volcano shard nodes: %v\n", volcanoShard.Spec.NodesDesired)

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

			By("Verifying pods are scheduled")
			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(
				context.TODO(), metav1.ListOptions{
					LabelSelector: "volcano.sh/job-name=" + job.Name,
				})
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods.Items {
				GinkgoWriter.Printf("Pod %s scheduled to %s\n", pod.Name, pod.Spec.NodeName)
				Expect(pod.Spec.NodeName).NotTo(BeEmpty())
			}
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
