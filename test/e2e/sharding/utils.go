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
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// Timeouts for various operations
	ShardCreationTimeout     = 5 * time.Minute
	ShardUpdateTimeout       = 3 * time.Minute
	ShardDeletionTimeout     = 2 * time.Minute
	NodeAssignmentTimeout    = 5 * time.Minute
	ControllerStartupTimeout = 2 * time.Minute

	// Default resource configurations
	DefaultNodeCPU    = "8"
	DefaultNodeMemory = "32Gi"

	// Labels
	WarmupNodeLabel = "node.volcano.sh/warmup"
)

// ShardTestContext holds common test context
type ShardTestContext struct {
	Namespace  string
	TestNodes  []string
	TestShards []string
}

// NewShardTestContext creates a new test context
func NewShardTestContext() *ShardTestContext {
	return &ShardTestContext{
		Namespace:  "default",
		TestNodes:  []string{},
		TestShards: []string{},
	}
}

// Cleanup removes all test resources
func (ctx *ShardTestContext) Cleanup() {
	// Delete test shards
	for _, shardName := range ctx.TestShards {
		DeleteShard(shardName)
	}

	// Delete test nodes
	for _, nodeName := range ctx.TestNodes {
		DeleteNode(nodeName)
	}

	// Wait for cleanup
	time.Sleep(2 * time.Second)
}

// CreateTestNode creates a test node with the specified configuration
func CreateTestNode(nodeName string, cpu string, memory string, isWarmup bool, extraLabels map[string]string) *corev1.Node {
	By(fmt.Sprintf("Creating test node: %s with CPU=%s, Memory=%s, Warmup=%v", nodeName, cpu, memory, isWarmup))

	labels := make(map[string]string)
	if isWarmup {
		labels[WarmupNodeLabel] = "true"
	}
	for k, v := range extraLabels {
		labels[k] = v
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			Labels: labels,
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
				corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalExponent),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
				corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalExponent),
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	createdNode, err := e2eutil.KubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "Failed to create node %s", nodeName)
	return createdNode
}

// CreateTestPod creates a test pod with resource requests on a specific node
func CreateTestPod(namespace, podName, nodeName, cpu, memory string) *corev1.Pod {
	By(fmt.Sprintf("Creating test pod: %s on node %s with CPU=%s, Memory=%s", podName, nodeName, cpu, memory))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "busybox",
					Command: []string{
						"sh",
						"-c",
						"sleep 3600",
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpu),
							corev1.ResourceMemory: resource.MustParse(memory),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpu),
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}

	createdPod, err := e2eutil.KubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "Failed to create pod %s", podName)
	return createdPod
}

// DeleteNode deletes a node
func DeleteNode(nodeName string) {
	By(fmt.Sprintf("Deleting node: %s", nodeName))
	err := e2eutil.KubeClient.CoreV1().Nodes().Delete(context.TODO(), nodeName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred(), "Failed to delete node %s", nodeName)
	}
}

// DeletePod deletes a pod
func DeletePod(namespace, podName string) {
	By(fmt.Sprintf("Deleting pod: %s/%s", namespace, podName))
	err := e2eutil.KubeClient.CoreV1().Pods(namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s/%s", namespace, podName)
	}
}

// DeleteShard deletes a NodeShard
func DeleteShard(shardName string) {
	By(fmt.Sprintf("Deleting shard: %s", shardName))
	err := e2eutil.VcClient.ShardV1alpha1().NodeShards().Delete(context.TODO(), shardName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred(), "Failed to delete shard %s", shardName)
	}
}

// GetShard retrieves a NodeShard
func GetShard(shardName string) (*shardv1alpha1.NodeShard, error) {
	return e2eutil.VcClient.ShardV1alpha1().NodeShards().Get(context.TODO(), shardName, metav1.GetOptions{})
}

// ListShards lists all NodeShards
func ListShards() (*shardv1alpha1.NodeShardList, error) {
	return e2eutil.VcClient.ShardV1alpha1().NodeShards().List(context.TODO(), metav1.ListOptions{})
}

// WaitForShardCreated waits for a shard to be created
func WaitForShardCreated(shardName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		shard, err := GetShard(shardName)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return shard != nil, nil
	})
}

// WaitForShardDeleted waits for a shard to be deleted
func WaitForShardDeleted(shardName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := GetShard(shardName)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitForShardNodesDesired waits for a shard to have the expected nodes in NodesDesired
func WaitForShardNodesDesired(shardName string, expectedNodes []string, timeout time.Duration) error {
	By(fmt.Sprintf("Waiting for shard %s to have expected nodes: %v", shardName, expectedNodes))

	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		shard, err := GetShard(shardName)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		// Sort both slices for comparison
		actualNodes := make([]string, len(shard.Spec.NodesDesired))
		copy(actualNodes, shard.Spec.NodesDesired)
		sort.Strings(actualNodes)

		expectedNodesSorted := make([]string, len(expectedNodes))
		copy(expectedNodesSorted, expectedNodes)
		sort.Strings(expectedNodesSorted)

		// Check if nodes match
		if len(actualNodes) != len(expectedNodesSorted) {
			return false, nil
		}

		for i := range actualNodes {
			if actualNodes[i] != expectedNodesSorted[i] {
				return false, nil
			}
		}

		return true, nil
	})
}

// WaitForShardNodeCount waits for a shard to have the expected number of nodes
func WaitForShardNodeCount(shardName string, expectedCount int, timeout time.Duration) error {
	By(fmt.Sprintf("Waiting for shard %s to have %d nodes", shardName, expectedCount))

	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		shard, err := GetShard(shardName)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		return len(shard.Spec.NodesDesired) == expectedCount, nil
	})
}

// WaitForShardContainsNode waits for a shard to contain a specific node
func WaitForShardContainsNode(shardName string, nodeName string, timeout time.Duration) error {
	By(fmt.Sprintf("Waiting for shard %s to contain node %s", shardName, nodeName))

	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		shard, err := GetShard(shardName)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		for _, n := range shard.Spec.NodesDesired {
			if n == nodeName {
				return true, nil
			}
		}
		return false, nil
	})
}

// WaitForShardNotContainsNode waits for a shard to not contain a specific node
func WaitForShardNotContainsNode(shardName string, nodeName string, timeout time.Duration) error {
	By(fmt.Sprintf("Waiting for shard %s to not contain node %s", shardName, nodeName))

	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		shard, err := GetShard(shardName)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}

		for _, n := range shard.Spec.NodesDesired {
			if n == nodeName {
				return false, nil
			}
		}
		return true, nil
	})
}

// UpdateNodeLabels updates node labels
func UpdateNodeLabels(nodeName string, labels map[string]string) error {
	node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	for k, v := range labels {
		node.Labels[k] = v
	}

	_, err = e2eutil.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

// RemoveNodeLabels removes node labels
func RemoveNodeLabels(nodeName string, labelKeys []string) error {
	node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if node.Labels == nil {
		return nil
	}

	for _, key := range labelKeys {
		delete(node.Labels, key)
	}

	_, err = e2eutil.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

// TaintNode adds a taint to a node
func TaintNode(nodeName string, taint corev1.Taint) error {
	node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	node.Spec.Taints = append(node.Spec.Taints, taint)
	_, err = e2eutil.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

// RemoveNodeTaint removes a taint from a node
func RemoveNodeTaint(nodeName string, taintKey string) error {
	node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	newTaints := []corev1.Taint{}
	for _, t := range node.Spec.Taints {
		if t.Key != taintKey {
			newTaints = append(newTaints, t)
		}
	}

	node.Spec.Taints = newTaints
	_, err = e2eutil.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

// ListNodes lists all nodes
func ListNodes() (*corev1.NodeList, error) {
	return e2eutil.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
}

// ListNodesWithLabel lists nodes with a specific label selector
func ListNodesWithLabel(selector string) (*corev1.NodeList, error) {
	labelSelector, err := labels.Parse(selector)
	if err != nil {
		return nil, err
	}
	return e2eutil.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
}

// CountNodesInShard counts the number of nodes in a shard
func CountNodesInShard(shardName string) (int, error) {
	shard, err := GetShard(shardName)
	if err != nil {
		return 0, err
	}
	return len(shard.Spec.NodesDesired), nil
}

// GetShardNodeNames returns the node names in a shard
func GetShardNodeNames(shardName string) ([]string, error) {
	shard, err := GetShard(shardName)
	if err != nil {
		return nil, err
	}
	return shard.Spec.NodesDesired, nil
}

// VerifyShardNodeDistribution verifies that nodes are distributed across shards
func VerifyShardNodeDistribution(shardNames []string, minNodesPerShard int) error {
	for _, shardName := range shardNames {
		count, err := CountNodesInShard(shardName)
		if err != nil {
			return fmt.Errorf("failed to count nodes in shard %s: %v", shardName, err)
		}
		if count < minNodesPerShard {
			return fmt.Errorf("shard %s has %d nodes, expected at least %d", shardName, count, minNodesPerShard)
		}
	}
	return nil
}

// CalculateNodeCPUUtilization calculates the CPU utilization of a node based on pod requests
func CalculateNodeCPUUtilization(nodeName string) (float64, error) {
	node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}

	pods, err := e2eutil.KubeClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return 0, err
	}

	cpuCapacity := node.Status.Allocatable.Cpu().MilliValue()
	var cpuRequested int64 = 0

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			cpuRequested += container.Resources.Requests.Cpu().MilliValue()
		}
	}

	if cpuCapacity == 0 {
		return 0, nil
	}

	return float64(cpuRequested) / float64(cpuCapacity), nil
}

// VerifyNodesStableInShard verifies that a minimum percentage of nodes remain in a shard after update
func VerifyNodesStableInShard(shardName string, oldNodes []string, minStabilityPercent float64) error {
	newNodes, err := GetShardNodeNames(shardName)
	if err != nil {
		return err
	}

	// Count how many old nodes are still in new assignment
	stableCount := 0
	for _, oldNode := range oldNodes {
		for _, newNode := range newNodes {
			if oldNode == newNode {
				stableCount++
				break
			}
		}
	}

	if len(oldNodes) == 0 {
		return nil
	}

	stabilityPercent := float64(stableCount) / float64(len(oldNodes))
	if stabilityPercent < minStabilityPercent {
		return fmt.Errorf("only %.2f%% of nodes remained stable (expected at least %.2f%%)",
			stabilityPercent*100, minStabilityPercent*100)
	}

	return nil
}
