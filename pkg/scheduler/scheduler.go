/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added dynamic configuration management with file watching and hot-reload capabilities
- Improved metrics collection and cache dumping functionality for better observability

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

package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/filewatcher"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/gate"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/deviceshare"
	"volcano.sh/volcano/pkg/scheduler/unschedulable"
)

// Scheduler represents a "Volcano Scheduler".
// Scheduler watches for new unscheduled pods(PodGroup) in Volcano.
// It attempts to find nodes that can accommodate these pods and writes the binding information back to the API server.
type Scheduler struct {
	cache                 schedcache.Cache
	unschedulableJobCache *unschedulable.JobCache
	schedulerConf         string
	fileWatcher           filewatcher.FileWatcher
	schedulePeriod        time.Duration
	once                  sync.Once

	mutex              sync.Mutex
	actions            []framework.Action
	plugins            []conf.Tier
	configurations     []conf.Configuration
	metricsConf        map[string]string
	dumper             schedcache.Dumper
	disableDefaultConf bool
	pluginsInitialized bool

	// schGateManager is used for async scheduling gate removal.
	schGateManager *gate.SchGateManager
}

// NewScheduler returns a Scheduler
func NewScheduler(config *rest.Config, opt *options.ServerOption) (*Scheduler, error) {
	var watcher filewatcher.FileWatcher
	if opt.SchedulerConf != "" {
		var err error
		path := filepath.Dir(opt.SchedulerConf)
		watcher, err = filewatcher.NewFileWatcher(path)
		if err != nil {
			return nil, fmt.Errorf("failed creating filewatcher for %s: %v", opt.SchedulerConf, err)
		}
	}

	metrics.SetUnschedulableJobCacheDebugMetricsEnabled(opt.UnschedulableJobCacheDebugMetrics)

	var unschedulableJobCache *unschedulable.JobCache
	var cacheOptions []schedcache.Option
	if utilfeature.DefaultFeatureGate.Enabled(features.UnschedulableJobCache) {
		unschedulableJobCache = unschedulable.NewJobCache(opt.UnschedulableJobCacheMaxSkipDuration)
		cacheOptions = append(cacheOptions, schedcache.WithUnschedulableJobCache(unschedulableJobCache))
	}
	cache := schedcache.New(config, opt.SchedulerNames, opt.DefaultQueue, opt.NodeSelector, opt.NodeWorkerThreads, opt.IgnoredCSIProvisioners, opt.ResyncPeriod, opt.ResourceSyncTimeout, cacheOptions...)
	scheduler := &Scheduler{
		schedulerConf:         opt.SchedulerConf,
		fileWatcher:           watcher,
		cache:                 cache,
		unschedulableJobCache: unschedulableJobCache,
		schedulePeriod:        opt.SchedulePeriod,
		dumper:                schedcache.Dumper{Cache: cache, RootDir: opt.CacheDumpFileDir},
		disableDefaultConf:    opt.DisableDefaultSchedulerConfig,
	}

	return scheduler, nil
}

// Run initializes and starts the Scheduler. It loads the configuration,
// initializes the cache, and begins the scheduling process.
func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	pc.loadSchedulerConf()

	if err := pc.initPlugins(); err != nil {
		klog.Fatalf("Failed to initialize plugins: %v", err)
	}

	// Start the gate manager (if the feature gate is enabled).
	if utilfeature.DefaultFeatureGate.Enabled(features.SchedulingGatesQueueAdmission) {
		pc.schGateManager = gate.NewSchGateManager(pc.cache.Client(), options.ServerOpts.GateRemovalWorkerNum)
		pc.schGateManager.Start()
		go func() {
			<-stopCh
			pc.schGateManager.Stop()
		}()
	}

	go pc.watchSchedulerConf(stopCh)
	// Start cache for policy.
	pc.cache.SetMetricsConf(pc.metricsConf)
	if pc.unschedulableJobCache != nil {
		pc.unschedulableJobCache.Run(stopCh)
	}
	pc.cache.Run(stopCh)
	klog.V(2).Infof("Scheduler completes Initialization and start to run")
	go wait.Until(pc.runOnce, pc.schedulePeriod, stopCh)
	if options.ServerOpts.EnableCacheDumper {
		pc.dumper.ListenForSignal(stopCh)
	}
	go runSchedulerSocket()
}

// runOnce executes a single scheduling cycle. This function is called periodically
// as defined by the Scheduler's schedule period.
func (pc *Scheduler) runOnce() {
	klog.V(4).Infof("Start scheduling ...")
	scheduleStartTime := time.Now()
	defer klog.V(4).Infof("End scheduling ...")

	pc.mutex.Lock()
	actions := pc.actions
	plugins := pc.plugins
	configurations := pc.configurations
	pc.mutex.Unlock()

	// Load ConfigMap to check which action is enabled.
	conf.EnabledActionMap = make(map[string]bool)
	for _, action := range actions {
		conf.EnabledActionMap[action.Name()] = true
	}

	ssn := framework.OpenSession(pc.cache, plugins, configurations, framework.WithUnschedulableJobCache(pc.unschedulableJobCache))
	ssn.SetSchGateManager(pc.schGateManager)
	defer func() {
		framework.CloseSession(ssn)
		metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
	}()

	for _, action := range actions {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}
}

// logLoadedSchedulerConf logs the scheduler configuration that was actually
// applied, line by line, to facilitate debugging.
func logLoadedSchedulerConf(confStr string) {
	klog.V(2).Infoln("Successfully loaded Scheduler conf as follows:")
	for _, line := range strings.Split(strings.TrimSpace(confStr), "\n") {
		klog.V(2).Infoln(line)
	}
}

func (pc *Scheduler) loadSchedulerConf() {
	klog.V(4).Infof("Start loadSchedulerConf ...")

	if pc.disableDefaultConf && len(pc.schedulerConf) == 0 {
		klog.Fatalf("No --scheduler-conf path provided and default configuration fallback is disabled")
	}

	var err error
	if !pc.disableDefaultConf {
		pc.once.Do(func() {
			pc.actions, pc.plugins, pc.configurations, pc.metricsConf, err = UnmarshalSchedulerConf(DefaultSchedulerConf)
			if err != nil {
				klog.Fatalf("Invalid default configuration: unmarshal Scheduler config %s failed: %v", DefaultSchedulerConf, err)
			}
			logLoadedSchedulerConf(DefaultSchedulerConf)
		})
	}

	var config string
	if len(pc.schedulerConf) != 0 {
		confData, err := os.ReadFile(pc.schedulerConf)
		if err != nil {
			if pc.disableDefaultConf {
				klog.Fatalf("Failed to read scheduler config and default configuration fallback is disabled")
			}
			klog.Errorf("Failed to read the Scheduler config in '%s', using previous configuration: %v",
				pc.schedulerConf, err)
			return
		}
		config = strings.TrimSpace(string(confData))
	}

	actions, plugins, configurations, metricsConf, err := UnmarshalSchedulerConf(config)
	if err != nil {
		if pc.disableDefaultConf {
			klog.Fatalf("Invalid scheduler configuration and default configuration fallback is disabled")
		}
		klog.Errorf("Scheduler config %s is invalid: %v", config, err)
		return
	}

	pc.mutex.Lock()
	if pc.pluginsInitialized {
		if err := validateDeviceShareReload(pc.plugins, plugins); err != nil {
			pc.mutex.Unlock()
			klog.Errorf("Failed to reload scheduler configuration: %v; retaining previously active configuration", err)
			return
		}
	}
	pc.actions = actions
	pc.plugins = plugins
	pc.configurations = configurations
	pc.metricsConf = metricsConf
	pc.mutex.Unlock()
	logLoadedSchedulerConf(config)
}

func (pc *Scheduler) initPlugins() error {
	pc.mutex.Lock()
	tiers := pc.plugins
	pc.mutex.Unlock()

	for _, tier := range tiers {
		for _, pluginConf := range tier.Plugins {
			pb, found := framework.GetPluginBuilder(pluginConf.Name)
			if !found {
				klog.Errorf("Failed to get plugin %s.", pluginConf.Name)
				continue
			}
			plugin := pb(pluginConf.Arguments)
			if initializer, ok := plugin.(framework.PluginInitializer); ok {
				if err := initializer.Init(); err != nil {
					return fmt.Errorf("failed to initialize plugin %s: %w", pluginConf.Name, err)
				}
			}
		}
	}

	pc.mutex.Lock()
	pc.pluginsInitialized = true
	pc.mutex.Unlock()
	return nil
}

func findPluginOptions(tiers []conf.Tier, name string) []conf.PluginOption {
	var opts []conf.PluginOption
	for _, tier := range tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name == name {
				opts = append(opts, plugin)
			}
		}
	}
	return opts
}

type deviceShareBackendConfig struct {
	enabledPredicate bool
	gpuSharing       bool
	gpuNumber        bool
	nodeLock         bool
	vgpu             bool
	mindCluster      bool
	hami             bool
	cmName           string
	cmNamespace      string
}

func extractDeviceShareBackendConfig(opt conf.PluginOption) deviceShareBackendConfig {
	cfg := deviceShareBackendConfig{
		enabledPredicate: opt.EnabledPredicate != nil && *opt.EnabledPredicate,
		cmName:           "volcano-vgpu-device-config",
		cmNamespace:      "kube-system",
	}
	args := framework.Arguments(opt.Arguments)
	args.GetBool(&cfg.gpuSharing, deviceshare.GPUSharingPredicate)
	args.GetBool(&cfg.gpuNumber, deviceshare.GPUNumberPredicate)
	args.GetBool(&cfg.nodeLock, deviceshare.NodeLockEnable)
	args.GetBool(&cfg.vgpu, deviceshare.VGPUEnable)
	args.GetBool(&cfg.mindCluster, deviceshare.AscendMindClusterVNPU)
	args.GetBool(&cfg.hami, deviceshare.AscendHAMiVNPUEnable)
	args.GetString(&cfg.cmName, deviceshare.KnownGeometriesCMName)
	args.GetString(&cfg.cmNamespace, deviceshare.KnownGeometriesCMNamespace)
	return cfg
}

func validateDeviceShareReload(currentTiers, newTiers []conf.Tier) error {
	currentOpts := findPluginOptions(currentTiers, deviceshare.PluginName)
	newOpts := findPluginOptions(newTiers, deviceshare.PluginName)

	if len(currentOpts) > 0 && len(newOpts) == 0 {
		return fmt.Errorf("live removal of DeviceShare plugin is unsupported without restarting the scheduler (cached node device state cannot be rebuilt dynamically)")
	}
	if len(currentOpts) == 0 && len(newOpts) > 0 {
		return fmt.Errorf("live addition of DeviceShare plugin is unsupported without restarting the scheduler (node cache was not initialized with device support)")
	}
	if len(currentOpts) == 0 && len(newOpts) == 0 {
		return nil
	}

	if len(currentOpts) != len(newOpts) {
		return fmt.Errorf("live modification of DeviceShare plugin declaration count (from %d to %d) is unsupported without restarting the scheduler", len(currentOpts), len(newOpts))
	}

	for i := range currentOpts {
		curCfg := extractDeviceShareBackendConfig(currentOpts[i])
		newCfg := extractDeviceShareBackendConfig(newOpts[i])

		if curCfg.enabledPredicate != newCfg.enabledPredicate {
			return fmt.Errorf("live modification of DeviceShare enablePredicate (from %v to %v) is unsupported without restarting the scheduler", curCfg.enabledPredicate, newCfg.enabledPredicate)
		}
		if curCfg.hami != newCfg.hami {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.AscendHAMiVNPUEnable, curCfg.hami, newCfg.hami)
		}
		if curCfg.gpuSharing != newCfg.gpuSharing {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.GPUSharingPredicate, curCfg.gpuSharing, newCfg.gpuSharing)
		}
		if curCfg.gpuNumber != newCfg.gpuNumber {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.GPUNumberPredicate, curCfg.gpuNumber, newCfg.gpuNumber)
		}
		if curCfg.vgpu != newCfg.vgpu {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.VGPUEnable, curCfg.vgpu, newCfg.vgpu)
		}
		if curCfg.mindCluster != newCfg.mindCluster {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.AscendMindClusterVNPU, curCfg.mindCluster, newCfg.mindCluster)
		}
		if curCfg.nodeLock != newCfg.nodeLock {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.NodeLockEnable, curCfg.nodeLock, newCfg.nodeLock)
		}
		if curCfg.cmName != newCfg.cmName {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.KnownGeometriesCMName, curCfg.cmName, newCfg.cmName)
		}
		if curCfg.cmNamespace != newCfg.cmNamespace {
			return fmt.Errorf("live modification of DeviceShare %s (from %v to %v) is unsupported without restarting the scheduler", deviceshare.KnownGeometriesCMNamespace, curCfg.cmNamespace, newCfg.cmNamespace)
		}
	}

	return nil
}

func (pc *Scheduler) watchSchedulerConf(stopCh <-chan struct{}) {
	if pc.fileWatcher == nil {
		return
	}
	eventCh := pc.fileWatcher.Events()
	errCh := pc.fileWatcher.Errors()
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			klog.V(4).Infof("watch %s event: %v", pc.schedulerConf, event)
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				pc.loadSchedulerConf()
				pc.cache.SetMetricsConf(pc.metricsConf)
			}
		case err, ok := <-errCh:
			if !ok {
				return
			}
			klog.Infof("watch %s error: %v", pc.schedulerConf, err)
		case <-stopCh:
			return
		}
	}
}
