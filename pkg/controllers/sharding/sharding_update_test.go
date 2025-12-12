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

// TestPodEventsTriggerNodeMetricsUpdate tests that pod events trigger node metrics update
func TestPodEventsTriggerNodeMetricsUpdate(t *testing.T) {
	// Setup controller
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("dynamic-node", "8", false, nil),
		},
		ShardSyncPeriod: 2 * time.Second,
	}

	testCtrl := NewTestShardingController(t, opt)
	defer close(testCtrl.StopCh)

	controller := testCtrl.Controller

	// Initial metrics (should be 0 since no pods)
	initialMetrics := controller.GetNodeMetrics("dynamic-node")
	assert.NotNil(t, initialMetrics, "initial metrics should exist")
	assert.Equal(t, 0.0, initialMetrics.CPUUtilization, "initial CPU utilization should be 0")

	// Create and add pods
	// Initial pods (low utilization)
	lowUtilPods := []*corev1.Pod{
		CreateTestPod("default", "low-pod-1", "dynamic-node", "1000m", "1Gi"),
		CreateTestPod("default", "low-pod-2", "dynamic-node", "500m", "512Mi"),
	}

	for _, pod := range lowUtilPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create pod")
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger events
	for _, pod := range lowUtilPods {
		controller.addPod(pod)
	}

	// Wait for metrics update
	WaitForNodeMetricsUpdate(t, controller, "dynamic-node", 2*time.Second)

	// Verify updated metrics (low utilization)
	lowUtilMetrics := controller.GetNodeMetrics("dynamic-node")
	assert.NotNil(t, lowUtilMetrics, "low utilization metrics should exist")
	assert.InDelta(t, 0.1875, lowUtilMetrics.CPUUtilization, 0.01, "CPU utilization should be ~18.75% after adding low util pods")

	// Add high utilization pods
	highUtilPods := []*corev1.Pod{
		CreateTestPod("default", "high-pod-1", "dynamic-node", "3000m", "3Gi"),
		CreateTestPod("default", "high-pod-2", "dynamic-node", "2500m", "2Gi"),
	}

	for _, pod := range highUtilPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create high util pod")
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger events
	for _, pod := range highUtilPods {
		controller.addPod(pod)
	}

	// Wait for metrics update
	WaitForNodeMetricsUpdate(t, controller, "dynamic-node", 2*time.Second)

	// Verify updated metrics (high utilization)
	highUtilMetrics := controller.GetNodeMetrics("dynamic-node")
	assert.NotNil(t, highUtilMetrics, "high utilization metrics should exist")
	// Total: 1000+500+3000+2500 = 7000m / 8000m = 0.875
	assert.InDelta(t, 0.875, highUtilMetrics.CPUUtilization, 0.01, "CPU utilization should be ~87.5% after adding high util pods")
}

// TestNodeAdditionAndDeletion tests node addition and deletion handling
func TestNodeAdditionAndDeletion(t *testing.T) {
	// Setup controller with one node
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("initial-node", "8", true, nil),
		},
		SchedulerConfigs: getDefaultTestSchedulerConfigs(),
		ShardSyncPeriod:  2 * time.Second,
	}

	testCtrl := NewTestShardingController(t, opt)
	defer close(testCtrl.StopCh)

	controller := testCtrl.Controller

	// Create initial pods
	initialPods := []*corev1.Pod{
		CreateTestPod("default", "initial-pod-1", "initial-node", "6000m", "6Gi"), // 75% utilization
	}

	for _, pod := range initialPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create initial pod")
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger events and sync
	for _, pod := range initialPods {
		controller.addPod(pod)
	}
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify initial assignment - should go to agent scheduler
	VerifyAssignment(t, controller, "agent-scheduler", []string{"initial-node"})
	VerifyAssignment(t, controller, "volcano-scheduler", []string{})

	// Add new node
	newNode := CreateTestNode("new-node", "8", false, nil)
	_, err := controller.kubeClient.CoreV1().Nodes().Create(context.TODO(), newNode, metav1.CreateOptions{})
	assert.NoError(t, err, "should create new node")

	// Create pods on new node (low utilization)
	newPods := []*corev1.Pod{
		CreateTestPod("default", "new-pod-1", "new-node", "1000m", "1Gi"),
		CreateTestPod("default", "new-pod-2", "new-node", "500m", "512Mi"),
	}

	for _, pod := range newPods {
		_, err := controller.kubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create new pod")
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger events
	controller.addNodeEvent(newNode)
	for _, pod := range newPods {
		controller.addPod(pod)
	}

	// Fake Use
	fakeShardInUse(t, controller, "agent-scheduler")
	fakeShardInUse(t, controller, "volcano-scheduler")

	// Sync
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify assignments
	// initial-node: 75% -> agent-scheduler
	// new-node: ~18.75% -> volcano-scheduler
	VerifyAssignment(t, controller, "agent-scheduler", []string{"initial-node"})
	VerifyAssignment(t, controller, "volcano-scheduler", []string{"new-node"})
	VerifyAssignmentUpdate(t, controller, "volcano-scheduler", []string{"new-node"}, []string{})

	// Delete initial node
	err = controller.kubeClient.CoreV1().Nodes().Delete(context.TODO(), "initial-node", metav1.DeleteOptions{})
	assert.NoError(t, err, "should delete initial node")

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)

	// Trigger event
	controller.deleteNodeEvent(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "initial-node"}})

	// Sync
	fakeShardInUse(t, controller, "agent-scheduler")
	fakeShardInUse(t, controller, "volcano-scheduler")
	ForceSyncShards(t, controller, 2*time.Second)

	// Verify assignments after deletion
	// Only new-node should remain, assigned to volcano-scheduler
	VerifyAssignment(t, controller, "agent-scheduler", []string{})
	VerifyAssignmentUpdate(t, controller, "agent-scheduler", []string{}, []string{"initial-node"})
	VerifyAssignment(t, controller, "volcano-scheduler", []string{"new-node"})
}
