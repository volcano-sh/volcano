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

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
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
	defaultShardSyncPeriod      = 120 * time.Second
	maxAssignmentCacheRetention = 5 * time.Minute
	nodeUsageChangeThreshold    = 0.5
	nodeRefreshPeriod           = 80 * time.Second
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

	nodeInformer      k8sinformerv1.NodeInformer
	podInformer       k8sinformerv1.PodInformer
	shardInformer     shardinformers.NodeShardInformer
	configMapInformer k8sinformerv1.ConfigMapInformer

	nodeLister               k8slisterv1.NodeLister
	podLister                k8slisterv1.PodLister
	shardLister              shardlisters.NodeShardLister
	configMapLister          k8slisterv1.ConfigMapNamespaceLister
	configMapInformerFactory informers.SharedInformerFactory

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

	// configMapName / configMapNamespace identify the ConfigMap used for
	// live-reloadable sharding configurations.
	configMapName      string
	configMapNamespace string
	// configMu guards scheduler config updates triggered by ConfigMap changes.
	configMu sync.RWMutex
	// shardSyncPeriodUpdateCh tells the periodic sync loop to reset its timer
	// after a live config update.
	shardSyncPeriodUpdateCh chan struct{}
	// flagsRegistered tracks whether AddFlags was called (production path).
	// When false, Initialize creates default options instead.
	flagsRegistered bool
}

// Return the name of the controller
func (sc *ShardingController) Name() string {
	return controllerName
}

// AddFlags implements framework.FlagProvider, registering controller-specific
// flags before the binary's flag set is parsed.
func (sc *ShardingController) AddFlags(fs *pflag.FlagSet) {
	sc.controllerOptions = NewShardingControllerOptions()
	sc.controllerOptions.AddFlags(fs)
	sc.flagsRegistered = true
}

// Initialize initializes the controller
func (sc *ShardingController) Initialize(opt *framework.ControllerOption) error {
	klog.V(2).Infof("Initializing ShardingController...")
	sc.ctx = context.Background()
	// When AddFlags was called (production), re-parse the raw config strings
	// that may have been overridden by command-line flags.
	// When it was not called (tests), initialize options with defaults.
	if sc.flagsRegistered {
		if err := sc.controllerOptions.ParseConfig(); err != nil {
			klog.Warningf("Failed to parse scheduler configs from flags: %v", err)
		}
	} else {
		sc.controllerOptions = NewShardingControllerOptions()
	}

	sc.kubeClient = opt.KubeClient
	sc.vcClient = opt.VolcanoClient

	sc.vcInformerFactory = opt.VCSharedInformerFactory
	sc.kubeInformerFactory = opt.SharedInformerFactory

	sc.configMapName = sc.controllerOptions.ConfigMapName
	sc.configMapNamespace = sc.controllerOptions.ConfigMapNamespace

	// Initialize informers
	sc.nodeInformer = sc.kubeInformerFactory.Core().V1().Nodes()
	sc.podInformer = sc.kubeInformerFactory.Core().V1().Pods()
	sc.shardInformer = sc.vcInformerFactory.Shard().V1alpha1().NodeShards()

	// Create a namespace+name-scoped informer factory so only the single
	// sharding ConfigMap is cached, not all ConfigMaps cluster-wide.
	// Only set up ConfigMap watching when both name and namespace are provided.
	if sc.configMapName != "" && sc.configMapNamespace != "" {
		sc.configMapInformerFactory = informers.NewSharedInformerFactoryWithOptions(
			sc.kubeClient,
			0,
			informers.WithNamespace(sc.configMapNamespace),
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", sc.configMapName).String()
			}),
		)
		sc.configMapInformer = sc.configMapInformerFactory.Core().V1().ConfigMaps()
		sc.configMapLister = sc.configMapInformer.Lister().ConfigMaps(sc.configMapNamespace)
	}

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

	sc.assignmentCache = &AssignmentCache{
		Assignments: make(map[string]*ShardAssignment),
	}
	sc.shardSyncPeriodUpdateCh = make(chan struct{}, 1)

	// Only parse flag-based scheduler configs when no ConfigMap is configured.
	// When a ConfigMap is set, its contents will be loaded in Run(). If the
	// ConfigMap is not found at that point, Run() falls back to flag-based
	// defaults.
	if sc.configMapName == "" {
		sc.parseSchedulerConfigsFromOptions()
	}

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

	// Watch the sharding ConfigMap for live configuration updates.
	if sc.configMapInformer != nil {
		sc.configMapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: sc.isShardingConfigMap,
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.onConfigMapAdd,
				UpdateFunc: sc.onConfigMapUpdate,
				DeleteFunc: sc.onConfigMapDelete,
			},
		})
	}

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

	cacheSyncs := []cache.InformerSynced{
		sc.nodeInformer.Informer().HasSynced,
		sc.shardInformer.Informer().HasSynced,
		sc.podInformer.Informer().HasSynced,
	}

	if sc.configMapInformerFactory != nil {
		sc.configMapInformerFactory.Start(stopCh)
		cacheSyncs = append(cacheSyncs, sc.configMapInformer.Informer().HasSynced)
	}

	klog.Infof("Waiting for cache synchronization...")
	if !cache.WaitForCacheSync(stopCh, cacheSyncs...) {
		klog.Errorf("cache sync failed")
	}
	klog.Infof("Cache synchronization completed successfully")

	// When a ConfigMap is configured, load scheduler configs from it.
	// If the ConfigMap does not exist yet or fails to load, fall back to
	// the deprecated flag-based scheduler configs.
	if sc.configMapName != "" {
		loaded, err := sc.loadConfigFromConfigMap()
		if err != nil {
			klog.Warningf("Could not load sharding config from ConfigMap %s/%s: %v – falling back to flag-based defaults",
				sc.configMapNamespace, sc.configMapName, err)
		}
		if !loaded {
			klog.Infof("Sharding ConfigMap %s/%s not found – using flag-based defaults until ConfigMap is created",
				sc.configMapNamespace, sc.configMapName)
			sc.configMu.Lock()
			sc.parseSchedulerConfigsFromOptions()
			sc.shardingManager = NewShardingManager(sc.schedulerConfigs, sc)
			sc.configMu.Unlock()
		}
	}

	// Initialize node metrics
	sc.refreshNodeMetrics()

	// Initial sync
	sc.syncShards()

	// Start workers
	go wait.Until(sc.worker, time.Second, stopCh)
	go wait.Until(sc.nodeEventWorker, time.Second, stopCh)

	// Start periodic sync
	go sc.periodicShardSync(stopCh)

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
		applyPolicyDefaults(&configSpec)

		config := SchedulerConfig{
			Name:            configSpec.Name,
			Type:            configSpec.Type,
			PolicyName:      configSpec.Policy,
			PolicyArguments: configSpec.Arguments,
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
		klog.Infof("Added scheduler config: %s with policy %s", config.Name, config.PolicyName)
	}

	klog.Infof("Initialized with %d scheduler configurations", len(sc.schedulerConfigs))
	for _, config := range sc.schedulerConfigs {
		klog.Infof("  Scheduler %s (%s): policy=%s, args=%v",
			config.Name, config.Type, config.PolicyName, config.PolicyArguments)
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

// syncShards performs global shard assignment calculation
func (sc *ShardingController) syncShards() {
	klog.Infof("Starting global shard synchronization")
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		klog.V(3).Infof("Completed global shard synchronization in %v", duration)
	}()

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

	// Snapshot the shardingManager under RLock so that a concurrent config
	// reload cannot swap the pointer out from under us mid-calculation.
	sc.configMu.RLock()
	mgr := sc.shardingManager
	sc.configMu.RUnlock()

	// Calculate new assignments
	newAssignments, err := mgr.CalculateShardAssignments(nodes, currentShards)
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

	// Clean up stale NodeShards for schedulers that are no longer configured.
	sc.cleanupStaleNodeShards(currentShards, newAssignments)

	klog.Infof("Global shard sync completed: %d schedulers, %d nodes",
		len(newAssignments), len(nodes))
}

// cleanupStaleNodeShards deletes NodeShards that belong to schedulers no longer
// present in the current configuration.
func (sc *ShardingController) cleanupStaleNodeShards(currentShards []*shardv1alpha1.NodeShard, activeAssignments map[string]*ShardAssignment) {
	for _, shard := range currentShards {
		if _, active := activeAssignments[shard.Name]; active {
			continue
		}
		klog.Infof("Deleting stale NodeShard %s (scheduler no longer configured)", shard.Name)
		if err := sc.vcClient.ShardV1alpha1().NodeShards().Delete(sc.ctx, shard.Name, metav1.DeleteOptions{}); err != nil {
			if !errors.IsNotFound(err) {
				klog.Errorf("Failed to delete stale NodeShard %s: %v", shard.Name, err)
			}
		}
	}
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

	klog.V(6).Infof("Assignment changed for %s: %d -> %d nodes", schedulerName, len(shard.Spec.NodesDesired), len(assignment.NodesDesired))

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
	sc.configMu.RLock()
	defer sc.configMu.RUnlock()
	for _, config := range sc.schedulerConfigs {
		if config.Name == name {
			c := config
			return &c
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
		AllNodes:      nodes,
		AssignedNodes: make(map[string]string),
	}

	// Snapshot manager under RLock.
	sc.configMu.RLock()
	mgr := sc.shardingManager
	sc.configMu.RUnlock()

	assignment, err := mgr.calculateSingleSchedulerAssignment(*schedulerConfig, ctx)
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

// isShardingConfigMap returns true when the object is the sharding ConfigMap
// that this controller watches.
func (sc *ShardingController) isShardingConfigMap(obj interface{}) bool {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		// Handle tombstone objects from the Delete event.
		tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !isTombstone {
			return false
		}
		cm, ok = tombstone.Obj.(*corev1.ConfigMap)
		if !ok {
			return false
		}
	}
	return cm.Name == sc.configMapName && cm.Namespace == sc.configMapNamespace
}

// onConfigMapAdd reacts to the sharding ConfigMap being created.
func (sc *ShardingController) onConfigMapAdd(obj interface{}) {
	cm := obj.(*corev1.ConfigMap)
	klog.Infof("Sharding ConfigMap %s/%s added; reloading scheduler configs", cm.Namespace, cm.Name)
	sc.reloadFromConfigMap(cm)
}

// onConfigMapUpdate reacts to the sharding ConfigMap being modified.
func (sc *ShardingController) onConfigMapUpdate(oldObj, newObj interface{}) {
	old := oldObj.(*corev1.ConfigMap)
	cur := newObj.(*corev1.ConfigMap)
	if old.ResourceVersion == cur.ResourceVersion {
		return
	}
	klog.Infof("Sharding ConfigMap %s/%s updated; reloading scheduler configs", cur.Namespace, cur.Name)
	sc.reloadFromConfigMap(cur)
}

// onConfigMapDelete logs a warning when the sharding ConfigMap is removed.
// The controller continues to use the last successfully loaded configuration.
func (sc *ShardingController) onConfigMapDelete(obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		if tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown); isTombstone {
			cm, ok = tombstone.Obj.(*corev1.ConfigMap)
		}
	}
	if ok {
		klog.Warningf("Sharding ConfigMap %s/%s deleted; retaining last known scheduler configs", cm.Namespace, cm.Name)
	}
}

// reloadFromConfigMap parses a ConfigMap and updates the in-memory scheduler
// configs, then triggers a full shard re-sync.
func (sc *ShardingController) reloadFromConfigMap(cm *corev1.ConfigMap) {
	data, ok := cm.Data[ConfigMapDataKey]
	if !ok {
		klog.Warningf("Sharding ConfigMap %s/%s does not contain key %q – skipping reload",
			cm.Namespace, cm.Name, ConfigMapDataKey)
		return
	}
	cfg, err := ParseShardingConfig([]byte(data))
	if err != nil {
		klog.Errorf("Failed to parse sharding config from ConfigMap %s/%s: %v – keeping previous config",
			cm.Namespace, cm.Name, err)
		return
	}
	sc.applyShardingConfig(cfg)
	// Invalidate assignment cache.
	sc.cacheMutex.Lock()
	sc.assignmentCache = &AssignmentCache{Assignments: make(map[string]*ShardAssignment)}
	sc.cacheMutex.Unlock()
	// Enqueue all scheduler shards for resync via the work queue (non-blocking,
	// avoids holding up the informer event handler goroutine).
	sc.configMu.RLock()
	configs := make([]SchedulerConfig, len(sc.schedulerConfigs))
	copy(configs, sc.schedulerConfigs)
	sc.configMu.RUnlock()
	for _, c := range configs {
		sc.enqueueShard(c.Name)
	}
}

// loadConfigFromConfigMap fetches the sharding ConfigMap from the lister cache
// and applies it.  Returns (true, nil) when the ConfigMap was found and applied,
// (false, nil) when it does not exist yet, or (false, err) on other failures.
func (sc *ShardingController) loadConfigFromConfigMap() (bool, error) {
	if sc.configMapLister == nil || sc.configMapName == "" {
		return false, nil
	}
	cm, err := sc.configMapLister.Get(sc.configMapName)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	sc.reloadFromConfigMap(cm)
	return true, nil
}

// applyShardingConfig updates the controller's schedulerConfigs and
// shardingManager from a parsed ShardingConfig.  It acquires configMu to
// prevent races with concurrent reads.
func (sc *ShardingController) applyShardingConfig(cfg *ShardingConfig) {
	newConfigs := make([]SchedulerConfig, 0, len(cfg.SchedulerConfigs))
	for _, spec := range cfg.SchedulerConfigs {
		applyPolicyDefaults(&spec)

		newConfigs = append(newConfigs, SchedulerConfig{
			Name:            spec.Name,
			Type:            spec.Type,
			PolicyName:      spec.Policy,
			PolicyArguments: spec.Arguments,
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
		})
	}

	periodChanged := false
	sc.configMu.Lock()
	sc.schedulerConfigs = newConfigs
	sc.shardingManager = NewShardingManager(newConfigs, sc)
	if cfg.ShardSyncPeriod != "" {
		if d, err := time.ParseDuration(cfg.ShardSyncPeriod); err == nil {
			if sc.shardSyncPeriod != d {
				periodChanged = true
			}
			sc.shardSyncPeriod = d
			klog.Infof("ShardingController: shardSyncPeriod updated to %v from ConfigMap", d)
		} else {
			klog.Warningf("ShardingController: invalid shardSyncPeriod %q in ConfigMap: %v", cfg.ShardSyncPeriod, err)
		}
	}
	if cfg.EnableNodeEventTrigger != nil {
		sc.controllerOptions.EnableNodeEventTrigger = *cfg.EnableNodeEventTrigger
	}
	sc.configMu.Unlock()

	if periodChanged {
		select {
		case sc.shardSyncPeriodUpdateCh <- struct{}{}:
		default:
		}
		klog.V(4).Infof("ShardingController: notified periodic sync loop of new shardSyncPeriod %v", sc.getShardSyncPeriod())
	}

	klog.Infof("ShardingController: applied %d scheduler configs from ConfigMap:", len(newConfigs))
	for _, c := range newConfigs {
		klog.Infof("  %s (%s): policy=%s, CPU [%.2f, %.2f], warmup=%v, nodes=[%d,%d]",
			c.Name, c.Type, c.PolicyName,
			c.ShardStrategy.CPUUtilizationRange.Min,
			c.ShardStrategy.CPUUtilizationRange.Max,
			c.ShardStrategy.PreferWarmupNodes,
			c.ShardStrategy.MinNodes,
			c.ShardStrategy.MaxNodes)
	}
}

func (sc *ShardingController) getShardSyncPeriod() time.Duration {
	sc.configMu.RLock()
	defer sc.configMu.RUnlock()
	if sc.shardSyncPeriod <= 0 {
		return defaultShardSyncPeriod
	}
	return sc.shardSyncPeriod
}

func (sc *ShardingController) periodicShardSync(stopCh <-chan struct{}) {
	timer := time.NewTimer(sc.getShardSyncPeriod())
	defer timer.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-sc.shardSyncPeriodUpdateCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(sc.getShardSyncPeriod())
		case <-timer.C:
			sc.syncShards()
			timer.Reset(sc.getShardSyncPeriod())
		}
	}
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
