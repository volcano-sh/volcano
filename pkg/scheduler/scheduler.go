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
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	corev1 "k8s.io/api/core/v1"
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
	topologyconfig "volcano.sh/volcano/pkg/scheduler/topology/config"
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

	mutex                    sync.Mutex
	actions                  []framework.Action
	plugins                  []conf.Tier
	configurations           []conf.Configuration
	metricsConf              map[string]string
	deviceTopologyConf       *conf.DeviceTopologyConfiguration
	deviceTopologyConfFrozen bool
	dumper                   schedcache.Dumper
	disableDefaultConf       bool

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
	if err := pc.configureDeviceTopology(); err != nil {
		klog.Fatalf("Failed to configure device topology: %v", err)
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

	schedulerConfiguration, err := unmarshalSchedulerConfiguration(config)
	if err != nil {
		if pc.disableDefaultConf {
			klog.Fatalf("Invalid scheduler configuration and default configuration fallback is disabled")
		}
		klog.Errorf("Scheduler config %s is invalid: %v", config, err)
		return
	}

	actions, plugins, configurations, metricsConf, err := schedulerConfigurationRuntime(schedulerConfiguration)
	if err != nil {
		if pc.disableDefaultConf {
			klog.Fatalf("Invalid scheduler configuration and default configuration fallback is disabled")
		}
		klog.Errorf("Scheduler config %s is invalid: %v", config, err)
		return
	}
	deviceTopologyConf := cloneDeviceTopologyConfiguration(schedulerConfiguration.DeviceTopology)

	pc.mutex.Lock()
	topologyProviderChange := pc.deviceTopologyConfFrozen && !reflect.DeepEqual(pc.deviceTopologyConf, deviceTopologyConf)
	if !topologyProviderChange {
		pc.deviceTopologyConf = deviceTopologyConf
	}
	pc.actions = actions
	pc.plugins = plugins
	pc.configurations = configurations
	pc.metricsConf = metricsConf
	pc.mutex.Unlock()
	if topologyProviderChange {
		klog.Errorf("Ignoring deviceTopology configuration change; restart the scheduler to change topology providers or adapters")
		klog.V(2).Info("Loaded scheduler configuration while retaining the existing deviceTopology setting")
		return
	}
	logLoadedSchedulerConf(config)
}

// configureDeviceTopology builds the immutable provider and allocation adapter
// registries after scheduler configuration is loaded but before cache.Run
// starts informer processing. This configuration is startup-only in alpha.
func (pc *Scheduler) configureDeviceTopology() error {
	pc.mutex.Lock()
	configuration := cloneDeviceTopologyConfiguration(pc.deviceTopologyConf)
	pc.mutex.Unlock()
	if configuration == nil || (len(configuration.Providers) == 0 && len(configuration.Adapters) == 0) {
		pc.mutex.Lock()
		pc.deviceTopologyConfFrozen = true
		pc.mutex.Unlock()
		return nil
	}

	runtime, err := topologyconfig.NewProviderRuntime(configuration)
	if err != nil {
		return err
	}
	adapterRegistry, err := topologyconfig.NewAdapterRegistry(configuration)
	if err != nil {
		return err
	}
	cache, supported := pc.cache.(schedcache.DeviceTopologyProviderCache)
	if !supported {
		return fmt.Errorf("scheduler cache does not support device topology providers")
	}
	if err := cache.ConfigureDeviceTopologyProviders(runtime.Registry, runtime.Sources); err != nil {
		return err
	}
	if adapterRegistry != nil {
		adapterCache, supported := pc.cache.(schedcache.DeviceTopologyAdapterCache)
		if !supported {
			return fmt.Errorf("scheduler cache does not support device topology adapters")
		}
		if err := adapterCache.ConfigureDeviceTopologyAdapters(adapterRegistry); err != nil {
			return err
		}
	}

	pc.mutex.Lock()
	pc.deviceTopologyConfFrozen = true
	pc.mutex.Unlock()
	return nil
}

// cloneDeviceTopologyConfiguration returns an independent configuration so a
// watcher reload cannot mutate the startup-only provider ownership settings.
func cloneDeviceTopologyConfiguration(configuration *conf.DeviceTopologyConfiguration) *conf.DeviceTopologyConfiguration {
	if configuration == nil {
		return nil
	}

	clone := *configuration
	clone.Providers = make([]conf.DeviceTopologyProviderConfiguration, len(configuration.Providers))
	for index, provider := range configuration.Providers {
		clone.Providers[index] = provider
		clone.Providers[index].Resources = append([]corev1.ResourceName(nil), provider.Resources...)
	}
	clone.Adapters = append([]conf.DeviceTopologyAdapterConfiguration(nil), configuration.Adapters...)
	return &clone
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
