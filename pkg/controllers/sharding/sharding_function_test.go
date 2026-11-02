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

package sharding

import (
	"context"
	"sync"
	"sync/atomic"
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
	controller.configMu.RLock()
	configs := make([]SchedulerConfig, len(controller.schedulerConfigs))
	copy(configs, controller.schedulerConfigs)
	controller.configMu.RUnlock()

	assert.Equal(t, 2, len(configs), "should have 2 scheduler configs")

	agentConfig := controller.getSchedulerConfigByName("custom-agent")
	assert.NotNil(t, agentConfig, "agent config should exist")
	assert.Equal(t, 2, agentConfig.MinNodes, "agent min nodes should be 2")
	assert.Equal(t, 5, agentConfig.MaxNodes, "agent max nodes should be 5")
	// Deprecated scalar fields synthesize a Policies chain for compatibility.
	assert.Equal(t, 3, len(agentConfig.Policies), "agent should have allocation-rate + warmup + node-limit")
	assert.Equal(t, "allocation-rate", agentConfig.Policies[0].Name)
	assert.Equal(t, 0.8, agentConfig.Policies[0].Arguments["minCPUUtil"])
	assert.Equal(t, 1.0, agentConfig.Policies[0].Arguments["maxCPUUtil"])
	assert.Equal(t, "warmup", agentConfig.Policies[1].Name)
	assert.Equal(t, "node-limit", agentConfig.Policies[2].Name)

	volcanoConfig := controller.getSchedulerConfigByName("custom-volcano")
	assert.NotNil(t, volcanoConfig, "volcano config should exist")
	assert.Equal(t, 1, volcanoConfig.MinNodes, "volcano min nodes should be 1")
	assert.Equal(t, 10, volcanoConfig.MaxNodes, "volcano max nodes should be 10")
	assert.Equal(t, 2, len(volcanoConfig.Policies), "volcano should have allocation-rate + node-limit")
	assert.Equal(t, "allocation-rate", volcanoConfig.Policies[0].Name)
	assert.Equal(t, 0.0, volcanoConfig.Policies[0].Arguments["minCPUUtil"])
	assert.Equal(t, 0.4, volcanoConfig.Policies[0].Arguments["maxCPUUtil"])
	assert.Equal(t, "node-limit", volcanoConfig.Policies[1].Name)
}

func TestScheduleShardSyncCoalescesBurstRequests(t *testing.T) {
	controller := &ShardingController{}
	var syncCount atomic.Int32
	done := make(chan struct{}, 4)

	controller.syncShardsRunner = func() {
		syncCount.Add(1)
		done <- struct{}{}
	}

	controller.scheduleShardSync()
	controller.scheduleShardSync()
	controller.scheduleShardSync()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for debounced shard sync")
	}

	time.Sleep(shardSyncDebounceDelay * 2)
	assert.Equal(t, int32(1), syncCount.Load(), "burst requests should collapse into one shard sync")
	controller.stopScheduledShardSync()
}

func TestScheduleShardSyncSerializesFollowUpExecution(t *testing.T) {
	controller := &ShardingController{}
	var syncCount atomic.Int32
	var concurrentRuns atomic.Int32
	firstStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	allDone := make(chan struct{}, 2)
	var once sync.Once

	controller.syncShardsRunner = func() {
		if concurrentRuns.Add(1) != 1 {
			t.Error("shard sync should not run concurrently")
		}
		currentRun := syncCount.Add(1)
		if currentRun == 1 {
			once.Do(func() { firstStarted <- struct{}{} })
			<-releaseFirst
		}
		concurrentRuns.Add(-1)
		allDone <- struct{}{}
	}

	controller.scheduleShardSync()

	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first shard sync to start")
	}

	controller.scheduleShardSync()
	close(releaseFirst)

	select {
	case <-allDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first shard sync to finish")
	}

	select {
	case <-allDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for follow-up shard sync to finish")
	}

	assert.Equal(t, int32(2), syncCount.Load(), "a request raised during an in-flight sync should trigger one follow-up run")
	assert.Equal(t, int32(0), concurrentRuns.Load(), "all shard sync executions should have completed")
	controller.stopScheduledShardSync()
}

func TestScheduleShardSyncQueuesOnlyOneFollowUpWhileExecutionBlocked(t *testing.T) {
	controller := &ShardingController{}
	var syncCount atomic.Int32
	started := make(chan struct{}, 2)
	done := make(chan struct{}, 2)

	controller.syncShardsRunner = func() {
		syncCount.Add(1)
		started <- struct{}{}
		done <- struct{}{}
	}

	// Simulate a periodic/initial sync already holding the full-sync execution lock.
	controller.shardSyncExecMu.Lock()
	controller.scheduleShardSync()

	time.Sleep(shardSyncDebounceDelay * 2)
	controller.scheduleShardSync()
	controller.scheduleShardSync()
	controller.scheduleShardSync()

	controller.shardSyncExecMu.Unlock()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first blocked shard sync to start")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first blocked shard sync to finish")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued follow-up shard sync to start")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued follow-up shard sync to finish")
	}

	time.Sleep(shardSyncDebounceDelay * 2)
	assert.Equal(t, int32(2), syncCount.Load(), "blocked execution should produce only one queued follow-up shard sync")
	controller.stopScheduledShardSync()
}
