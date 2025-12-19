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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestLargeClusterPerformance tests performance with large number of nodes
func TestLargeClusterPerformance(t *testing.T) {
	nodeCount := 100
	podCountPerNode := 10

	// Create test objects
	objects := make([]runtime.Object, 0, nodeCount+(nodeCount*podCountPerNode))

	// Create nodes
	for i := 0; i < nodeCount; i++ {
		nodeName := fmt.Sprintf("node-%d", i)
		isWarmup := i%3 == 0

		node := CreateTestNode(nodeName, "8", isWarmup, nil)
		objects = append(objects, node)

		// Create pods (varying utilization)
		baseCPU := 100 + (i%8)*100 // 100m to 800m
		for j := 0; j < podCountPerNode; j++ {
			podName := fmt.Sprintf("pod-%d-%d", i, j)
			podCPU := fmt.Sprintf("%dm", baseCPU+j*50)

			pod := CreateTestPod("default", podName, nodeName, podCPU, "1Gi")
			objects = append(objects, pod)
		}
	}

	// Setup controller with appropriate configs for performance testing
	opt := &TestControllerOption{
		InitialObjects: objects,
		// FIX: Increase max nodes for performance testing
		SchedulerConfigs: []SchedulerConfigSpec{
			{
				Name:              "volcano-scheduler",
				Type:              "volcano",
				CPUUtilizationMin: 0.0,
				CPUUtilizationMax: 0.6,
				PreferWarmupNodes: false,
				MinNodes:          1,
				MaxNodes:          50, // Increased from 10
			},
			{
				Name:              "agent-scheduler",
				Type:              "agent",
				CPUUtilizationMin: 0.7,
				CPUUtilizationMax: 1.0,
				PreferWarmupNodes: true,
				MinNodes:          1,
				MaxNodes:          50, // Increased from 10
			},
		},
		ShardSyncPeriod: 10 * time.Second,
	}

	startTime := time.Now()

	testCtrl := NewTestShardingController(t, opt)
	defer close(testCtrl.StopCh)

	creationTime := time.Since(startTime)
	t.Logf("Controller setup completed in %v", creationTime)

	assert.Less(t, creationTime.Seconds(), 15.0, "controller setup should complete within 15 seconds")

	controller := testCtrl.Controller

	// Trigger initial metrics calculation
	// Get all nodes
	nodes, err := controller.nodeLister.List(labels.Everything())
	assert.NoError(t, err, "should list nodes")
	assert.Equal(t, nodeCount, len(nodes), "should have correct number of nodes")

	// Trigger events for all nodes
	for _, node := range nodes {
		controller.addNodeEvent(node)

		// Get pods on node
		pods, err := controller.podLister.Pods(metav1.NamespaceAll).List(labels.Everything())
		assert.NoError(t, err, "should list pods")

		for _, pod := range pods {
			if pod.Spec.NodeName == node.Name {
				controller.addPod(pod)
			}
		}
	}

	// Measure sync time
	syncStartTime := time.Now()
	controller.syncShards()
	syncDuration := time.Since(syncStartTime)

	t.Logf("Shard sync completed in %v", syncDuration)
	assert.Less(t, syncDuration.Seconds(), 5.0, "shard sync should complete within 5 seconds for %d nodes", nodeCount)

	// Wait for queue processing
	queueStartTime := time.Now()
	assert.NoError(t, WaitForQueueProcessing(controller, 10*time.Second), "queues should be processed within timeout")
	queueDuration := time.Since(queueStartTime)

	t.Logf("Queue processing completed in %v", queueDuration)
	assert.Less(t, queueDuration.Seconds(), 10.0, "queue processing should complete within 10 seconds")

	// Verify assignments
	agentShard, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.TODO(), "agent-scheduler", metav1.GetOptions{})
	assert.NoError(t, err, "should get agent shard")

	volcanoShard, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.TODO(), "volcano-scheduler", metav1.GetOptions{})
	assert.NoError(t, err, "should get volcano shard")

	totalAssigned := len(agentShard.Spec.NodesDesired) + len(volcanoShard.Spec.NodesDesired)
	t.Logf("Assigned %d out of %d nodes", totalAssigned, nodeCount)

	// Should assign most nodes
	assert.Greater(t, totalAssigned, nodeCount/2, "should assign most nodes")

	// Verify no overlap
	agentNodes := make(map[string]bool)
	for _, node := range agentShard.Spec.NodesDesired {
		agentNodes[node] = true
	}

	for _, node := range volcanoShard.Spec.NodesDesired {
		assert.False(t, agentNodes[node], "node %s should not be in both shards", node)
	}
}

// TestEventPressure tests handling of high event pressure
func TestEventPressure(t *testing.T) {
	// Setup controller with 10 nodes
	objects := make([]runtime.Object, 0, 20)
	for i := 0; i < 10; i++ {
		nodeName := fmt.Sprintf("event-node-%d", i)
		node := CreateTestNode(nodeName, "8", i%2 == 0, nil)
		objects = append(objects, node)

		// One pod per node
		podCount := 2 + i%3
		for j := 0; j < podCount; j++ {
			cpuRequest := 200 + j*300 + (i%5)*50 // Varied CPU requests
			pod := CreateTestPod("default", fmt.Sprintf("init-pod-%d-%d", i, j), nodeName,
				fmt.Sprintf("%dm", cpuRequest), "1Gi")
			objects = append(objects, pod)
		}
	}

	opt := &TestControllerOption{
		InitialObjects: objects,
		// FIX: Configure for better node assignment
		SchedulerConfigs: []SchedulerConfigSpec{
			{
				Name:              "agent-scheduler",
				Type:              "agent",
				CPUUtilizationMin: 0.3, // Lower threshold for testing
				CPUUtilizationMax: 1.0,
				PreferWarmupNodes: true,
				MinNodes:          1,
				MaxNodes:          10,
			},
			{
				Name:              "volcano-scheduler",
				Type:              "volcano",
				CPUUtilizationMin: 0.0,
				CPUUtilizationMax: 0.29,
				PreferWarmupNodes: false,
				MinNodes:          1,
				MaxNodes:          10,
			},
		},
		ShardSyncPeriod: 5 * time.Second,
	}

	testCtrl := NewTestShardingController(t, opt)
	defer close(testCtrl.StopCh)

	controller := testCtrl.Controller

	// Initial sync
	ForceSyncShards(t, controller, 5*time.Second)

	// Create event pressure - rapid pod updates
	startTime := time.Now()
	eventCount := 100

	for i := 0; i < eventCount; i++ {
		nodeIndex := i % 10
		nodeName := fmt.Sprintf("event-node-%d", nodeIndex)

		// Create and delete pods rapidly
		podName := fmt.Sprintf("pressure-pod-%d", i)
		pod := CreateTestPod("default", podName, nodeName, "500m", "512Mi")

		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create pressure pod")

		controller.addPod(pod)

		// Delete after short delay
		time.Sleep(10 * time.Millisecond)
		err = controller.kubeClient.CoreV1().Pods("default").Delete(context.TODO(), podName, metav1.DeleteOptions{})
		assert.NoError(t, err, "should delete pressure pod")

		controller.deletePod(pod)
	}

	// Measure total time
	totalDuration := time.Since(startTime)
	t.Logf("Processed %d events in %v (%.2f events/second)", eventCount, totalDuration, float64(eventCount)/totalDuration.Seconds())

	// Should handle events efficiently
	assert.Less(t, totalDuration.Seconds(), 10.0, "should process 100 events within 10 seconds")

	// Verify controller is still healthy
	// Check if queues are manageable
	mainQueueLen := controller.queue.Len()
	nodeEventQueueLen := controller.nodeEventQueue.Len()

	t.Logf("Queue lengths after pressure test - main: %d, node: %d", mainQueueLen, nodeEventQueueLen)
	assert.Less(t, mainQueueLen, 10, "main queue should not be overloaded")
	assert.Less(t, nodeEventQueueLen, 10, "node event queue should not be overloaded")

	// Final sync
	ForceSyncShards(t, controller, 5*time.Second)

	// Verify assignments still work
	agentShard, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.TODO(), "agent-scheduler", metav1.GetOptions{})
	assert.NoError(t, err, "should get agent shard after pressure test")

	volcanoShard, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.TODO(), "volcano-scheduler", metav1.GetOptions{})
	assert.NoError(t, err, "should get volcano shard after pressure test")

	assert.True(t, len(agentShard.Spec.NodesDesired) > 0, "agent scheduler should still have nodes")
	assert.True(t, len(volcanoShard.Spec.NodesDesired) > 0, "volcano scheduler should still have nodes")
}
