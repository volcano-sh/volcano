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
	"k8s.io/klog/v2"
)

// TestControllerInitialization tests controller initialization
func TestControllerInitialization(t *testing.T) {
	// Setup with minimal objects
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	klog.Info("Controller initialization test passed")
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	klog.Info("Controller initialization test passed")
	controller := testCtrl.Controller

	// Verify controller state
	assert.NotNil(t, controller.kubeClient, "kubeClient should be initialized")
	assert.NotNil(t, controller.vcClient, "vcClient should be initialized")
	assert.NotNil(t, controller.nodeLister, "nodeLister should be initialized")
	assert.NotNil(t, controller.podLister, "podLister should be initialized")
	assert.NotNil(t, controller.shardLister, "shardLister should be initialized")
	assert.NotNil(t, controller.nodeShardQueue, "queue should be initialized")
	assert.NotNil(t, controller.nodeEventQueue, "nodeEventQueue should be initialized")
	assert.NotNil(t, controller.shardingManager, "shardingManager should be initialized")
	assert.NotNil(t, controller.nodeMetricsCache, "nodeMetricsCache should be initialized")
	assert.Equal(t, 2, len(controller.schedulerConfigs), "should have default scheduler configs")

	// Verify node metrics cache
	controller.metricsMutex.RLock()
	metrics := controller.GetNodeMetrics("node-1")
	controller.metricsMutex.RUnlock()
	assert.NotNil(t, metrics, "node metrics should be calculated")
	assert.Equal(t, "node-1", metrics.NodeName, "node name should match")
	assert.False(t, metrics.LastUpdated.IsZero(), "last updated should be set")
	klog.Info("Controller initialization test passed")
}

// TestNodeMetricsCalculation tests node metrics calculation
func TestNodeMetricsCalculation(t *testing.T) {
	// Create test node and pods
	testNode := CreateTestNode("test-node", "8", true, nil)
	testPods := []*corev1.Pod{
		CreateTestPod("default", "pod-1", "test-node", "1000m", "1Gi"),
		CreateTestPod("default", "pod-2", "test-node", "2000m", "2Gi"),
		CreateTestPod("default", "pod-3", "test-node", "500m", "512Mi"),
	}

	// Setup controller
	opt := &TestControllerOption{
		InitialObjects:  []runtime.Object{testNode},
		ShardSyncPeriod: 2 * time.Second,
	}

	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)

	controller := testCtrl.Controller

	// Create pods
	for _, pod := range testPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create pod")
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger metrics update via pod events
	for _, pod := range testPods {
		controller.addPod(pod)
	}

	// Wait for metrics to be updated
	WaitForNodeMetricsUpdate(t, controller, "test-node", 2*time.Second)

	// Get and verify metrics
	controller.metricsMutex.RLock()
	metrics := controller.GetNodeMetrics("test-node")
	controller.metricsMutex.RUnlock()
	assert.NotNil(t, metrics, "metrics should be calculated")

	// CPU: (1000 + 2000 + 500) = 3500m / 8000m = 0.4375
	// Memory: (1024 + 2048 + 512) = 3584Mi / 32768Mi = 0.109375
	assert.InDelta(t, 0.4375, metrics.CPUUtilization, 0.01, "CPU utilization should be 43.75%")
	assert.InDelta(t, 0.109375, metrics.MemoryUtilization, 0.01, "Memory utilization should be 10.94%")
	assert.True(t, metrics.IsWarmupNode, "node should be warmup node")
	assert.Equal(t, 3, metrics.PodCount, "pod count should be 3")
	assert.Equal(t, "true", metrics.Labels["node.volcano.sh/warmup"], "warmup label should be preserved")
}

// TestSchedulerConfigParsing tests scheduler config parsing
func TestSchedulerConfigParsing(t *testing.T) {
	customConfigs := []SchedulerConfigSpec{
		{
			Name:              "custom-agent",
			Type:              "agent",
			CPUUtilizationMin: 0.8,
			CPUUtilizationMax: 1.0,
			PreferWarmupNodes: true,
			MinNodes:          2,
			MaxNodes:          5,
		},
		{
			Name:              "custom-volcano",
			Type:              "volcano",
			CPUUtilizationMin: 0.0,
			CPUUtilizationMax: 0.4,
			PreferWarmupNodes: false,
			MinNodes:          1,
			MaxNodes:          10,
		},
	}

	// Setup controller with custom configs
	opt := &TestControllerOption{
		SchedulerConfigs: customConfigs,
	}

	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)

	controller := testCtrl.Controller
	configs := controller.schedulerConfigs

	assert.Equal(t, 2, len(configs), "should have 2 scheduler configs")

	agentConfig := controller.getSchedulerConfigByName("custom-agent")
	assert.NotNil(t, agentConfig, "agent config should exist")
	assert.Equal(t, 0.8, agentConfig.ShardStrategy.CPUUtilizationRange.Min, "agent min CPU should be 0.8")
	assert.Equal(t, 1.0, agentConfig.ShardStrategy.CPUUtilizationRange.Max, "agent max CPU should be 1.0")
	assert.True(t, agentConfig.ShardStrategy.PreferWarmupNodes, "agent should prefer warmup nodes")
	assert.Equal(t, 2, agentConfig.ShardStrategy.MinNodes, "agent min nodes should be 2")
	assert.Equal(t, 5, agentConfig.ShardStrategy.MaxNodes, "agent max nodes should be 5")

	volcanoConfig := controller.getSchedulerConfigByName("custom-volcano")
	assert.NotNil(t, volcanoConfig, "volcano config should exist")
	assert.Equal(t, 0.0, volcanoConfig.ShardStrategy.CPUUtilizationRange.Min, "volcano min CPU should be 0.0")
	assert.Equal(t, 0.4, volcanoConfig.ShardStrategy.CPUUtilizationRange.Max, "volcano max CPU should be 0.4")
	assert.False(t, volcanoConfig.ShardStrategy.PreferWarmupNodes, "volcano should not prefer warmup nodes")
	assert.Equal(t, 1, volcanoConfig.ShardStrategy.MinNodes, "volcano min nodes should be 1")
	assert.Equal(t, 10, volcanoConfig.ShardStrategy.MaxNodes, "volcano max nodes should be 10")
}
