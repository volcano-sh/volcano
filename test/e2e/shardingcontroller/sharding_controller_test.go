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

package shardingcontroller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"volcano.sh/volcano/pkg/controllers/sharding"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// Sharding ConfigMap coordinates (must match helm release name and namespace)
	shardingConfigMapName      = "integration-sharding-configmap"
	shardingConfigMapNamespace = "volcano-system"

	// Polling intervals
	pollInterval = 500 * time.Millisecond

	// Wait time for sharding controller to create NodeShards after startup
	shardCreationTimeout = 3 * time.Minute

	// Wait time for shard reassignment after workload changes
	reassignmentTimeout = 3 * time.Minute
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

	// getShardingConfig reads the sharding configuration from the ConfigMap deployed by helm.
	getShardingConfig := func() *sharding.ShardingConfig {
		cfg, err := e2eutil.GetShardingConfigFromConfigMap(ctx, shardingConfigMapName, shardingConfigMapNamespace)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to read sharding config from ConfigMap")
		return cfg
	}

	// waitForNodeShardsCreated waits for all NodeShards defined in the ConfigMap to be created.
	waitForNodeShardsCreated := func(cfg *sharding.ShardingConfig) {
		By("Waiting for ShardingController to create NodeShards")
		expectedNames := make(map[string]bool, len(cfg.SchedulerConfigs))
		for _, sc := range cfg.SchedulerConfigs {
			expectedNames[sc.Name] = false
		}
		err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, shardCreationTimeout, true,
			func(c context.Context) (bool, error) {
				shards, err := e2eutil.ListNodeShards(ctx)
				if err != nil {
					GinkgoWriter.Printf("Error listing NodeShards: %v\n", err)
					return false, nil
				}
				found := 0
				for _, shard := range shards.Items {
					if _, ok := expectedNames[shard.Name]; ok {
						found++
					}
				}
				if found == len(expectedNames) {
					GinkgoWriter.Printf("All %d expected NodeShards found\n", found)
					return true, nil
				}
				GinkgoWriter.Printf("Waiting for NodeShards... found=%d, expected=%d, total=%d\n",
					found, len(expectedNames), len(shards.Items))
				return false, nil
			})
		Expect(err).NotTo(HaveOccurred(), "NodeShards should be created for all configured schedulers")
	}

	Describe("Shard Creation", func() {
		It("should create NodeShards matching the ConfigMap configuration", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			By("Listing all NodeShards in the cluster")
			shards, err := e2eutil.ListNodeShards(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to list NodeShards")

			By("Verifying NodeShard count matches ConfigMap")
			Expect(len(shards.Items)).To(BeNumerically(">=", len(cfg.SchedulerConfigs)),
				"at least as many NodeShards as configured schedulers should exist")

			By("Verifying each configured scheduler has a corresponding NodeShard")
			shardMap := make(map[string]bool)
			for _, shard := range shards.Items {
				shardMap[shard.Name] = true
				GinkgoWriter.Printf("Found NodeShard: %s with %d nodes desired, %d nodes in use\n",
					shard.Name, len(shard.Spec.NodesDesired), len(shard.Status.NodesInUse))
			}

			for _, sc := range cfg.SchedulerConfigs {
				Expect(shardMap).To(HaveKey(sc.Name),
					fmt.Sprintf("NodeShard for scheduler %q should exist", sc.Name))
			}
		})

		It("should have worker nodes assigned to shards after controller startup", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			By("Getting cluster worker nodes")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to list nodes")
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1),
				"cluster should have at least 1 worker node")

			By("Summing nodes assigned across all configured shards")
			totalAssigned := 0
			for _, sc := range cfg.SchedulerConfigs {
				shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
				Expect(err).NotTo(HaveOccurred(), "failed to get NodeShard %s", sc.Name)
				GinkgoWriter.Printf("Shard %s: %d nodes desired\n", sc.Name, len(shard.Spec.NodesDesired))
				totalAssigned += len(shard.Spec.NodesDesired)
			}

			GinkgoWriter.Printf("Total worker nodes: %d, Total assigned: %d\n", len(workerNodes), totalAssigned)
			Expect(totalAssigned).To(BeNumerically(">", 0),
				"at least one node should be assigned to a shard")
		})

		It("should recreate a NodeShard after it is manually deleted", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			schedulerName := cfg.SchedulerConfigs[0].Name

			By(fmt.Sprintf("Deleting NodeShard %q to simulate accidental removal", schedulerName))
			err := e2eutil.DeleteNodeShard(ctx, schedulerName)
			Expect(err).NotTo(HaveOccurred(), "failed to delete NodeShard")

			By("Waiting for ShardingController to recreate the NodeShard")
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					_, err := e2eutil.GetNodeShard(ctx, schedulerName)
					return err == nil, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("NodeShard %q should be recreated by the controller after deletion", schedulerName))
		})
	})

	Describe("CPU-Based Node Assignment", func() {
		It("nodes with low CPU utilization should be assigned to the low-utilization shard", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Find the scheduler configured for low CPU utilization (cpuUtilizationMin == 0)
			var lowUtilScheduler *sharding.SchedulerConfigSpec
			for i := range cfg.SchedulerConfigs {
				if cfg.SchedulerConfigs[i].CPUUtilizationMin == 0.0 {
					lowUtilScheduler = &cfg.SchedulerConfigs[i]
					break
				}
			}
			Expect(lowUtilScheduler).NotTo(BeNil(),
				"ConfigMap should have a scheduler with cpuUtilizationMin=0")

			By(fmt.Sprintf("Verifying low-utilization scheduler %q shard has nodes", lowUtilScheduler.Name))
			// In a fresh kind cluster with no workloads, all worker nodes have ~0% CPU utilization
			// and should be assigned to the low-utilization shard
			err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, err := e2eutil.GetNodeShard(ctx, lowUtilScheduler.Name)
					if err != nil {
						return false, nil
					}
					if len(shard.Spec.NodesDesired) > 0 {
						GinkgoWriter.Printf("Low-util shard %q has %d nodes: %v\n",
							lowUtilScheduler.Name, len(shard.Spec.NodesDesired), shard.Spec.NodesDesired)
						return true, nil
					}
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				"low-utilization shard should have nodes assigned in a fresh cluster")
		})

		It("nodes with high CPU utilization should be assigned to the high-utilization shard", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Find the scheduler configured for high CPU utilization
			var highUtilScheduler *sharding.SchedulerConfigSpec
			for i := range cfg.SchedulerConfigs {
				if cfg.SchedulerConfigs[i].CPUUtilizationMax >= 1.0 && cfg.SchedulerConfigs[i].CPUUtilizationMin > 0 {
					highUtilScheduler = &cfg.SchedulerConfigs[i]
					break
				}
			}
			Expect(highUtilScheduler).NotTo(BeNil(),
				"ConfigMap should have a scheduler for high CPU utilization (max=1.0, min>0)")

			By("Getting a worker node to load")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1))
			targetNode := workerNodes[0]

			// Calculate CPU request to push utilization above the high-util shard's min threshold.
			// CPU utilization = total pod CPU requests / node CPU capacity.
			cpuCapacity := targetNode.Status.Capacity.Cpu().MilliValue()
			targetUtilization := (highUtilScheduler.CPUUtilizationMin + highUtilScheduler.CPUUtilizationMax) / 2
			cpuRequestMillis := int64(float64(cpuCapacity) * targetUtilization)
			if cpuRequestMillis < 1 {
				cpuRequestMillis = 1
			}

			By(fmt.Sprintf("Creating CPU stress pod on node %s requesting %dm CPU (target util %.0f%%, capacity %dm)",
				targetNode.Name, cpuRequestMillis, targetUtilization*100, cpuCapacity))

			stressPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-stress-high-util",
					Namespace: ctx.Namespace,
				},
				Spec: corev1.PodSpec{
					NodeName:      targetNode.Name,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "stress",
							Image:   "busybox",
							Command: []string{"sleep", "3600"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: *resource.NewMilliQuantity(cpuRequestMillis, resource.DecimalSI),
								},
							},
						},
					},
				},
			}
			_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), stressPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create CPU stress pod")

			By(fmt.Sprintf("Waiting for node %s to appear in high-util shard %q", targetNode.Name, highUtilScheduler.Name))
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, err := e2eutil.GetNodeShard(ctx, highUtilScheduler.Name)
					if err != nil {
						return false, nil
					}
					for _, n := range shard.Spec.NodesDesired {
						if n == targetNode.Name {
							GinkgoWriter.Printf("Node %s found in high-util shard %q\n",
								targetNode.Name, highUtilScheduler.Name)
							return true, nil
						}
					}
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("node %s should be assigned to high-util shard %q after CPU load",
					targetNode.Name, highUtilScheduler.Name))
		})

		It("node with CPU utilization in the gap between shard ranges should not be assigned to any shard", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Find the largest gap between any two configured shard CPU ranges.
			// A utilization value inside such a gap should match no shard.
			type gap struct{ lo, hi float64 }
			var chosen *gap
			for i := range cfg.SchedulerConfigs {
				a := cfg.SchedulerConfigs[i]
				for j := range cfg.SchedulerConfigs {
					if i == j {
						continue
					}
					b := cfg.SchedulerConfigs[j]
					if a.CPUUtilizationMax < b.CPUUtilizationMin {
						g := gap{lo: a.CPUUtilizationMax, hi: b.CPUUtilizationMin}
						if chosen == nil || (g.hi-g.lo) > (chosen.hi-chosen.lo) {
							chosen = &g
						}
					}
				}
			}
			if chosen == nil {
				Skip("no gap between configured shard CPU ranges; cannot test unassigned-node behavior")
			}
			targetUtil := (chosen.lo + chosen.hi) / 2
			GinkgoWriter.Printf("Targeting CPU utilization %.2f in gap (%.2f, %.2f)\n",
				targetUtil, chosen.lo, chosen.hi)

			By("Selecting a worker node and pushing its CPU utilization into the gap")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1))
			targetNode := workerNodes[0]

			cpuCapacity := targetNode.Status.Capacity.Cpu().MilliValue()
			cpuRequestMillis := int64(float64(cpuCapacity) * targetUtil)
			Expect(cpuRequestMillis).To(BeNumerically(">", 0))

			stressPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-gap-util",
					Namespace: ctx.Namespace,
				},
				Spec: corev1.PodSpec{
					NodeName:      targetNode.Name,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "stress",
							Image:   "busybox",
							Command: []string{"sleep", "3600"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: *resource.NewMilliQuantity(cpuRequestMillis, resource.DecimalSI),
								},
							},
						},
					},
				},
			}
			_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), stressPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create gap-util pod")

			By(fmt.Sprintf("Verifying node %s is excluded from every shard", targetNode.Name))
			err = wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					for _, sc := range cfg.SchedulerConfigs {
						shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
						if err != nil {
							return false, nil
						}
						for _, n := range shard.Spec.NodesDesired {
							if n == targetNode.Name {
								GinkgoWriter.Printf("Node %s still in shard %q, waiting for eviction\n",
									targetNode.Name, sc.Name)
								return false, nil
							}
						}
					}
					return true, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("node %s with utilization %.2f should not be assigned to any shard (gap: %.2f-%.2f)",
					targetNode.Name, targetUtil, chosen.lo, chosen.hi))
		})
	})

	Describe("Node Reassignment on Workload Change", func() {
		It("node should move between shards when CPU utilization changes", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Identify low-util and high-util schedulers from the ConfigMap
			var lowUtilScheduler, highUtilScheduler *sharding.SchedulerConfigSpec
			for i := range cfg.SchedulerConfigs {
				sc := &cfg.SchedulerConfigs[i]
				if sc.CPUUtilizationMin == 0.0 && lowUtilScheduler == nil {
					lowUtilScheduler = sc
				}
				if sc.CPUUtilizationMax >= 1.0 && sc.CPUUtilizationMin > 0 && highUtilScheduler == nil {
					highUtilScheduler = sc
				}
			}
			Expect(lowUtilScheduler).NotTo(BeNil(), "need a low-utilization scheduler in config")
			Expect(highUtilScheduler).NotTo(BeNil(), "need a high-utilization scheduler in config")

			By("Getting a worker node")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1))
			targetNode := workerNodes[0]

			By(fmt.Sprintf("Verifying node %s starts in the low-util shard %q", targetNode.Name, lowUtilScheduler.Name))
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, err := e2eutil.GetNodeShard(ctx, lowUtilScheduler.Name)
					if err != nil {
						return false, nil
					}
					for _, n := range shard.Spec.NodesDesired {
						if n == targetNode.Name {
							return true, nil
						}
					}
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				"node should be in low-util shard initially")

			By("Creating CPU-intensive pods to push utilization into the high-util range")
			cpuCapacity := targetNode.Status.Capacity.Cpu().MilliValue()
			targetUtilization := (highUtilScheduler.CPUUtilizationMin + highUtilScheduler.CPUUtilizationMax) / 2
			cpuRequestMillis := int64(float64(cpuCapacity) * targetUtilization)

			stressPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cpu-stress-reassign",
					Namespace: ctx.Namespace,
					Labels:    map[string]string{"e2e-test": "sharding-reassign"},
				},
				Spec: corev1.PodSpec{
					NodeName:      targetNode.Name,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "stress",
							Image:   "busybox",
							Command: []string{"sleep", "3600"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: *resource.NewMilliQuantity(cpuRequestMillis, resource.DecimalSI),
								},
							},
						},
					},
				},
			}
			_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), stressPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create stress pod")

			By(fmt.Sprintf("Waiting for node %s to move to high-util shard %q", targetNode.Name, highUtilScheduler.Name))
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, err := e2eutil.GetNodeShard(ctx, highUtilScheduler.Name)
					if err != nil {
						return false, nil
					}
					for _, n := range shard.Spec.NodesDesired {
						if n == targetNode.Name {
							return true, nil
						}
					}
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				"node should move to high-util shard after CPU load increases")

			By("Deleting the stress pod to reduce CPU utilization")
			err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(
				context.TODO(), stressPod.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to delete stress pod")

			By(fmt.Sprintf("Waiting for node %s to return to low-util shard %q", targetNode.Name, lowUtilScheduler.Name))
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, err := e2eutil.GetNodeShard(ctx, lowUtilScheduler.Name)
					if err != nil {
						return false, nil
					}
					for _, n := range shard.Spec.NodesDesired {
						if n == targetNode.Name {
							return true, nil
						}
					}
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				"node should return to low-util shard after CPU load is removed")
		})
	})

	Describe("Mutual Exclusivity", func() {
		It("no node should appear in multiple shards simultaneously", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			By("Collecting NodesDesired from all shards")
			nodeToShard := make(map[string]string)
			for _, sc := range cfg.SchedulerConfigs {
				shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
				Expect(err).NotTo(HaveOccurred(), "failed to get NodeShard %s", sc.Name)
				for _, nodeName := range shard.Spec.NodesDesired {
					existingShard, duplicate := nodeToShard[nodeName]
					Expect(duplicate).To(BeFalse(),
						fmt.Sprintf("node %s is assigned to both %q and %q -- shards must be mutually exclusive",
							nodeName, existingShard, sc.Name))
					nodeToShard[nodeName] = sc.Name
				}
			}
			GinkgoWriter.Printf("Verified mutual exclusivity across %d shards, %d total node assignments\n",
				len(cfg.SchedulerConfigs), len(nodeToShard))
		})
	})

	Describe("MinNodes Constraint", func() {
		It("shards with eligible nodes should respect the configured MinNodes constraint", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Wait for the controller to complete at least one full sync cycle
			// so that node assignments are populated.
			time.Sleep(30 * time.Second)

			By("Checking MinNodes for each configured scheduler")
			for _, sc := range cfg.SchedulerConfigs {
				shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
				Expect(err).NotTo(HaveOccurred(), "failed to get NodeShard %s", sc.Name)

				nodeCount := len(shard.Spec.NodesDesired)
				GinkgoWriter.Printf("Shard %q: %d nodes desired (minNodes=%d, maxNodes=%d)\n",
					sc.Name, nodeCount, sc.MinNodes, sc.MaxNodes)

				// MinNodes is a best-effort constraint that can only be satisfied
				// when there are enough nodes whose CPU utilization falls within
				// the scheduler's configured range. In a fresh cluster all nodes
				// have ~0% CPU utilization, so a shard with cpuUtilizationMin > 0
				// may have zero eligible nodes. Only assert MinNodes when the
				// shard actually has some nodes assigned (i.e. eligible nodes exist).
				if nodeCount > 0 {
					Expect(nodeCount).To(BeNumerically(">=", sc.MinNodes),
						fmt.Sprintf("shard %q has eligible nodes but fewer than minNodes=%d (has %d)",
							sc.Name, sc.MinNodes, nodeCount))
				} else {
					GinkgoWriter.Printf("Shard %q has 0 nodes -- no eligible nodes in CPU range [%.2f, %.2f], skipping MinNodes check\n",
						sc.Name, sc.CPUUtilizationMin, sc.CPUUtilizationMax)
				}

				// MaxNodes should always be respected
				Expect(nodeCount).To(BeNumerically("<=", sc.MaxNodes),
					fmt.Sprintf("shard %q should have at most %d nodes (has %d)",
						sc.Name, sc.MaxNodes, nodeCount))
			}
		})
	})

	Describe("Warmup Node Preference", func() {
		It("warmup-labeled nodes should be prioritized for schedulers with preferWarmupNodes=true", func() {
			cfg := getShardingConfig()

			// Find a scheduler with preferWarmupNodes=true
			var warmupScheduler *sharding.SchedulerConfigSpec
			for i := range cfg.SchedulerConfigs {
				if cfg.SchedulerConfigs[i].PreferWarmupNodes {
					warmupScheduler = &cfg.SchedulerConfigs[i]
					break
				}
			}
			if warmupScheduler == nil {
				Skip("no scheduler with preferWarmupNodes=true in ConfigMap")
			}

			By("Labeling a worker node as a warmup node")
			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1))
			targetNode := workerNodes[0]

			// Label the node as warmup
			targetNode.Labels["node.volcano.sh/warmup"] = "true"
			_, err = ctx.Kubeclient.CoreV1().Nodes().Update(context.TODO(), &targetNode, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to label node as warmup")

			// Ensure label is cleaned up after the test
			defer func() {
				node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), targetNode.Name, metav1.GetOptions{})
				if err == nil {
					delete(node.Labels, "node.volcano.sh/warmup")
					_, _ = ctx.Kubeclient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
				}
			}()

			// The warmup node needs to be in the CPU utilization range of the warmup scheduler.
			// Create a pod to push its utilization into that range if needed.
			cpuCapacity := targetNode.Status.Capacity.Cpu().MilliValue()
			targetUtilization := (warmupScheduler.CPUUtilizationMin + warmupScheduler.CPUUtilizationMax) / 2
			cpuRequestMillis := int64(float64(cpuCapacity) * targetUtilization)

			if cpuRequestMillis > 0 {
				By(fmt.Sprintf("Creating pod to push warmup node %s into util range [%.0f%%, %.0f%%]",
					targetNode.Name, warmupScheduler.CPUUtilizationMin*100, warmupScheduler.CPUUtilizationMax*100))
				warmupPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "warmup-util-driver",
						Namespace: ctx.Namespace,
					},
					Spec: corev1.PodSpec{
						NodeName:      targetNode.Name,
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:    "driver",
								Image:   "busybox",
								Command: []string{"sleep", "3600"},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: *resource.NewMilliQuantity(cpuRequestMillis, resource.DecimalSI),
									},
								},
							},
						},
					},
				}
				_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), warmupPod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create warmup util driver pod")
			}

			waitForNodeShardsCreated(cfg)

			By(fmt.Sprintf("Waiting for warmup node %s to appear in shard %q", targetNode.Name, warmupScheduler.Name))
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, err := e2eutil.GetNodeShard(ctx, warmupScheduler.Name)
					if err != nil {
						return false, nil
					}
					for _, n := range shard.Spec.NodesDesired {
						if n == targetNode.Name {
							return true, nil
						}
					}
					return false, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("warmup node %s should be in shard %q (preferWarmupNodes=true)",
					targetNode.Name, warmupScheduler.Name))
		})
	})

	Describe("Node Stability", func() {
		It("node assignments should remain stable without significant cluster changes", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Use the first configured scheduler for stability check
			schedulerName := cfg.SchedulerConfigs[0].Name

			By(fmt.Sprintf("Recording initial shard assignments for %q", schedulerName))
			shard1, err := e2eutil.GetNodeShard(ctx, schedulerName)
			Expect(err).NotTo(HaveOccurred())
			initialNodes := make([]string, len(shard1.Spec.NodesDesired))
			copy(initialNodes, shard1.Spec.NodesDesired)

			By("Waiting for a sync period without making changes")
			time.Sleep(30 * time.Second)

			By("Verifying assignments remain stable")
			shard2, err := e2eutil.GetNodeShard(ctx, schedulerName)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Initial nodes: %v\n", initialNodes)
			GinkgoWriter.Printf("Current nodes: %v\n", shard2.Spec.NodesDesired)

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
		if !isControlPlane && !node.Spec.Unschedulable {
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
