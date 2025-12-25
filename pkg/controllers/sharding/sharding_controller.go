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
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	k8sinformerv1 "k8s.io/client-go/informers/core/v1"
	kubeclient "k8s.io/client-go/kubernetes"
	k8slisterv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformers "volcano.sh/apis/pkg/client/informers/externalversions"
	shardinformers "volcano.sh/apis/pkg/client/informers/externalversions/shard/v1alpha1"
	shardlisters "volcano.sh/apis/pkg/client/listers/shard/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/framework"
)

const (
	controllerName              = "sharding-controller"
	defaultShardSyncPeriod      = 60 * time.Second
	maxAssignmentCacheRetention = 5 * time.Minute
	nodeCountChangeThreshold    = 0.5
	updateTimeoutThreshold      = 15 * time.Second
	nodeUsageChangeThreshold    = 0.5
	nodeRefreshPeriod           = 120 * time.Second
)

func init() {
	framework.RegisterController(&ShardingController{})
}

// ShardingController implements the framework.Controller interface
type ShardingController struct {
	ctx               context.Context
	controllerOptions ShardingControllerOptions

	kubeClient kubeclient.Interface
	vcClient   vcclient.Interface

	vcInformerFactory   vcinformers.SharedInformerFactory
	kubeInformerFactory informers.SharedInformerFactory

	nodeInformer  k8sinformerv1.NodeInformer
	podInformer   k8sinformerv1.PodInformer
	shardInformer shardinformers.NodeShardInformer

	nodeLister  k8slisterv1.NodeLister
	podLister   k8slisterv1.PodLister
	shardLister shardlisters.NodeShardLister

	nodeMetricsCache map[string]*NodeMetrics
	metricsMutex     sync.RWMutex

	nodeShardQueue workqueue.TypedRateLimitingInterface[string]
	nodeEventQueue workqueue.TypedRateLimitingInterface[string]

	shardingManager  *ShardingManager
	shardSyncPeriod  time.Duration
	schedulerConfigs []SchedulerConfig

	// assignment cache
	assignmentCache *AssignmentCache
	cacheMutex      sync.Mutex

	// Event channels
	assignmentChangeChan chan *AssignmentChangeEvent
}

// Return the name of the controller
func (sc *ShardingController) Name() string {
	return controllerName
}

// Initialize initializes the controller
func (sc *ShardingController) Initialize(opt *framework.ControllerOption) error {
	klog.V(2).Infof("Initializing ShardingController...")
	sc.ctx = context.Background()
	sc.controllerOptions = NewShardingControllerOptions()

	sc.kubeClient = opt.KubeClient
	sc.vcClient = opt.VolcanoClient

	sc.vcInformerFactory = opt.VCSharedInformerFactory
	sc.kubeInformerFactory = opt.SharedInformerFactory

	// Initialize informers
	sc.nodeInformer = sc.kubeInformerFactory.Core().V1().Nodes()
	sc.podInformer = sc.kubeInformerFactory.Core().V1().Pods()
	sc.shardInformer = sc.vcInformerFactory.Shard().V1alpha1().NodeShards()

	sc.nodeLister = sc.nodeInformer.Lister()
	sc.podLister = sc.podInformer.Lister()
	sc.shardLister = sc.shardInformer.Lister()

	sc.initNodeIndices()

	// Initialize queues
	sc.nodeShardQueue = workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: controllerName},
	)
	sc.nodeEventQueue = workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: controllerName + "-node-events"},
	)

	// Initialize components
	// Initialize sharding manager
	sc.assignmentCache = &AssignmentCache{
		Assignments: make(map[string]*ShardAssignment),
	}
	sc.assignmentChangeChan = make(chan *AssignmentChangeEvent, 100)

	// Parse scheduler configs from options
	sc.parseSchedulerConfigsFromOptions()

	// Initialize metrics cache
	sc.nodeMetricsCache = make(map[string]*NodeMetrics)

	// Initialize sharding manager with self as metrics provider
	sc.shardingManager = NewShardingManager(sc.schedulerConfigs, sc)
	sc.shardSyncPeriod = sc.controllerOptions.ShardSyncPeriod

	// Setup event handlers
	sc.nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.addNodeEvent,
		UpdateFunc: sc.updateNodeEvent,
		DeleteFunc: sc.deleteNodeEvent,
	})

	sc.shardInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.addShard,
		UpdateFunc: sc.updateShard,
		DeleteFunc: sc.deleteShard,
	})

	sc.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.addPod,
		UpdateFunc: sc.updatePod,
		DeleteFunc: sc.deletePod,
	})
	klog.V(6).Infof("ShardingController initialized successfully")

	return nil
}

// Run starts the controller
func (sc *ShardingController) Run(stopCh <-chan struct{}) {
	defer sc.nodeShardQueue.ShutDown()
	defer sc.nodeEventQueue.ShutDown()

	klog.Infof("Starting sharding controller.")
	defer klog.Infof("Shutting down sharding controller.")

	// Start informer factories
	sc.kubeInformerFactory.Start(stopCh)
	sc.vcInformerFactory.Start(stopCh)

	// Add specific sync checks with detailed logging
	klog.Infof("Waiting for cache synchronization...")
	if !cache.WaitForCacheSync(
		stopCh,
		sc.nodeInformer.Informer().HasSynced,
		sc.shardInformer.Informer().HasSynced,
		sc.podInformer.Informer().HasSynced,
	) {
		klog.Errorf("cache sync failed")
	}
	klog.Infof("Cache synchronization completed successfully")

	// Initialize node metrics
	sc.refreshNodeMetrics()

	// Initial sync
	sc.syncShards()

	// Start workers
	go wait.Until(sc.worker, time.Second, stopCh)
	go wait.Until(sc.nodeEventWorker, time.Second, stopCh)

	// Start periodic sync
	go wait.Until(sc.syncShards, sc.shardSyncPeriod, stopCh)

	// Start assignment change processor
	go sc.assignmentChangeProcessor(stopCh)

	// Start periodic metrics refresh
	go wait.Until(sc.refreshNodeMetrics, nodeRefreshPeriod, stopCh)

	<-stopCh
	klog.Infof("Shutting down %s", controllerName)
}

// refreshNodeMetrics periodically refreshes node metrics
func (sc *ShardingController) refreshNodeMetrics() {
	klog.V(4).Infof("Refreshing node metrics...")

	nodes, err := sc.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list nodes for metrics refresh: %v", err)
		return
	}

	// Refresh metrics for nodes that haven't been updated recently
	sc.metricsMutex.Lock()
	defer sc.metricsMutex.Unlock()
	for _, node := range nodes {
		metrics := sc.GetNodeMetrics(node.Name)
		if metrics == nil || time.Since(metrics.LastUpdated) > 2*time.Minute {
			newMetrics, err := sc.calculateNodeUtilization(node.Name)
			if err != nil {
				klog.Warningf("Failed to refresh metrics for node %s: %v", node.Name, err)
				return
			}
			sc.updateNodeUtilization(node.Name, newMetrics)
		}
	}
}

// parseSchedulerConfigsFromOptions parses scheduler configs from controller options
func (sc *ShardingController) parseSchedulerConfigsFromOptions() {
	sc.schedulerConfigs = make([]SchedulerConfig, 0, len(sc.controllerOptions.SchedulerConfigs))

	for _, configSpec := range sc.controllerOptions.SchedulerConfigs {
		config := SchedulerConfig{
			Name: configSpec.Name,
			Type: configSpec.Type,
			ShardStrategy: ShardStrategy{
				CPUUtilizationRange: struct {
					Min float64
					Max float64
				}{
					Min: configSpec.CPUUtilizationMin,
					Max: configSpec.CPUUtilizationMax,
				},
				PreferWarmupNodes: configSpec.PreferWarmupNodes,
				MinNodes:          configSpec.MinNodes,
				MaxNodes:          configSpec.MaxNodes,
			},
		}

		sc.schedulerConfigs = append(sc.schedulerConfigs, config)
		klog.Infof("Added scheduler config: %s", config.Name)
	}

	klog.Infof("Initialized with %d scheduler configurations", len(sc.schedulerConfigs))
	for _, config := range sc.schedulerConfigs {
		klog.Infof("  Scheduler %s (%s): CPU range [%.2f, %.2f], warmup=%v, nodes=[%d,%d]",
			config.Name, config.Type,
			config.ShardStrategy.CPUUtilizationRange.Min,
			config.ShardStrategy.CPUUtilizationRange.Max,
			config.ShardStrategy.PreferWarmupNodes,
			config.ShardStrategy.MinNodes,
			config.ShardStrategy.MaxNodes)
	}
}

// worker processes items from the work queue
func (sc *ShardingController) worker() {
	for sc.processNextItem() {
	}
}

// processNextItem processes a single item from the work queue
func (sc *ShardingController) processNextItem() bool {
	key, quit := sc.nodeShardQueue.Get()
	if quit {
		return false
	}
	defer sc.nodeShardQueue.Done(key)

	err := sc.syncHandler(key)
	if err == nil {
		sc.nodeShardQueue.Forget(key)
	} else if sc.nodeShardQueue.NumRequeues(key) < 3 {
		klog.Errorf("Error syncing shard %v: %v", key, err)
		sc.nodeShardQueue.AddRateLimited(key)
	} else {
		klog.Errorf("Dropping shard %q out of the queue: %v", key, err)
		sc.nodeShardQueue.Forget(key)
	}

	return true
}

// assignmentChangeProcessor processes assignment changes
func (sc *ShardingController) assignmentChangeProcessor(stopCh <-chan struct{}) {
	for {
		select {
		case event := <-sc.assignmentChangeChan:
			sc.processAssignmentChange(event)
		case <-stopCh:
			return
		}
	}
}

// processAssignmentChange processes an assignment change event
func (sc *ShardingController) processAssignmentChange(event *AssignmentChangeEvent) {
	klog.V(4).Infof("Processing assignment change for %s: %d -> %d nodes",
		event.SchedulerName, len(event.OldNodes), len(event.NewNodes))

	// Log significant changes
	if len(event.OldNodes) > 0 {
		changePercent := float64(abs(len(event.NewNodes)-len(event.OldNodes))) / float64(len(event.OldNodes))
		if changePercent > nodeCountChangeThreshold {
			klog.Infof("Significant node change for %s: %.0f%% (%d -> %d nodes)",
				event.SchedulerName, changePercent*100, len(event.OldNodes), len(event.NewNodes))
		}
	}
}

// syncShards performs global shard assignment calculation
func (sc *ShardingController) syncShards() {
	klog.Infof("Starting global shard synchronization")
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		klog.V(3).Infof("Completed global shard synchronization in %v", duration)
	}()

	// ensure all nodes states are updated
	//sc.ensureNodeStatesUpdated()

	// Get nodes from cache (not API server)
	nodes, err := sc.listNodesFromCache()
	if err != nil {
		klog.Errorf("Failed to list nodes from cache: %v", err)
		return
	}

	// Get current shards
	currentShards, err := sc.shardLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list current shards: %v", err)
		return
	}

	// Calculate new assignments
	newAssignments, err := sc.shardingManager.CalculateShardAssignments(nodes, currentShards)
	if err != nil {
		klog.Errorf("Failed to calculate shard assignments: %v", err)
		return
	}

	// Update cache with new assignments
	sc.updateAssignmentCache(newAssignments)

	// Trigger sync for each scheduler
	for schedulerName := range newAssignments {
		sc.enqueueShard(schedulerName)
	}

	klog.Infof("Global shard sync completed: %d schedulers, %d nodes",
		len(newAssignments), len(nodes))
}

// syncHandler handles synchronization of a single shard
func (sc *ShardingController) syncHandler(schedulerName string) error {
	klog.V(4).Infof("Processing shard sync for scheduler: %s", schedulerName)

	// Get assignment from cache
	assignment := sc.getAssignmentFromCache(schedulerName)
	if assignment == nil {
		klog.Warningf("No cached assignment for %s, falling back to direct calculation", schedulerName)
		return sc.calculateAndApplyAssignment(schedulerName)
	}

	// Apply assignment
	return sc.applyAssignment(schedulerName, assignment)
}

// getAssignmentFromCache retrieves assignment from cache with version check
func (sc *ShardingController) getAssignmentFromCache(schedulerName string) *ShardAssignment {
	sc.cacheMutex.Lock()
	defer sc.cacheMutex.Unlock()

	cache := sc.assignmentCache
	if cache == nil || cache.Timestamp.Add(maxAssignmentCacheRetention).Before(time.Now()) {
		return nil
	}

	return cache.Assignments[schedulerName]
}

// updateAssignmentCache updates the assignment cache with version control
func (sc *ShardingController) updateAssignmentCache(newAssignments map[string]*ShardAssignment) {
	sc.cacheMutex.Lock()
	defer sc.cacheMutex.Unlock()

	version := fmt.Sprintf("%d", time.Now().UnixNano())
	sc.assignmentCache = &AssignmentCache{
		Version:     version,
		Timestamp:   time.Now(),
		Assignments: newAssignments,
	}

	klog.V(4).Infof("Updated assignment cache with version %s", version)
}

// createShard creates a new shard CRD
func (sc *ShardingController) createShard(schedulerName string, nodesDesired []string) error {
	klog.Infof("Creating new shard for scheduler: %s", schedulerName)

	// First check whether the shard already exists or not
	_, err := sc.shardLister.Get(schedulerName)
	if err == nil {
		klog.Infof("Shard %s already exists, skipping creation", schedulerName)
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if shard %s exists: %v", schedulerName, err)
	}

	schedulerConfig := sc.getSchedulerConfigByName(schedulerName)
	if schedulerConfig == nil {
		return fmt.Errorf("scheduler config not found for %s", schedulerName)
	}

	shard := &shardv1alpha1.NodeShard{
		ObjectMeta: metav1.ObjectMeta{
			Name: schedulerName,
		},
		Spec: shardv1alpha1.NodeShardSpec{
			NodesDesired: nodesDesired,
		},
		Status: shardv1alpha1.NodeShardStatus{
			LastUpdateTime: metav1.Now(),
			NodesToAdd:     nodesDesired,
			NodesToRemove:  []string{},
		},
	}

	// create a new shard
	_, err = sc.vcClient.ShardV1alpha1().NodeShards().Create(sc.ctx, shard, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			klog.Infof("Shard %s was created concurrently by another process", schedulerName)
			return nil
		}
		return fmt.Errorf("failed to create shard %s: %v", schedulerName, err)
	}

	klog.Infof("Successfully created shard %s with %d nodes", schedulerName, len(nodesDesired))
	return nil
}

// applyAssignment applies an assignment to a shard
func (sc *ShardingController) applyAssignment(schedulerName string, assignment *ShardAssignment) error {
	// Get current shard
	shard, err := sc.shardLister.Get(schedulerName)
	if err != nil {
		if errors.IsNotFound(err) {
			return sc.createShard(schedulerName, assignment.NodesDesired)
		}
		return fmt.Errorf("failed to get shard %s: %v", schedulerName, err)
	}

	// Check if update is needed
	if !sc.assignmentNeedsUpdate(shard, assignment) {
		klog.V(4).Infof("No update needed for shard %s", schedulerName)
		return nil
	}

	// Create new shard
	currentNodesInUse := shard.Status.NodesInUse
	newShard := shard.DeepCopy()
	newShard.Spec.NodesDesired = assignment.NodesDesired
	newShard.Status.LastUpdateTime = metav1.Now()
	nodesToAdd, nodesToRemove := sc.findNodesToAddRemove(currentNodesInUse, assignment.NodesDesired)
	newShard.Status.NodesToAdd = nodesToAdd
	newShard.Status.NodesToRemove = nodesToRemove
	klog.V(6).Infof("%s - current shard: %s, assignment: %s, NodesToadd: %s, NodesToRemove: %s", schedulerName, currentNodesInUse, assignment.NodesDesired, nodesToAdd, nodesToRemove)

	// Apply update
	_, err = sc.vcClient.ShardV1alpha1().NodeShards().Update(sc.ctx, newShard, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update shard %s: %v", schedulerName, err)
	}

	// Publish change event
	sc.assignmentChangeChan <- &AssignmentChangeEvent{
		SchedulerName: schedulerName,
		OldNodes:      shard.Spec.NodesDesired,
		NewNodes:      assignment.NodesDesired,
		NodesToAdd:    nodesToAdd,
		NodesToRemove: nodesToRemove,
		Version:       assignment.Version,
		Timestamp:     time.Now(),
	}

	klog.V(6).Infof("Updated shard %s with %d nodes", schedulerName, len(assignment.NodesDesired))
	return nil
}

// find NodesToAdd and NodesToRemove in assignments
func (sc *ShardingController) findNodesToAddRemove(oldNodes []string, newNodes []string) (nodesToAdd []string, nodesToRemove []string) {
	// Create hash maps to store presence of elements in each list.
	oldNodesMap := make(map[string]bool)
	newNodesMap := make(map[string]bool)

	// Populate oldNodesMap with elements from oldNodes.
	for _, node := range oldNodes {
		oldNodesMap[node] = true
	}

	// Populate newNodesMap with elements from newNodes.
	for _, node := range newNodes {
		newNodesMap[node] = true
	}

	// find nodes to remove
	nodesToRemove = []string{}
	for _, node := range oldNodes {
		if _, foundInNewNodes := newNodesMap[node]; !foundInNewNodes {
			nodesToRemove = append(nodesToRemove, node)
		}
	}

	// find nodes to add
	nodesToAdd = []string{}
	for _, node := range newNodes {
		if _, foundInOldNodes := oldNodesMap[node]; !foundInOldNodes {
			nodesToAdd = append(nodesToAdd, node)
		}
	}

	return nodesToAdd, nodesToRemove
}

// assignmentNeedsUpdate determines if assignment needs update
func (sc *ShardingController) assignmentNeedsUpdate(shard *shardv1alpha1.NodeShard, assignment *ShardAssignment) bool {
	// Fast path: different node count
	if len(shard.Spec.NodesDesired) != len(assignment.NodesDesired) {
		return true
	}

	// Check node set difference
	currentSet := make(map[string]bool)
	for _, node := range shard.Spec.NodesDesired {
		currentSet[node] = true
	}

	changed := false
	minChangeThreshold := max(1, len(assignment.NodesDesired)/10) // 10% threshold
	changeCount := 0

	for _, node := range assignment.NodesDesired {
		if !currentSet[node] {
			changeCount++
			if changeCount >= minChangeThreshold {
				changed = true
				break
			}
		}
	}

	return changed
}

// listNodesFromCache lists nodes from informer cache
func (sc *ShardingController) listNodesFromCache() ([]*corev1.Node, error) {
	if sc.nodeLister == nil {
		return nil, fmt.Errorf("nodeLister not initialized")
	}

	nodes, err := sc.nodeLister.List(labels.Everything())
	if err != nil {
		klog.V(4).Infof("No nodes found in cache")
		return nil, err
	}

	return nodes, nil
}

// getSchedulerConfigByName finds scheduler config by name
func (sc *ShardingController) getSchedulerConfigByName(name string) *SchedulerConfig {
	for _, config := range sc.schedulerConfigs {
		if config.Name == name {
			return &config
		}
	}
	return nil
}

// calculateAndApplyAssignment calculates and applies assignment directly (fallback)
func (sc *ShardingController) calculateAndApplyAssignment(schedulerName string) error {
	nodes, err := sc.listNodesFromCache()
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	schedulerConfig := sc.getSchedulerConfigByName(schedulerName)
	if schedulerConfig == nil {
		return fmt.Errorf("scheduler config not found for %s", schedulerName)
	}

	ctx := &AssignmentContext{
		AllNodes: nodes,
	}

	assignment, err := sc.shardingManager.calculateSingleSchedulerAssignment(*schedulerConfig, ctx)
	if err != nil {
		return fmt.Errorf("failed to calculate assignment: %v", err)
	}

	return sc.applyAssignment(schedulerName, &ShardAssignment{
		SchedulerName: schedulerName,
		NodesDesired:  assignment.NodesDesired,
		Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
	})
}

// enqueueShard adds a shard to the work queue
func (sc *ShardingController) enqueueShard(schedulerName string) {
	sc.nodeShardQueue.Add(schedulerName)
}

// addShard handles shard addition events
func (sc *ShardingController) addShard(obj interface{}) {
	shard := obj.(*shardv1alpha1.NodeShard)
	klog.V(4).Infof("Added shard: %s", shard.Name)
	sc.enqueueShard(shard.Name)
}

// updateShard handles shard update events
func (sc *ShardingController) updateShard(oldObj, newObj interface{}) {
	oldShard := oldObj.(*shardv1alpha1.NodeShard)
	newShard := newObj.(*shardv1alpha1.NodeShard)

	if oldShard.ResourceVersion == newShard.ResourceVersion {
		return
	}

	klog.V(4).Infof("Updated shard: %s", newShard.Name)
	sc.enqueueShard(newShard.Name)
}

// deleteShard handles shard deletion events
func (sc *ShardingController) deleteShard(obj interface{}) {
	shard, ok := obj.(*shardv1alpha1.NodeShard)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone")
			return
		}
		shard, ok = tombstone.Obj.(*shardv1alpha1.NodeShard)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a NodeShard")
			return
		}
	}
	klog.V(4).Infof("Deleted shard: %s", shard.Name)
	sc.enqueueShard(shard.Name)
}

// InitializeWithConfigs initializes the controller with specific scheduler configs
func (sc *ShardingController) InitializeWithConfigs(opt *framework.ControllerOption, configSpecs []SchedulerConfigSpec) error {
	if err := sc.Initialize(opt); err != nil {
		return err
	}

	// Convert config specs to internal scheduler configs
	sc.schedulerConfigs = make([]SchedulerConfig, 0, len(configSpecs))
	for _, spec := range configSpecs {
		config := SchedulerConfig{
			Name: spec.Name,
			Type: spec.Type,
			ShardStrategy: ShardStrategy{
				CPUUtilizationRange: struct {
					Min float64
					Max float64
				}{
					Min: spec.CPUUtilizationMin,
					Max: spec.CPUUtilizationMax,
				},
				PreferWarmupNodes: spec.PreferWarmupNodes,
				MinNodes:          spec.MinNodes,
				MaxNodes:          spec.MaxNodes,
			},
		}
		sc.schedulerConfigs = append(sc.schedulerConfigs, config)
	}

	// Reinitialize sharding manager with new configs
	sc.shardingManager = NewShardingManager(sc.schedulerConfigs, sc)

	klog.Infof("Initialized with %d scheduler configurations:", len(sc.schedulerConfigs))
	for _, config := range sc.schedulerConfigs {
		klog.Infof("  %s (%s): CPU [%.2f, %.2f], warmup=%v, nodes=[%d,%d]",
			config.Name, config.Type,
			config.ShardStrategy.CPUUtilizationRange.Min,
			config.ShardStrategy.CPUUtilizationRange.Max,
			config.ShardStrategy.PreferWarmupNodes,
			config.ShardStrategy.MinNodes,
			config.ShardStrategy.MaxNodes)
	}

	return nil
}

// abs returns absolute value of int
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// max returns maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
