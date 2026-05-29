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

package schedulersharding

import (
	"context"
	"fmt"
	"sort"
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
	// VolcanoSchedulerName is the scheduler name for Volcano
	VolcanoSchedulerName    = "volcano"
	VolcanoShardName        = "volcano"
	AgentSchedulerShardName = "agent-scheduler"

	schedulerShardingNodeLabel        = "volcano.sh/e2e-scheduler-sharding"
	schedulerShardingNodeValue        = "preferred"
	schedulerShardingUpdatedNodeValue = "updated"
	volcanoSystemNamespace            = "volcano-system"
	shardingConfigMapName             = "integration-sharding-configmap"
	shardingConfigMapDataKey          = "sharding.yaml"

	pollInterval            = 500 * time.Millisecond
	nodeShardRefreshTimeout = time.Minute
	holdResourcesCommand    = "sleep 3600"
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

	// waitForNodeShardsCreated waits for both volcano and agent-scheduler NodeShards.
	waitForNodeShardsCreated := func() {
		By("Waiting for ShardingController to create NodeShards")
		err := e2eutil.WaitForNodeShardsCreated(ctx, []string{VolcanoShardName, AgentSchedulerShardName})
		Expect(err).NotTo(HaveOccurred(), "NodeShards should be created by ShardingController")
	}

	getShardPartition := func() ([]string, []string) {
		waitForNodeShardsCreated()

		volcanoShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
		Expect(err).NotTo(HaveOccurred())
		agentShard, err := e2eutil.GetNodeShard(ctx, AgentSchedulerShardName)
		Expect(err).NotTo(HaveOccurred())

		volcanoNodes := filterWorkerNodeNames(ctx, volcanoShard.Spec.NodesDesired)
		if len(volcanoNodes) == 0 {
			volcanoNodes = append([]string(nil), volcanoShard.Spec.NodesDesired...)
		}
		volcanoNodeSet := stringSet(volcanoNodes)
		agentOnlyNodes := make([]string, 0, len(agentShard.Spec.NodesDesired))
		for _, n := range agentShard.Spec.NodesDesired {
			if _, ok := volcanoNodeSet[n]; !ok && !isControlPlaneNode(ctx, n) {
				agentOnlyNodes = append(agentOnlyNodes, n)
			}
		}

		GinkgoWriter.Printf("Volcano shard nodes: %v\n", volcanoNodes)
		GinkgoWriter.Printf("Agent-only shard nodes: %v\n", agentOnlyNodes)
		Expect(volcanoNodes).NotTo(BeEmpty(), "volcano shard must have nodes")
		Expect(agentOnlyNodes).NotTo(BeEmpty(),
			"agent-scheduler shard must have nodes outside the volcano shard for sharding verification")
		return volcanoNodes, agentOnlyNodes
	}

	prepareSingleWorkerShard := func() (string, []string, func()) {
		waitForNodeShardsCreated()
		workers := workerNodeNames(ctx)
		Expect(len(workers)).To(BeNumerically(">=", 2), "scheduler sharding tests need at least two worker nodes")
		volcanoNode := workers[0]
		agentOnlyNodes := append([]string(nil), workers[1:]...)

		cleanupWarmupLabel := addNodeLabel(ctx, volcanoNode, schedulerShardingNodeLabel, schedulerShardingNodeValue)
		originalConfig := updateShardingConfigMap(ctx, preferredNodeShardingConfig())
		restored := false
		restore := func() {
			if restored {
				return
			}
			restoreShardingConfigMap(ctx, originalConfig)
			cleanupWarmupLabel()
			restored = true
		}

		err := waitForNodeShardSpec(ctx, VolcanoShardName, []string{volcanoNode})
		Expect(err).NotTo(HaveOccurred(), "volcano NodeShard spec should use the pinned worker shard")
		if !currentSpecHasLabel("none") {
			err = waitForNodeShardStatus(ctx, VolcanoShardName, []string{volcanoNode})
			Expect(err).NotTo(HaveOccurred(), "scheduler should observe the pinned worker shard")
		}
		return volcanoNode, agentOnlyNodes, restore
	}

	Describe("Sharding Infrastructure", Label("hard", "soft", "none"), func() {
		It("Volcano NodeShard should exist with nodes assigned", func() {
			volcanoNodes, _ := getShardPartition()
			Expect(volcanoNodes).NotTo(BeEmpty(), "volcano shard should have at least 1 node assigned")
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

	Describe("Basic Volcano Job Scheduling with Sharding", Label("hard", "soft"), func() {
		It("Volcano jobs should be scheduled only to volcano shard nodes", func() {
			volcanoNode, _, restore := prepareSingleWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, VolcanoShardName, []string{volcanoNode})
			Expect(err).NotTo(HaveOccurred(), "scheduler should observe the test worker shard")
			volcanoNodes := []string{volcanoNode}

			By("Creating a Volcano job")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "volcano-sharding-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         2,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})

			By("Waiting for job to be ready")
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should become ready")

			By("Verifying all pods are scheduled onto volcano shard nodes")
			expectJobPodsOnNodes(ctx, job.Name, stringSet(volcanoNodes))
		})

	})

	Describe("Soft Mode", Label("soft"), func() {
		It("should prefer the shard node when shard and outside nodes are both feasible", func() {
			volcanoNode, _, restore := prepareSingleWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, VolcanoShardName, []string{volcanoNode})
			Expect(err).NotTo(HaveOccurred(), "scheduler should observe the test worker shard")

			By("Creating a job that can fit on either shard or outside-shard nodes")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "soft-prefer-shard-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should become ready")
			expectJobPodsOnNodes(ctx, job.Name, stringSet([]string{volcanoNode}))
		})

		It("should fall back outside the shard when in-shard resources are exhausted", func() {
			volcanoNode, agentOnlyNodes, restore := prepareSingleWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, VolcanoShardName, []string{volcanoNode})
			Expect(err).NotTo(HaveOccurred(), "scheduler should observe the test worker shard")
			createBoundPlaceholderPod(ctx, "soft-fill-shard-node", volcanoNode, allocatableCPU(ctx, volcanoNode))

			By("Creating a job that should use outside-shard capacity")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "soft-fallback-shard-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should fall back outside the shard")
			expectJobPodsOnNodes(ctx, job.Name, stringSet(agentOnlyNodes))
		})
	})

	Describe("None Mode", Label("none"), func() {
		It("should not force placement onto the shard node", func() {
			volcanoNode, agentOnlyNodes, restore := prepareSingleWorkerShard()
			defer restore()
			createBoundPlaceholderPod(ctx, "none-make-shard-less-preferred", volcanoNode, fractionOfAllocatableCPU(ctx, volcanoNode, 2))

			By("Creating a job that can still fit on the shard node but has better outside-shard scores")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "none-score-outside",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should become ready")
			expectJobPodsOnNodes(ctx, job.Name, stringSet(agentOnlyNodes))
		})

		It("should schedule outside the shard when in-shard resources are exhausted", func() {
			volcanoNode, agentOnlyNodes, restore := prepareSingleWorkerShard()
			defer restore()
			createBoundPlaceholderPod(ctx, "none-fill-shard-node", volcanoNode, allocatableCPU(ctx, volcanoNode))

			By("Creating a job that should use outside-shard capacity")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "none-fallback-shard-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should fall back outside the shard")
			expectJobPodsOnNodes(ctx, job.Name, stringSet(agentOnlyNodes))
		})
	})

	Describe("Hard Mode", Label("hard"), func() {
		It("should keep a second job pending when in-shard resources are exhausted", func() {
			volcanoNode, agentOnlyNodes, restore := prepareSingleWorkerShard()
			defer restore()
			err := waitForNodeShardStatus(ctx, VolcanoShardName, []string{volcanoNode})
			Expect(err).NotTo(HaveOccurred(), "scheduler should observe the test worker shard")
			createBoundPlaceholderPod(ctx, "hard-fill-shard-node", volcanoNode, allocatableCPU(ctx, volcanoNode))

			By("Creating a job that must not fall back outside the volcano shard")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "hard-exhaust-shard-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err = e2eutil.WaitJobUnschedulable(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should remain unschedulable in hard mode")
			expectJobPodsNotOnNodes(ctx, job.Name, stringSet(agentOnlyNodes))
		})
	})

	Describe("Shard State Consistency", Label("hard"), func() {
		It("should refresh scheduler shard membership after NodeShard spec changes", func() {
			volcanoNode, agentOnlyNodes, restore := prepareSingleWorkerShard()
			defer restore()
			originalVolcanoNodes := []string{volcanoNode}
			newVolcanoNodes := []string{agentOnlyNodes[0]}
			cleanupWarmupLabel := addNodeLabel(ctx, newVolcanoNodes[0], schedulerShardingNodeLabel, schedulerShardingUpdatedNodeValue)
			defer cleanupWarmupLabel()
			err := waitForNodeShardStatus(ctx, VolcanoShardName, originalVolcanoNodes)
			Expect(err).NotTo(HaveOccurred(), "scheduler should observe original test shard membership")

			By("Creating a baseline Volcano job before the shard update")
			baselineJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "original-shard-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err = e2eutil.WaitJobReady(ctx, baselineJob)
			Expect(err).NotTo(HaveOccurred(), "baseline job should become ready on the original shard")
			expectJobPodsOnNodes(ctx, baselineJob.Name, stringSet(originalVolcanoNodes))
			err = waitForNodeShardStatus(ctx, VolcanoShardName, originalVolcanoNodes)
			Expect(err).NotTo(HaveOccurred(), "scheduler should publish original NodeShard status before the spec update")

			By(fmt.Sprintf("Updating sharding config to move volcano shard from %v to %v", originalVolcanoNodes, newVolcanoNodes))
			originalConfig := updateShardingConfigMap(ctx, preferredNodeShardingConfigForValue(schedulerShardingUpdatedNodeValue))
			restoredConfig := false
			defer func() {
				if restoredConfig {
					return
				}
				restoreShardingConfigMap(ctx, originalConfig)
			}()

			err = waitForNodeShardSpec(ctx, VolcanoShardName, newVolcanoNodes)
			Expect(err).NotTo(HaveOccurred(), "volcano NodeShard spec should reflect updated shard membership")

			By("Waiting for scheduler to publish refreshed NodeShard status")
			err = waitForNodeShardStatus(ctx, VolcanoShardName, newVolcanoNodes)
			Expect(err).NotTo(HaveOccurred(), "scheduler should refresh NodeShard status after spec update")
			updatedShard, err := e2eutil.GetNodeShard(ctx, VolcanoShardName)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedShard.Status.NodesToAdd).To(BeEmpty(), "updated shard nodes should no longer be pending add")
			Expect(updatedShard.Status.NodesToRemove).To(BeEmpty(), "updated shard should not expose stale nodes to remove")

			By("Creating a new Volcano job after the shard update")
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name: "updated-shard-job",
				Tasks: []e2eutil.TaskSpec{
					{
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
					},
				},
			})
			err = e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "job should become ready after scheduler refreshes shard membership")
			expectJobPodsOnNodes(ctx, job.Name, stringSet(newVolcanoNodes))

			By("Restoring the original sharding config")
			restoreShardingConfigMap(ctx, originalConfig)
			err = waitForNodeShardSpec(ctx, VolcanoShardName, originalVolcanoNodes)
			Expect(err).NotTo(HaveOccurred(), "volcano NodeShard spec should return to the original shard membership")
			err = waitForNodeShardStatus(ctx, VolcanoShardName, originalVolcanoNodes)
			Expect(err).NotTo(HaveOccurred(), "scheduler should refresh NodeShard status after restore")
			restoredConfig = true
		})

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
						Img:         e2eutil.DefaultBusyBoxImage,
						Command:     holdResourcesCommand,
						Req:         e2eutil.HalfCPU,
						Min:         1,
						Rep:         1,
						Tolerations: controlPlaneTolerations(),
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
			sort.Strings(initialNodes)
			finalNodes := append([]string(nil), finalShard.Spec.NodesDesired...)
			sort.Strings(finalNodes)
			Expect(finalNodes).To(Equal(initialNodes), "shard should maintain node assignments")
		})
	})
})

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

func sameStringSet(actual, expected map[string]struct{}) bool {
	if len(actual) != len(expected) {
		return false
	}
	return containsAll(actual, expected)
}

func containsAll(actual, expected map[string]struct{}) bool {
	for n := range expected {
		if _, ok := actual[n]; !ok {
			return false
		}
	}
	return true
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
			GinkgoWriter.Printf("Failed to get node %s for label cleanup: %v\n", nodeName, err)
			return
		}
		nodeCopy := node.DeepCopy()
		if hadOriginalValue {
			nodeCopy.Labels[key] = originalValue
		} else {
			delete(nodeCopy.Labels, key)
		}
		if _, err := ctx.Kubeclient.CoreV1().Nodes().Update(context.TODO(), nodeCopy, metav1.UpdateOptions{}); err != nil {
			GinkgoWriter.Printf("Failed to clean up label %s on node %s: %v\n", key, nodeName, err)
		}
	}
}

func updateShardingConfigMap(ctx *e2eutil.TestContext, data string) string {
	cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNamespace).Get(
		context.TODO(), shardingConfigMapName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get sharding ConfigMap")

	originalData := cm.Data[shardingConfigMapDataKey]
	cmCopy := cm.DeepCopy()
	if cmCopy.Data == nil {
		cmCopy.Data = make(map[string]string)
	}
	cmCopy.Data[shardingConfigMapDataKey] = data
	_, err = ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNamespace).Update(
		context.TODO(), cmCopy, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to update sharding ConfigMap")
	return originalData
}

func restoreShardingConfigMap(ctx *e2eutil.TestContext, data string) {
	cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNamespace).Get(
		context.TODO(), shardingConfigMapName, metav1.GetOptions{})
	if err != nil {
		GinkgoWriter.Printf("Failed to get sharding ConfigMap for restore: %v\n", err)
		return
	}

	cmCopy := cm.DeepCopy()
	if cmCopy.Data == nil {
		cmCopy.Data = make(map[string]string)
	}
	cmCopy.Data[shardingConfigMapDataKey] = data
	if _, err := ctx.Kubeclient.CoreV1().ConfigMaps(volcanoSystemNamespace).Update(
		context.TODO(), cmCopy, metav1.UpdateOptions{}); err != nil {
		GinkgoWriter.Printf("Failed to restore sharding ConfigMap: %v\n", err)
	}
}

func preferredNodeShardingConfig() string {
	return preferredNodeShardingConfigForValue(schedulerShardingNodeValue)
}

func preferredNodeShardingConfigForValue(warmupLabelValue string) string {
	return fmt.Sprintf(`schedulerConfigs:
  - name: volcano
    type: volcano
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
  - name: agent-scheduler
    type: agent
    policies:
      - name: node-limit
        arguments:
          minNodes: 1
          maxNodes: 100
shardSyncPeriod: "1s"
enableNodeEventTrigger: true
`, schedulerShardingNodeLabel, warmupLabelValue)
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

func waitForNodeShardStatus(ctx *e2eutil.TestContext, shardName string, expectedNodesInUse []string) error {
	expected := stringSet(expectedNodesInUse)
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, nodeShardRefreshTimeout, true,
		func(c context.Context) (bool, error) {
			shard, err := e2eutil.GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			nodesInUse := stringSet(shard.Status.NodesInUse)
			return sameStringSet(nodesInUse, expected), nil
		})
}

func waitForNodeShardSpec(ctx *e2eutil.TestContext, shardName string, expectedNodesDesired []string) error {
	expected := stringSet(expectedNodesDesired)
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, nodeShardRefreshTimeout, true,
		func(c context.Context) (bool, error) {
			shard, err := e2eutil.GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			return sameStringSet(stringSet(shard.Spec.NodesDesired), expected), nil
		})
}

func listJobPods(ctx *e2eutil.TestContext, jobName string) []corev1.Pod {
	pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(
		context.TODO(), metav1.ListOptions{
			LabelSelector: "volcano.sh/job-name=" + jobName,
		})
	Expect(err).NotTo(HaveOccurred())
	Expect(pods.Items).NotTo(BeEmpty(), "job should have pods")
	return pods.Items
}

func expectJobPodsOnNodes(ctx *e2eutil.TestContext, jobName string, allowedNodes map[string]struct{}) {
	for _, pod := range listJobPods(ctx, jobName) {
		GinkgoWriter.Printf("Pod %s scheduled to %s\n", pod.Name, pod.Spec.NodeName)
		Expect(pod.Spec.NodeName).NotTo(BeEmpty())
		Expect(allowedNodes).To(HaveKey(pod.Spec.NodeName),
			fmt.Sprintf("pod %s scheduled to node %q outside expected nodes %v",
				pod.Name, pod.Spec.NodeName, allowedNodes))
	}
}

func expectJobPodsNotOnNodes(ctx *e2eutil.TestContext, jobName string, forbiddenNodes map[string]struct{}) {
	for _, pod := range listJobPods(ctx, jobName) {
		if pod.Spec.NodeName == "" {
			continue
		}
		Expect(forbiddenNodes).NotTo(HaveKey(pod.Spec.NodeName),
			fmt.Sprintf("pod %s scheduled to forbidden outside-shard node %q",
				pod.Name, pod.Spec.NodeName))
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
