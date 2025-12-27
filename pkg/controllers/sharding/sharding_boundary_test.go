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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestBoundaryUtilization tests boundary utilization conditions
func TestBoundaryUtilization(t *testing.T) {
	// Setup controller with boundary configs
	opt := &TestControllerOption{
		SchedulerConfigs: []SchedulerConfigSpec{
			{
				Name:              "below-50",
				Type:              "volcano",
				CPUUtilizationMin: 0.0,
				CPUUtilizationMax: 0.49,
				PreferWarmupNodes: false,
				MinNodes:          1,
				MaxNodes:          10,
			},
			{
				Name:              "above-50",
				Type:              "agent",
				CPUUtilizationMin: 0.5,
				CPUUtilizationMax: 1.0,
				PreferWarmupNodes: false,
				MinNodes:          1,
				MaxNodes:          10,
			},
		},
		ShardSyncPeriod: 2 * time.Second,
	}

	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)

	controller := testCtrl.Controller

	// Create boundary nodes
	boundaryNodes := []runtime.Object{
		CreateTestNode("node-exactly-0.5", "8", false, nil),    // Exactly 50%
		CreateTestNode("node-just-below-0.5", "8", false, nil), // Just below 50%
		CreateTestNode("node-just-above-0.5", "8", false, nil), // Just above 50%
	}

	for _, node := range boundaryNodes {
		_, err := controller.kubeClient.CoreV1().Nodes().Create(context.TODO(), node.(*corev1.Node), metav1.CreateOptions{})
		assert.NoError(t, err, "should create boundary node")
	}

	// Create pods for each node
	// node-exactly-0.5: 4000m (exactly 50%)
	exactlyPods := []*corev1.Pod{
		CreateTestPod("default", "exactly-pod", "node-exactly-0.5", "4000m", "4Gi"),
	}

	// node-just-below-0.5: 3900m (~48.75%)
	belowPods := []*corev1.Pod{
		CreateTestPod("default", "below-pod", "node-just-below-0.5", "3900m", "4Gi"),
	}

	// node-just-above-0.5: 4100m (~51.25%)
	abovePods := []*corev1.Pod{
		CreateTestPod("default", "above-pod", "node-just-above-0.5", "4100m", "4Gi"),
	}

	// Create all pods
	allPods := append(append(exactlyPods, belowPods...), abovePods...)
	for _, pod := range allPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create boundary pod")
	}

	// Wait for cache sync
	time.Sleep(500 * time.Millisecond)

	// Trigger events
	for _, pod := range allPods {
		controller.addPod(pod)
	}

	// Wait for metrics update
	WaitForNodeMetricsUpdate(t, controller, "node-exactly-0.5", 2*time.Second)
	WaitForNodeMetricsUpdate(t, controller, "node-just-below-0.5", 2*time.Second)
	WaitForNodeMetricsUpdate(t, controller, "node-just-above-0.5", 2*time.Second)

	// Verify metrics
	controller.metricsMutex.RLock()
	exactlyMetrics := controller.GetNodeMetrics("node-exactly-0.5")
	controller.metricsMutex.RUnlock()
	assert.NotNil(t, exactlyMetrics)
	assert.InDelta(t, 0.5, exactlyMetrics.CPUUtilization, 0.01, "exactly 50% node")

	controller.metricsMutex.RLock()
	belowMetrics := controller.GetNodeMetrics("node-just-below-0.5")
	controller.metricsMutex.RUnlock()
	assert.NotNil(t, belowMetrics)
	assert.True(t, belowMetrics.CPUUtilization < 0.5, "below 50% node")

	controller.metricsMutex.RLock()
	aboveMetrics := controller.GetNodeMetrics("node-just-above-0.5")
	controller.metricsMutex.RUnlock()
	assert.NotNil(t, aboveMetrics)
	assert.True(t, aboveMetrics.CPUUtilization > 0.5, "above 50% node")

	// Sync shards
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify assignments
	// node-exactly-0.5 should go to above-50 (agent)
	// node-just-below-0.5 should go to below-50 (volcano)
	// node-just-above-0.5 should go to above-50 (agent)
	VerifyAssignment(t, controller, "above-50", []string{"node-exactly-0.5", "node-just-above-0.5"})
	VerifyAssignment(t, controller, "below-50", []string{"node-just-below-0.5"})
}

// TestEmptyCluster tests empty cluster handling
func TestEmptyCluster(t *testing.T) {
	// Setup controller with no initial objects
	opt := &TestControllerOption{
		InitialObjects:   []runtime.Object{},
		SchedulerConfigs: getDefaultTestSchedulerConfigs(),
		ShardSyncPeriod:  2 * time.Second,
	}

	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)

	controller := testCtrl.Controller

	// Initial sync
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify no shards created
	shards, err := controller.vcClient.ShardV1alpha1().NodeShards().List(context.TODO(), metav1.ListOptions{})
	assert.NoError(t, err, "should list shards")
	assert.Equal(t, 2, len(shards.Items), "should have no shards initially")

	// Add a node and pods
	testNode := CreateTestNode("empty-node", "8", false, nil)
	_, err = controller.kubeClient.CoreV1().Nodes().Create(context.TODO(), testNode, metav1.CreateOptions{})
	assert.NoError(t, err, "should create node")

	testPod := CreateTestPod("default", "empty-pod", "empty-node", "4000m", "4Gi")
	_, err = controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), testPod, metav1.CreateOptions{})
	assert.NoError(t, err, "should create pod")

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger events
	controller.addNodeEvent(testNode)
	controller.addPod(testPod)

	// Sync
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify shard created
	VerifyAssignment(t, controller, "agent-scheduler", []string{}) // 50% -> agent
	VerifyAssignment(t, controller, "volcano-scheduler", []string{"empty-node"})
}

// TestMaxUtilization tests max utilization handling
func TestMaxUtilization(t *testing.T) {
	// Setup controller
	opt := &TestControllerOption{
		ShardSyncPeriod:  2 * time.Second,
		SchedulerConfigs: getDefaultTestSchedulerConfigs(),
	}

	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)

	controller := testCtrl.Controller

	// Create overutilized node
	overNode := CreateTestNode("over-node", "8", false, nil)
	_, err := controller.kubeClient.CoreV1().Nodes().Create(context.TODO(), overNode, metav1.CreateOptions{})
	assert.NoError(t, err, "should create overutilized node")

	// Create pods that exceed node capacity (10000m > 8000m)
	overPods := []*corev1.Pod{
		CreateTestPod("default", "over-pod-1", "over-node", "5000m", "5Gi"),
		CreateTestPod("default", "over-pod-2", "over-node", "5000m", "5Gi"),
	}

	for _, pod := range overPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create overutilized pod")
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger events
	controller.addNodeEvent(overNode)
	for _, pod := range overPods {
		controller.addPod(pod)
	}

	// Wait for metrics update
	WaitForNodeMetricsUpdate(t, controller, "over-node", 2*time.Second)

	// Verify metrics - should be capped at 1.0
	controller.metricsMutex.RLock()
	metrics := controller.GetNodeMetrics("over-node")
	controller.metricsMutex.RUnlock()
	assert.NotNil(t, metrics)
	assert.Equal(t, 1.0, metrics.CPUUtilization, "CPU utilization should be capped at 1.0")

	// Sync
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify assignment - should go to agent scheduler
	VerifyAssignment(t, controller, "agent-scheduler", []string{"over-node"})
	VerifyAssignment(t, controller, "volcano-scheduler", []string{})
}
