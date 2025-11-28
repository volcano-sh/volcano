package sharding

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformer "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"

	vcfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"

	"volcano.sh/volcano/pkg/controllers/framework"
)

// TestShardingController is a test wrapper for ShardingController
type TestShardingController struct {
	Controller *ShardingController
	StopCh     chan struct{}
}

// TestControllerOption contains options for test controller
type TestControllerOption struct {
	InitialObjects   []runtime.Object
	SchedulerConfigs []SchedulerConfigSpec
	ShardSyncPeriod  time.Duration
	StopCh           chan struct{} // Add stop channel
}

// NewTestControllerOption creates a new test controller option
// func NewTestControllerOption(objs ...runtime.Object) *TestControllerOption {
// 	stopCh := make(chan struct{})
// 	return &TestControllerOption{
// 		StopCh: stopCh,
// 	}
// }

// addTestIndexers adds required indexers for testing
// func addTestIndexers(kubeFactory kubeinformer.SharedInformerFactory) {
// 	// Add pod index by node name
// 	podInformer := kubeFactory.Core().V1().Pods()
// 	if err := podInformer.Informer().AddIndexers(cache.Indexers{
// 		"node": func(obj interface{}) ([]string, error) {
// 			pod, ok := obj.(*corev1.Pod)
// 			if !ok {
// 				return []string{}, nil
// 			}
// 			if pod.Spec.NodeName == "" {
// 				return []string{}, nil
// 			}
// 			return []string{pod.Spec.NodeName}, nil
// 		},
// 	}); err != nil {
// 		klog.Errorf("Failed to add pod index by node in test: %v", err)
// 	}
// }

// NewTestShardingController creates a new test controller with proper setup
func NewTestShardingController(t *testing.T, opt *TestControllerOption) *TestShardingController {
	// Create controller
	controller := &ShardingController{
		shardSyncPeriod: defaultShardSyncPeriod,
	}

	if opt == nil {
		opt = &TestControllerOption{}
	}

	// Set default scheduler configs if none provided
	if len(opt.SchedulerConfigs) == 0 {
		opt.SchedulerConfigs = getDefaultTestSchedulerConfigs()
	}

	if opt.StopCh == nil {
		opt.StopCh = make(chan struct{})
	}

	// Create fake clients
	kubeClient := kubefake.NewSimpleClientset(opt.InitialObjects...)
	vcClient := vcfake.NewSimpleClientset()

	// Create informer factories
	kubeInformerFactory := kubeinformer.NewSharedInformerFactory(kubeClient, time.Millisecond*500)
	vcInformerFactory := vcinformer.NewSharedInformerFactory(vcClient, time.Millisecond*500)

	// Create controller options
	controllerOpt := &framework.ControllerOption{
		KubeClient:              kubeClient,
		VolcanoClient:           vcClient,
		SharedInformerFactory:   kubeInformerFactory,
		VCSharedInformerFactory: vcInformerFactory,
	}

	// Initialize controller
	err := controller.Initialize(controllerOpt)
	assert.NoError(t, err, "should initialize controller successfully")

	// Initialize with scheduler configs
	err = controller.InitializeWithConfigs(controllerOpt, opt.SchedulerConfigs)
	assert.NoError(t, err, "should initialize controller with configs successfully")

	// // Start informers
	// stopCh := make(chan struct{})
	// //addTestIndexers(kubeInformerFactory)
	// kubeInformerFactory.Start(stopCh)
	// vcInformerFactory.Start(stopCh)

	// // Wait for cache sync
	// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// defer cancel()

	// if !cache.WaitForCacheSync(
	// 	ctx.Done(),
	// 	kubeInformerFactory.Core().V1().Nodes().Informer().HasSynced,
	// 	vcInformerFactory.Shard().V1alpha1().NodeShards().Informer().HasSynced,
	// 	kubeInformerFactory.Core().V1().Pods().Informer().HasSynced,
	// ) {
	// 	t.Fatal("Failed to sync caches within timeout")
	// }

	// // Give time for event handlers to register
	// time.Sleep(200 * time.Millisecond)

	// // Initialize node metrics for test nodes
	// controller.initializeNodeMetrics()

	// Start controller in separate goroutine
	go controller.Run(opt.StopCh)

	// Wait for controller to start
	time.Sleep(300 * time.Millisecond)

	return &TestShardingController{
		Controller: controller,
		StopCh:     opt.StopCh,
	}
}

// getDefaultTestSchedulerConfigs returns default scheduler configs for testing
func getDefaultTestSchedulerConfigs() []SchedulerConfigSpec {
	return []SchedulerConfigSpec{
		{
			Name:              "volcano-scheduler",
			Type:              "volcano",
			CPUUtilizationMin: 0.0,
			CPUUtilizationMax: 0.6,
			PreferWarmupNodes: false,
			MinNodes:          1,
			MaxNodes:          10,
		},
		{
			Name:              "agent-scheduler",
			Type:              "agent",
			CPUUtilizationMin: 0.7,
			CPUUtilizationMax: 1.0,
			PreferWarmupNodes: true,
			MinNodes:          1,
			MaxNodes:          10,
		},
	}
}

// WaitForNodeMetricsUpdate waits for node metrics to be updated
func WaitForNodeMetricsUpdate(t *testing.T, controller *ShardingController, nodeName string, timeout time.Duration) {
	start := time.Now()
	for {
		metrics := controller.GetNodeMetrics(nodeName)
		if metrics != nil && !metrics.LastUpdated.IsZero() {
			return
		}

		if time.Since(start) > timeout {
			t.Fatalf("Timeout waiting for node metrics to be updated for %s", nodeName)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// WaitForQueueProcessing waits for queue to be processed
func WaitForQueueProcessing(controller *ShardingController, timeout time.Duration) error {
	start := time.Now()
	for {
		if controller.queue.Len() == 0 && controller.nodeEventQueue.Len() == 0 {
			return nil
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for queue to empty, main queue: %d, node event queue: %d",
				controller.queue.Len(), controller.nodeEventQueue.Len())
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// ForceSyncShards forces a shard synchronization and waits for completion
func ForceSyncShards(t *testing.T, controller *ShardingController, timeout time.Duration) {
	controller.syncShards()
	assert.NoError(t, WaitForQueueProcessing(controller, timeout), "queue should be processed within timeout")
	// Clean up queue
	for controller.queue.Len() > 0 {
		controller.processNextItem()
	}
}

// VerifyAssignment verifies assignment for a scheduler
func VerifyAssignment(t *testing.T, controller *ShardingController, schedulerName string, expectedNodes []string) {
	shard, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.TODO(), schedulerName, metav1.GetOptions{})
	assert.NoError(t, err, "should get shard for %s", schedulerName)

	// Sort nodes for consistent comparison
	actualNodes := shard.Spec.NodesDesired
	sort.Strings(actualNodes)
	sort.Strings(expectedNodes)

	assert.ElementsMatch(t, expectedNodes, shard.Spec.NodesDesired,
		"nodes assigned to %s should match expected", schedulerName)
}

// SetupPodsOnNode creates multiple pods on a specific node
func SetupPodsOnNode(t *testing.T, controller *ShardingController, nodeName string, pods []*corev1.Pod) {
	for _, pod := range pods {
		pod.Spec.NodeName = nodeName
		_, err := controller.kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create pod %s", pod.Name)
	}
}

// CreateTestNode creates a test node with specified configuration
func CreateTestNode(name string, cpu string, isWarmup bool, labels map[string]string) *corev1.Node {
	if labels == nil {
		labels = make(map[string]string)
	}

	if isWarmup {
		labels["node.volcano.sh/warmup"] = "true"
	}

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
				corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalExponent),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
				corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalExponent),
			},
		},
	}
}

// CreateTestPod creates a test pod with specified resource requests
func CreateTestPod(namespace, name, nodeName, cpu, memory string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name: "test-container",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpu),
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

// CreateTestNodeWithPods creates a test node with multiple pods
func CreateTestNodeWithPods(t *testing.T, controller *ShardingController, nodeName string, cpu string, isWarmup bool, pods []*corev1.Pod) {
	// Create node
	node := CreateTestNode(nodeName, cpu, isWarmup, nil)
	_, err := controller.kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	assert.NoError(t, err, "should create node %s", nodeName)

	// Create pods
	for _, pod := range pods {
		pod.Spec.NodeName = nodeName
		_, err := controller.kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err, "should create pod %s", pod.Name)
	}

	// Wait for cache sync
	time.Sleep(300 * time.Millisecond)
}

// CleanupController cleans up controller resources
func CleanupController(testCtrl *TestShardingController) {
	if testCtrl == nil {
		return
	}

	// First, process any remaining items in queues
	if testCtrl.Controller != nil {
		// Process main queue
		for testCtrl.Controller.queue.Len() > 0 {
			testCtrl.Controller.processNextItem()
		}
		klog.Infof("Processed remaining items in main queue")

		// Process node event queue
		for testCtrl.Controller.nodeEventQueue.Len() > 0 {
			key, quit := testCtrl.Controller.nodeEventQueue.Get()
			if quit {
				break
			}
			testCtrl.Controller.nodeEventQueue.Done(key)
		}
		klog.Infof("Processed remaining items in queues")
		// Close assignment change channel if not nil
		if testCtrl.Controller.assignmentChangeChan != nil {
			close(testCtrl.Controller.assignmentChangeChan)
			testCtrl.Controller.assignmentChangeChan = nil
		}
		klog.Infof("Closed assignment change channel")
	}

	// Close stop channel if exists
	if testCtrl.StopCh != nil {
		select {
		case <-testCtrl.StopCh:
			// Already closed
		default:
			close(testCtrl.StopCh)
		}
		testCtrl.StopCh = nil
	}
	klog.Infof("Closed stop channel")

	// Clear controller reference
	if testCtrl.Controller != nil {
		testCtrl.Controller = nil
	}
}
