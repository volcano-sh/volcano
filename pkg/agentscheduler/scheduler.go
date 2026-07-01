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
	workers            []*Worker
	configApplied      bool
}

type Worker struct {
	mutex     sync.RWMutex
	framework *framework.Framework
	index     int
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
	// Start cache for policy.
	sched.cache.SetMetricsConf(sched.getMetricsConf())
	sched.cache.Run(stopCh)

	klog.V(2).Infof("Scheduler completes Initialization and start to run %d workers", sched.workerCount)
	for i := range sched.workerCount {
		worker := sched.newWorker(i)
		sched.addWorker(worker)
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
	go sched.watchSchedulerConf(stopCh)
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

	worker.mutex.RLock()
	defer worker.mutex.RUnlock()
	if worker.framework == nil {
		klog.Errorf("Framework is not initialized in worker %d", worker.index)
		return
	}
	fwk := worker.framework

	scheduleStartTime := time.Now()

	// Update snapshot from cache before scheduling
	snapshot := fwk.GetSnapshot()
	snapshotStart := time.Now()
	if err := fwk.Cache.UpdateSnapshot(snapshot); err != nil {
		klog.Errorf("Failed to update snapshot in worker %d: %v, skip this scheduling cycle", worker.index, err)
		return
	}
	agentmetrics.UpdateUpdateSnapshotDuration(time.Since(snapshotStart))

	fwk.Cache.OnWorkerStartSchedulingCycle(worker.index, schedCtx)

	// TODO: Call OnCycleStart for all plugins
	// fwk.OnCycleStart()

	defer func() {
		agentmetrics.UpdateWorkerSchedulingCycleDuration(schedulermetrics.Duration(scheduleStartTime))
		// TODO: Call OnCycleEnd for all plugins
		// fwk.OnCycleEnd()
		fwk.Cache.OnWorkerEndSchedulingCycle(worker.index)
		fwk.ClearCycleState()
	}()

	for _, action := range fwk.Actions {
		actionStartTime := time.Now()
		action.Execute(fwk, schedCtx)
		schedulermetrics.UpdateActionDuration(action.Name(), schedulermetrics.Duration(actionStartTime))
	}
}

func (sched *Scheduler) newWorker(index int) *Worker {
	actions, tiers, configurations := sched.getFrameworkConfig()
	return &Worker{
		index:     index,
		framework: framework.NewFramework(actions, tiers, sched.cache, configurations),
	}
}

func (sched *Scheduler) addWorker(worker *Worker) {
	sched.mutex.Lock()
	defer sched.mutex.Unlock()
	sched.workers = append(sched.workers, worker)
}

func (sched *Scheduler) getFrameworkConfig() ([]framework.Action, []conf.Tier, []conf.Configuration) {
	sched.mutex.Lock()
	defer sched.mutex.Unlock()
	return sched.actions, sched.tiers, sched.configurations
}

func (sched *Scheduler) getMetricsConf() map[string]string {
	sched.mutex.Lock()
	defer sched.mutex.Unlock()
	return sched.metricsConf
}

func (sched *Scheduler) getWorkers() []*Worker {
	sched.mutex.Lock()
	defer sched.mutex.Unlock()
	workers := make([]*Worker, len(sched.workers))
	copy(workers, sched.workers)
	return workers
}

// generateNextSchedulingContext generates a new scheduling context for the next task to be scheduled.
func (worker *Worker) generateNextSchedulingContext() (*agentapi.SchedulingContext, error) {
	worker.mutex.RLock()
	var cache schedcache.Cache
	if worker.framework != nil {
		cache = worker.framework.Cache
	}
	worker.mutex.RUnlock()

	if cache == nil {
		return nil, fmt.Errorf("framework is not initialized")
	}

	queue := cache.SchedulingQueue()
	if queue == nil {
		return nil, fmt.Errorf("scheduling queue is not initialized")
	}

	podInfo, err := queue.Pop(klog.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to pop scheduling task: %w", err)
	}
	if podInfo == nil || podInfo.Pod == nil {
		return nil, nil
	}

	task, exist := cache.GetTaskInfo(schedulingapi.TaskID(podInfo.Pod.UID))
	if !exist {
		klog.Warningf("Task %s/%s not found in cache, skip scheduling", podInfo.Pod.Namespace, podInfo.Pod.Name)
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
			sched.actions, sched.tiers, sched.configurations, sched.metricsConf, err = UnmarshalSchedulerConf(DefaultSchedulerConf)
			if err != nil {
				klog.Fatalf("Invalid default configuration: unmarshal Scheduler config %s failed: %v", DefaultSchedulerConf, err)
			}
		})
		if len(sched.schedulerConf) == 0 {
			sched.applySchedulerConf(sched.actions, sched.tiers, sched.configurations, sched.metricsConf)
			return
		}
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
			sched.applyDefaultConfIfNeeded()
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
		sched.applyDefaultConfIfNeeded()
		return
	}

	sched.applySchedulerConf(actions, tiers, configurations, metricsConf)
}

func (sched *Scheduler) applyDefaultConfIfNeeded() {
	sched.mutex.Lock()
	applied := sched.configApplied
	actions := sched.actions
	tiers := sched.tiers
	configurations := sched.configurations
	metricsConf := sched.metricsConf
	sched.mutex.Unlock()

	if applied || len(actions) == 0 {
		return
	}
	sched.applySchedulerConf(actions, tiers, configurations, metricsConf)
}

func (sched *Scheduler) applySchedulerConf(actions []framework.Action, tiers []conf.Tier, configurations []conf.Configuration, metricsConf map[string]string) {
	workers := sched.getWorkers()
	for _, worker := range workers {
		worker.mutex.Lock()
	}
	defer func() {
		for i := len(workers) - 1; i >= 0; i-- {
			workers[i].mutex.Unlock()
		}
	}()

	for _, action := range actions {
		action.OnActionInit(configurations)
	}

	sched.mutex.Lock()
	sched.actions = actions
	sched.tiers = tiers
	sched.configurations = configurations
	sched.metricsConf = metricsConf
	sched.configApplied = true
	sched.mutex.Unlock()

	for _, worker := range workers {
		worker.framework = framework.NewFramework(actions, tiers, sched.cache, configurations)
	}
}

func (sched *Scheduler) getSchedulerConf() (actions []string, plugins []string) {
	sched.mutex.Lock()
	defer sched.mutex.Unlock()

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
	defer sched.fileWatcher.Close()
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
				sched.cache.SetMetricsConf(sched.getMetricsConf())
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
