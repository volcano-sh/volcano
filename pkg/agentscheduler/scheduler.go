/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added dynamic configuration management with file watching and hot-reload capabilities
- Improved metrics collection and agentCache dumping functionality for better observability

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

package agentscheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	schedcache "volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/filewatcher"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	schedulermetrics "volcano.sh/volcano/pkg/scheduler/metrics"

	agentmetrics "volcano.sh/volcano/pkg/agentscheduler/metrics"
)

type configSnapshot struct {
	actions        []framework.Action
	tiers          []conf.Tier
	configurations []conf.Configuration
	version        uint64
}

// Scheduler represents a "Volcano Agent Scheduler".
// Scheduler watches for new unscheduled pods.
// It attempts to find nodes that can accommodate these pods and writes the binding information back to the API server.
type Scheduler struct {
	cache         schedcache.Cache
	schedulerConf string
	fileWatcher   filewatcher.FileWatcher
	once          sync.Once

	mutex              sync.Mutex
	actions            []framework.Action
	tiers              []conf.Tier
	configurations     []conf.Configuration
	metricsConf        map[string]string
	dumper             schedcache.Dumper
	disableDefaultConf bool
	workerCount        int
	shardingMode       string
	confSnapshot       *configSnapshot
}

func (sched *Scheduler) getConfSnapshot() *configSnapshot {
	sched.mutex.Lock()
	defer sched.mutex.Unlock()
	return sched.confSnapshot
}

type Worker struct {
	framework   *framework.Framework
	index       int
	sched       *Scheduler
	confVersion uint64
}

func (worker *Worker) updateFrameworkIfNeeded() {
	if worker.sched == nil {
		return
	}

	snap := worker.sched.getConfSnapshot()
	if snap == nil || snap.version == worker.confVersion {
		return
	}

	worker.framework = framework.NewFramework(snap.actions, snap.tiers, worker.sched.cache, snap.configurations)
	worker.confVersion = snap.version
}

// NewAgentScheduler returns a Scheduler
func NewAgentScheduler(config *rest.Config, opt *options.ServerOption) (*Scheduler, error) {
	var watcher filewatcher.FileWatcher
	if opt.SchedulerConf != "" {
		var err error
		path := filepath.Dir(opt.SchedulerConf)
		watcher, err = filewatcher.NewFileWatcher(path)
		if err != nil {
			return nil, fmt.Errorf("failed creating filewatcher for %s: %v", opt.SchedulerConf, err)
		}
	}
	cache := schedcache.New(config, opt)
	scheduler := &Scheduler{
		schedulerConf:      opt.SchedulerConf,
		fileWatcher:        watcher,
		cache:              cache,
		dumper:             schedcache.Dumper{Cache: cache, RootDir: opt.CacheDumpFileDir},
		disableDefaultConf: opt.DisableDefaultSchedulerConfig,
		workerCount:        int(opt.ScheduleWorkerCount),
		shardingMode:       opt.ShardingMode,
	}

	return scheduler, nil
}

// Run initializes and starts the Scheduler. It loads the configuration,
// initializes the cache, and begins the scheduling process.
func (sched *Scheduler) Run(stopCh <-chan struct{}) {
	sched.loadSchedulerConf()
	go sched.watchSchedulerConf(stopCh)
	// Start cache for policy.
	sched.cache.SetMetricsConf(sched.metricsConf)
	sched.cache.Run(stopCh)

	snap := sched.getConfSnapshot()
	klog.V(2).Infof("Scheduler completes Initialization and start to run %d workers", sched.workerCount)
	for i := range sched.workerCount {
		worker := &Worker{
			index:       i,
			sched:       sched,
			confVersion: snap.version,
		}
		worker.framework = framework.NewFramework(snap.actions, snap.tiers, sched.cache, snap.configurations)
		go func() {
			for {
				select {
				case <-stopCh:
					return
				default:
					worker.runOnce()
				}
			}
		}()
	}
	if options.ServerOpts.EnableCacheDumper {
		sched.dumper.ListenForSignal(stopCh)
	}

	go runSchedulerSocket()
}

// runOnce executes a single scheduling cycle. This function is called periodically
// as defined by the Scheduler's schedule period.
func (worker *Worker) runOnce() {
	klog.V(4).Infof("Start scheduling in worker %d ...", worker.index)
	defer klog.V(4).Infof("End scheduling in worker %d ...", worker.index)

	schedCtx, err := worker.generateNextSchedulingContext()
	if err != nil {
		klog.Errorf("Failed to get next task: %v", err)
		return
	}
	if schedCtx == nil {
		klog.Warningf("No task to schedule")
		return
	}

	worker.updateFrameworkIfNeeded()

	scheduleStartTime := time.Now()

	// Update snapshot from cache before scheduling
	snapshot := worker.framework.GetSnapshot()
	snapshotStart := time.Now()
	if err := worker.framework.Cache.UpdateSnapshot(snapshot); err != nil {
		klog.Errorf("Failed to update snapshot in worker %d: %v, skip this scheduling cycle", worker.index, err)
		queue := worker.framework.Cache.SchedulingQueue()
		if err := queue.AddUnschedulableIfNotPresent(klog.Background(), schedCtx.QueuedPodInfo, queue.SchedulingCycle()); err != nil {
			klog.ErrorS(err, "Failed to add pod back to scheduling queue",
				"pod", klog.KObj(schedCtx.QueuedPodInfo.Pod))
		}
		return
	}
	agentmetrics.UpdateUpdateSnapshotDuration(time.Since(snapshotStart))

	worker.framework.Cache.OnWorkerStartSchedulingCycle(worker.index, schedCtx)

	// TODO: Call OnCycleStart for all plugins
	// worker.framework.OnCycleStart()

	defer func() {
		agentmetrics.UpdateWorkerSchedulingCycleDuration(schedulermetrics.Duration(scheduleStartTime))
		// TODO: Call OnCycleEnd for all plugins
		// worker.framework.OnCycleEnd()
		worker.framework.Cache.OnWorkerEndSchedulingCycle(worker.index)
		worker.framework.ClearCycleState()
	}()

	for _, action := range worker.framework.Actions {
		actionStartTime := time.Now()
		action.Execute(worker.framework, schedCtx)
		schedulermetrics.UpdateActionDuration(action.Name(), schedulermetrics.Duration(actionStartTime))
	}
}

// generateNextSchedulingContext generates a new scheduling context for the next task to be scheduled.
func (worker *Worker) generateNextSchedulingContext() (*agentapi.SchedulingContext, error) {
	if worker.framework == nil {
		return nil, fmt.Errorf("framework is not initialized")
	}
	if worker.framework.Cache == nil {
		return nil, fmt.Errorf("cache is not initialized")
	}

	queue := worker.framework.Cache.SchedulingQueue()
	if queue == nil {
		return nil, fmt.Errorf("scheduling queue is not initialized")
	}

	podInfo, err := queue.Pop(klog.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to pop scheduling task: %w", err)
	}

	task, exist := worker.framework.Cache.GetTaskInfo(schedulingapi.TaskID(podInfo.Pod.UID))
	if !exist {
		klog.Warningf("Task %s/%s not found in cache, skip scheduling", podInfo.Pod.Namespace, podInfo.Pod.Name)
		queue.Done(podInfo.Pod.UID)
		return nil, nil
	}

	return &agentapi.SchedulingContext{
		Task:          task,
		QueuedPodInfo: podInfo,
	}, nil
}

func (sched *Scheduler) loadSchedulerConf() {
	klog.V(4).Infof("Start loadSchedulerConf ...")
	defer func() {
		actions, plugins := sched.getSchedulerConf()
		klog.V(2).Infof("Finished loading scheduler config. Final state: actions=%v, plugins=%v", actions, plugins)
	}()

	if sched.disableDefaultConf && len(sched.schedulerConf) == 0 {
		klog.Fatalf("No --scheduler-conf path provided and default configuration fallback is disabled")
	}

	var err error
	if !sched.disableDefaultConf {
		sched.once.Do(func() {
			var dActions []framework.Action
			var dTiers []conf.Tier
			var dConfigurations []conf.Configuration
			var dMetricsConf map[string]string
			dActions, dTiers, dConfigurations, dMetricsConf, err = UnmarshalSchedulerConf(DefaultSchedulerConf)
			if err != nil {
				klog.Fatalf("Invalid default configuration: unmarshal Scheduler config %s failed: %v", DefaultSchedulerConf, err)
			}
			for _, action := range dActions {
				action.OnActionInit(dConfigurations)
			}
			sched.actions = dActions
			sched.tiers = dTiers
			sched.configurations = dConfigurations
			sched.metricsConf = dMetricsConf
			sched.confSnapshot = &configSnapshot{
				actions:        dActions,
				tiers:          dTiers,
				configurations: dConfigurations,
				version:        1,
			}
		})
	}

	var config string
	if len(sched.schedulerConf) != 0 {
		confData, err := os.ReadFile(sched.schedulerConf)
		if err != nil {
			if sched.disableDefaultConf {
				klog.Fatalf("Failed to read scheduler config and default configuration fallback is disabled")
			}
			klog.Errorf("Failed to read the Scheduler config in '%s', using previous configuration: %v",
				sched.schedulerConf, err)
			return
		}
		config = strings.TrimSpace(string(confData))
	}

	actions, tiers, configurations, metricsConf, err := UnmarshalSchedulerConf(config)
	if err != nil {
		if sched.disableDefaultConf {
			klog.Fatalf("Invalid scheduler configuration and default configuration fallback is disabled")
		}
		klog.Errorf("Scheduler config %s is invalid: %v", config, err)
		return
	}

	for _, action := range actions {
		action.OnActionInit(configurations)
	}

	sched.mutex.Lock()
	sched.actions = actions
	sched.tiers = tiers
	sched.configurations = configurations
	sched.metricsConf = metricsConf
	newVersion := uint64(1)
	if sched.confSnapshot != nil {
		newVersion = sched.confSnapshot.version + 1
	}
	sched.confSnapshot = &configSnapshot{
		actions:        actions,
		tiers:          tiers,
		configurations: configurations,
		version:        newVersion,
	}
	sched.mutex.Unlock()

}

func (sched *Scheduler) getSchedulerConf() (actions []string, plugins []string) {
	for _, action := range sched.actions {
		actions = append(actions, action.Name())
	}
	for _, tier := range sched.tiers {
		for _, plugin := range tier.Plugins {
			plugins = append(plugins, plugin.Name)
		}
	}
	return
}

func (sched *Scheduler) watchSchedulerConf(stopCh <-chan struct{}) {
	if sched.fileWatcher == nil {
		return
	}
	eventCh := sched.fileWatcher.Events()
	errCh := sched.fileWatcher.Errors()
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			klog.V(4).Infof("watch %s event: %v", sched.schedulerConf, event)
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				sched.loadSchedulerConf()
				sched.cache.SetMetricsConf(sched.metricsConf)
			}
		case err, ok := <-errCh:
			if !ok {
				return
			}
			klog.Infof("watch %s error: %v", sched.schedulerConf, err)
		case <-stopCh:
			return
		}
	}
}
