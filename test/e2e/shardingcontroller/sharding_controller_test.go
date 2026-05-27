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
	"slices"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/yaml"

	"volcano.sh/volcano/pkg/controllers/sharding"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// Sharding ConfigMap coordinates (must match helm release name and namespace)
	shardingConfigMapName      = "integration-sharding-configmap"
	shardingConfigMapNamespace = "volcano-system"

	pollInterval         = 500 * time.Millisecond
	shardCreationTimeout = 3 * time.Minute
	reassignmentTimeout  = 3 * time.Minute
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

	getShardingConfig := func() *sharding.ShardingConfig {
		cfg, err := e2eutil.GetShardingConfigFromConfigMap(ctx, shardingConfigMapName, shardingConfigMapNamespace)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to read sharding config from ConfigMap")
		return cfg
	}

	waitForNodeShardsCreated := func(cfg *sharding.ShardingConfig) {
		By("Waiting for ShardingController to create NodeShards")
		expected := make(map[string]struct{}, len(cfg.SchedulerConfigs))
		for _, sc := range cfg.SchedulerConfigs {
			expected[sc.Name] = struct{}{}
		}
		err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, shardCreationTimeout, true,
			func(c context.Context) (bool, error) {
				shards, err := e2eutil.ListNodeShards(ctx)
				if err != nil {
					return false, nil
				}
				found := 0
				for _, shard := range shards.Items {
					if _, ok := expected[shard.Name]; ok {
						found++
					}
				}
				return found == len(expected), nil
			})
		Expect(err).NotTo(HaveOccurred(), "NodeShards should be created for all configured schedulers")
	}

	findScheduler := func(cfg *sharding.ShardingConfig, pred func(*sharding.SchedulerConfigSpec) bool) *sharding.SchedulerConfigSpec {
		for i := range cfg.SchedulerConfigs {
			if pred(&cfg.SchedulerConfigs[i]) {
				return &cfg.SchedulerConfigs[i]
			}
		}
		return nil
	}

	isLowUtil := func(s *sharding.SchedulerConfigSpec) bool {
		return s.CPUUtilizationMin == 0.0
	}
	isHighUtil := func(s *sharding.SchedulerConfigSpec) bool {
		return s.CPUUtilizationMax >= 1.0 && s.CPUUtilizationMin > 0
	}

	Describe("Shard Creation", func() {
		It("should create NodeShards matching the ConfigMap configuration", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			shards, err := e2eutil.ListNodeShards(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to list NodeShards")
			Expect(len(shards.Items)).To(BeNumerically(">=", len(cfg.SchedulerConfigs)),
				"at least as many NodeShards as configured schedulers should exist")

			shardNames := make(map[string]struct{}, len(shards.Items))
			for _, shard := range shards.Items {
				shardNames[shard.Name] = struct{}{}
			}
			for _, sc := range cfg.SchedulerConfigs {
				Expect(shardNames).To(HaveKey(sc.Name),
					fmt.Sprintf("NodeShard for scheduler %q should exist", sc.Name))
			}
		})

		It("every worker node should be assigned to exactly one configured shard after controller startup", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to list nodes")
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1),
				"cluster should have at least 1 worker node")

			workerSet := make(map[string]struct{}, len(workerNodes))
			for _, n := range workerNodes {
				workerSet[n.Name] = struct{}{}
			}

			By("Waiting for every worker node to land in some configured shard")
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					nodeToShard := make(map[string]string)
					for _, sc := range cfg.SchedulerConfigs {
						shard, gerr := e2eutil.GetNodeShard(ctx, sc.Name)
						if gerr != nil {
							return false, nil
						}
						for _, n := range shard.Spec.NodesDesired {
							// Mutual exclusivity: a node must not appear in two shards.
							if existing, dup := nodeToShard[n]; dup {
								return false, fmt.Errorf("node %s assigned to both %q and %q", n, existing, sc.Name)
							}
							nodeToShard[n] = sc.Name
						}
					}
					for w := range workerSet {
						if _, ok := nodeToShard[w]; !ok {
							return false, nil
						}
					}
					return true, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				"every worker node should be assigned to exactly one configured shard")
		})

		It("should recreate a NodeShard after it is manually deleted", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			schedulerName := cfg.SchedulerConfigs[0].Name
			Expect(e2eutil.DeleteNodeShard(ctx, schedulerName)).To(Succeed())

			err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					_, err := e2eutil.GetNodeShard(ctx, schedulerName)
					return err == nil, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("NodeShard %q should be recreated after deletion", schedulerName))
		})
	})

	Describe("CPU-Based Node Assignment and Reassignment", func() {
		It("node should move from low-util shard to high-util shard when load increases, and back when load is removed", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			lowUtil := findScheduler(cfg, isLowUtil)
			highUtil := findScheduler(cfg, isHighUtil)
			Expect(lowUtil).NotTo(BeNil(), "ConfigMap should have a low-utilization scheduler (cpuUtilizationMin=0)")
			Expect(highUtil).NotTo(BeNil(), "ConfigMap should have a high-utilization scheduler (cpuUtilizationMax>=1.0, min>0)")

			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1))
			targetNode := workerNodes[0]

			By(fmt.Sprintf("Verifying node %s starts in low-util shard %q (fresh cluster, no load)", targetNode.Name, lowUtil.Name))
			Expect(waitNodeInShard(ctx, lowUtil.Name, targetNode.Name)).To(Succeed(),
				"node should start in low-util shard")

			cpuCapacity := targetNode.Status.Capacity.Cpu().MilliValue()
			targetUtil := (highUtil.CPUUtilizationMin + highUtil.CPUUtilizationMax) / 2
			cpuRequestMillis := int64(float64(cpuCapacity) * targetUtil)
			Expect(cpuRequestMillis).To(BeNumerically(">", 0))

			By(fmt.Sprintf("Creating CPU stress pod on %s requesting %dm (target util %.0f%% of %dm)",
				targetNode.Name, cpuRequestMillis, targetUtil*100, cpuCapacity))
			stressPod := newStressPod("cpu-stress-reassign", ctx.Namespace, targetNode.Name, cpuRequestMillis)
			_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), stressPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create stress pod")

			By(fmt.Sprintf("Waiting for node %s to enter high-util shard %q AND leave low-util shard %q",
				targetNode.Name, highUtil.Name, lowUtil.Name))
			Expect(waitNodeInShard(ctx, highUtil.Name, targetNode.Name)).To(Succeed(),
				"node should move to high-util shard after load")
			Expect(waitNodeNotInShard(ctx, lowUtil.Name, targetNode.Name)).To(Succeed(),
				"node should leave low-util shard after load (mutual exclusivity)")

			By("Deleting stress pod to remove CPU load")
			Expect(ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(
				context.TODO(), stressPod.Name, metav1.DeleteOptions{})).To(Succeed())

			By(fmt.Sprintf("Waiting for node %s to return to low-util shard %q AND leave high-util shard %q",
				targetNode.Name, lowUtil.Name, highUtil.Name))
			Expect(waitNodeInShard(ctx, lowUtil.Name, targetNode.Name)).To(Succeed(),
				"node should return to low-util shard after load removed")
			Expect(waitNodeNotInShard(ctx, highUtil.Name, targetNode.Name)).To(Succeed(),
				"node should leave high-util shard after load removed")
		})

		It("node with CPU utilization in the gap between shard ranges should not be assigned to any shard", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			// Pick the largest gap between configured CPU ranges; a util in this gap matches no shard.
			type gap struct{ lo, hi float64 }
			var chosen *gap
			for i := range cfg.SchedulerConfigs {
				for j := range cfg.SchedulerConfigs {
					if i == j {
						continue
					}
					a, b := cfg.SchedulerConfigs[i], cfg.SchedulerConfigs[j]
					if a.CPUUtilizationMax < b.CPUUtilizationMin {
						g := gap{lo: a.CPUUtilizationMax, hi: b.CPUUtilizationMin}
						if chosen == nil || (g.hi-g.lo) > (chosen.hi-chosen.lo) {
							chosen = &g
						}
					}
				}
			}
			if chosen == nil {
				Skip("no gap between configured shard CPU ranges")
			}
			targetUtil := (chosen.lo + chosen.hi) / 2

			nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			workerNodes := filterWorkerNodes(nodes.Items)
			Expect(len(workerNodes)).To(BeNumerically(">=", 1))
			targetNode := workerNodes[0]

			cpuCapacity := targetNode.Status.Capacity.Cpu().MilliValue()
			cpuRequestMillis := int64(float64(cpuCapacity) * targetUtil)
			Expect(cpuRequestMillis).To(BeNumerically(">", 0))

			By(fmt.Sprintf("Loading node %s into gap (%.2f, %.2f)", targetNode.Name, chosen.lo, chosen.hi))
			gapPod := newStressPod("cpu-gap-util", ctx.Namespace, targetNode.Name, cpuRequestMillis)
			_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), gapPod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create gap-util pod")

			Expect(waitNodeNotInAnyShard(ctx, cfg, targetNode.Name)).To(Succeed(),
				fmt.Sprintf("node %s with util %.2f should not appear in any shard (gap %.2f-%.2f)",
					targetNode.Name, targetUtil, chosen.lo, chosen.hi))
		})
	})

	Describe("Mutual Exclusivity", func() {
		It("no node should appear in multiple shards simultaneously", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			nodeToShard := make(map[string]string)
			for _, sc := range cfg.SchedulerConfigs {
				shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
				Expect(err).NotTo(HaveOccurred(), "failed to get NodeShard %s", sc.Name)
				for _, nodeName := range shard.Spec.NodesDesired {
					existing, dup := nodeToShard[nodeName]
					Expect(dup).To(BeFalse(),
						fmt.Sprintf("node %s assigned to both %q and %q", nodeName, existing, sc.Name))
					nodeToShard[nodeName] = sc.Name
				}
			}
		})
	})

	Describe("MinNodes / MaxNodes Constraints", func() {
		It("shards with eligible nodes should respect MinNodes and MaxNodes", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)
			time.Sleep(syncPeriod(cfg))

			for _, sc := range cfg.SchedulerConfigs {
				shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
				Expect(err).NotTo(HaveOccurred(), "failed to get NodeShard %s", sc.Name)
				count := len(shard.Spec.NodesDesired)

				// MinNodes is best-effort: only enforce when the shard has any eligible node.
				if count > 0 {
					Expect(count).To(BeNumerically(">=", sc.MinNodes),
						fmt.Sprintf("shard %q has %d nodes < MinNodes=%d", sc.Name, count, sc.MinNodes))
				}
				Expect(count).To(BeNumerically("<=", sc.MaxNodes),
					fmt.Sprintf("shard %q has %d nodes > MaxNodes=%d", sc.Name, count, sc.MaxNodes))
			}
		})
	})

	Describe("Node Lifecycle", func() {
		It("newly created node should be added to a shard, and removed from shards when deleted", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)
			lowUtil := findScheduler(cfg, isLowUtil)
			Expect(lowUtil).NotTo(BeNil())

			fakeName := "shard-e2e-fake-node"
			// Best-effort cleanup of leftover from a prior failed run.
			_ = ctx.Kubeclient.CoreV1().Nodes().Delete(context.TODO(), fakeName, metav1.DeleteOptions{})

			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   fakeName,
					Labels: map[string]string{"e2e.volcano.sh/fake-node": "true"},
				},
				Spec: corev1.NodeSpec{
					// Taint so real workloads do not land on a node without a kubelet.
					Taints: []corev1.Taint{{
						Key:    "e2e.volcano.sh/fake-node",
						Effect: corev1.TaintEffectNoSchedule,
					}},
				},
			}

			By(fmt.Sprintf("Creating fake Node %s to simulate a new node joining the cluster", fakeName))
			_, err := ctx.Kubeclient.CoreV1().Nodes().Create(context.TODO(), fakeNode, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create fake Node")
			defer func() {
				_ = ctx.Kubeclient.CoreV1().Nodes().Delete(context.TODO(), fakeName, metav1.DeleteOptions{})
			}()

			// Set capacity/allocatable via /status subresource. Retry on conflict because
			// the node controller may modify the Node concurrently after creation.
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				latest, gerr := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), fakeName, metav1.GetOptions{})
				if gerr != nil {
					return gerr
				}
				latest.Status = corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
						corev1.ResourcePods:   resource.MustParse("110"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
						corev1.ResourcePods:   resource.MustParse("110"),
					},
					Conditions: []corev1.NodeCondition{{
						Type:               corev1.NodeReady,
						Status:             corev1.ConditionTrue,
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
					}},
				}
				_, uerr := ctx.Kubeclient.CoreV1().Nodes().UpdateStatus(context.TODO(), latest, metav1.UpdateOptions{})
				return uerr
			})
			Expect(err).NotTo(HaveOccurred(), "failed to set fake Node status")

			By(fmt.Sprintf("Waiting for fake node %s to be added to low-util shard %q", fakeName, lowUtil.Name))
			Expect(waitNodeInShard(ctx, lowUtil.Name, fakeName)).To(Succeed(),
				"newly created node with 0 CPU utilization should join the low-util shard")

			By(fmt.Sprintf("Deleting fake Node %s to simulate node removal", fakeName))
			Expect(ctx.Kubeclient.CoreV1().Nodes().Delete(
				context.TODO(), fakeName, metav1.DeleteOptions{})).To(Succeed())

			Expect(waitNodeNotInAnyShard(ctx, cfg, fakeName)).To(Succeed(),
				"deleted node should be removed from every shard")
		})
	})

	Describe("ConfigMap Update", func() {
		It("changes to sharding ConfigMap should be reflected in shard parameters", func() {
			origCM, err := ctx.Kubeclient.CoreV1().ConfigMaps(shardingConfigMapNamespace).Get(
				context.TODO(), shardingConfigMapName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get sharding ConfigMap")
			origData := origCM.Data[sharding.ConfigMapDataKey]

			cfg, err := sharding.ParseShardingConfig([]byte(origData))
			Expect(err).NotTo(HaveOccurred())
			waitForNodeShardsCreated(cfg)

			// Pick the low-util scheduler and shrink MaxNodes to 1; controller should observe the limit.
			lowUtil := findScheduler(cfg, isLowUtil)
			Expect(lowUtil).NotTo(BeNil())
			targetSchedulerName := lowUtil.Name

			newCfg := *cfg
			newCfg.SchedulerConfigs = append([]sharding.SchedulerConfigSpec(nil), cfg.SchedulerConfigs...)
			for i := range newCfg.SchedulerConfigs {
				if newCfg.SchedulerConfigs[i].Name == targetSchedulerName {
					newCfg.SchedulerConfigs[i].MinNodes = 1
					newCfg.SchedulerConfigs[i].MaxNodes = 1
				}
			}
			newYAML, err := yaml.Marshal(&newCfg)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("Updating ConfigMap to set %q maxNodes=1", targetSchedulerName))
			origCM.Data[sharding.ConfigMapDataKey] = string(newYAML)
			_, err = ctx.Kubeclient.CoreV1().ConfigMaps(shardingConfigMapNamespace).Update(
				context.TODO(), origCM, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to update ConfigMap")
			defer func() {
				cm, gerr := ctx.Kubeclient.CoreV1().ConfigMaps(shardingConfigMapNamespace).Get(
					context.TODO(), shardingConfigMapName, metav1.GetOptions{})
				if gerr != nil {
					return
				}
				cm.Data[sharding.ConfigMapDataKey] = origData
				_, _ = ctx.Kubeclient.CoreV1().ConfigMaps(shardingConfigMapNamespace).Update(
					context.TODO(), cm, metav1.UpdateOptions{})
			}()

			By(fmt.Sprintf("Waiting for shard %q to respect new MaxNodes=1", targetSchedulerName))
			err = wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
				func(c context.Context) (bool, error) {
					shard, gerr := e2eutil.GetNodeShard(ctx, targetSchedulerName)
					if gerr != nil {
						return false, nil
					}
					return len(shard.Spec.NodesDesired) <= 1, nil
				})
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("shard %q should reflect updated MaxNodes=1 from ConfigMap", targetSchedulerName))
		})
	})

	Describe("Node Stability", func() {
		It("NodesDesired should be identical across sync cycles without cluster changes", func() {
			cfg := getShardingConfig()
			waitForNodeShardsCreated(cfg)

			schedulerName := cfg.SchedulerConfigs[0].Name
			shardBefore, err := e2eutil.GetNodeShard(ctx, schedulerName)
			Expect(err).NotTo(HaveOccurred())
			before := append([]string(nil), shardBefore.Spec.NodesDesired...)
			sort.Strings(before)

			waitDuration := 2 * syncPeriod(cfg)
			By(fmt.Sprintf("Sleeping %s (>= 2x syncPeriod) without cluster changes", waitDuration))
			time.Sleep(waitDuration)

			shardAfter, err := e2eutil.GetNodeShard(ctx, schedulerName)
			Expect(err).NotTo(HaveOccurred())
			after := append([]string(nil), shardAfter.Spec.NodesDesired...)
			sort.Strings(after)

			Expect(after).To(Equal(before),
				"NodesDesired must be identical before and after sync without cluster changes")
		})
	})
})

// Helpers

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

func newStressPod(name, ns, nodeName string, cpuMillis int64) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "stress",
				Image:   "busybox",
				Command: []string{"sleep", "3600"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: *resource.NewMilliQuantity(cpuMillis, resource.DecimalSI),
					},
				},
			}},
		},
	}
}

func waitNodeInShard(ctx *e2eutil.TestContext, shardName, nodeName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
		func(c context.Context) (bool, error) {
			shard, err := e2eutil.GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			return slices.Contains(shard.Spec.NodesDesired, nodeName), nil
		})
}

func waitNodeNotInShard(ctx *e2eutil.TestContext, shardName, nodeName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
		func(c context.Context) (bool, error) {
			shard, err := e2eutil.GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			return !slices.Contains(shard.Spec.NodesDesired, nodeName), nil
		})
}

func waitNodeNotInAnyShard(ctx *e2eutil.TestContext, cfg *sharding.ShardingConfig, nodeName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), pollInterval, reassignmentTimeout, true,
		func(c context.Context) (bool, error) {
			for _, sc := range cfg.SchedulerConfigs {
				shard, err := e2eutil.GetNodeShard(ctx, sc.Name)
				if err != nil {
					return false, nil
				}
				if slices.Contains(shard.Spec.NodesDesired, nodeName) {
					return false, nil
				}
			}
			return true, nil
		})
}

// syncPeriod returns the configured ShardSyncPeriod or a 60s default.
func syncPeriod(cfg *sharding.ShardingConfig) time.Duration {
	if cfg.ShardSyncPeriod == "" {
		return 60 * time.Second
	}
	d, err := time.ParseDuration(cfg.ShardSyncPeriod)
	if err != nil {
		return 60 * time.Second
	}
	return d
}
