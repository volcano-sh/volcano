/*
Copyright 2017 The Kubernetes Authors.

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
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"volcano.sh/volcano/pkg/features"

	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/filewatcher"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// Scheduler represents a "Volcano Scheduler".
// Scheduler watches for new unscheduled pods(PodGroup) in Volcano.
// It attempts to find nodes that can accommodate these pods and writes the binding information back to the API server.
type Scheduler struct {
	cache          schedcache.Cache
	schedulerConf  string
	fileWatcher    filewatcher.FileWatcher
	schedulePeriod time.Duration
	once           sync.Once

	mutex             sync.Mutex
	PolicyMutex       sync.Mutex
	actions           []framework.Action
	plugins           []conf.Tier
	configurations    []conf.Configuration
	metricsConf       map[string]string
	dumper            schedcache.Dumper
	schedulerPolicies map[string]*framework.SchedulerPolicy
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

	cache := schedcache.New(config, opt.SchedulerNames, opt.DefaultQueue, opt.NodeSelector, opt.NodeWorkerThreads, opt.IgnoredCSIProvisioners)
	scheduler := &Scheduler{
		schedulerConf:  opt.SchedulerConf,
		fileWatcher:    watcher,
		cache:          cache,
		schedulePeriod: opt.SchedulePeriod,
		dumper:         schedcache.Dumper{Cache: cache, RootDir: opt.CacheDumpFileDir},
	}

	return scheduler, nil
}

// Run initializes and starts the Scheduler. It loads the configuration,
// initializes the cache, and begins the scheduling process.
func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	pc.loadSchedulerConf()
	go pc.watchSchedulerConf(stopCh)
	// Start cache for policy.
	pc.cache.SetMetricsConf(pc.metricsConf)
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

	ssn := framework.OpenSession(pc.cache, plugins, configurations, pc.schedulerPolicies)
	ssn.Actions = actions

	defer func() {
		framework.CloseSession(ssn)
		metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
	}()

	for _, action := range pc.UnionActions() {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}
}

func (pc *Scheduler) loadSchedulerConf() {
	klog.V(4).Infof("Start loading configurations from directory...")
	defer func() {
		actions, plugins := pc.getSchedulerConf()
		klog.V(2).Infof("Successfully loaded Scheduler conf, actions: %v, plugins: %v", actions, plugins)

		policyActions, policyPlugins := pc.getSchedulerPolicyConf()
		for policyName := range pc.schedulerPolicies {
			klog.V(2).Infof("Successfully loaded policy %s - actions: %v, plugins: %v",
				policyName, policyActions[policyName], policyPlugins[policyName])
		}
	}()

	// Read all files in the directory.
	files, err := os.ReadDir(filepath.Dir(pc.schedulerConf))
	if err != nil {
		klog.Errorf("Failed to read configuration directory: %v", err)
		return
	}

	// Default configuration is loaded first.
	pc.once.Do(func() {
		pc.actions, pc.plugins, pc.configurations, pc.metricsConf, err = UnmarshalSchedulerConf(DefaultSchedulerConf)
		if err != nil {
			klog.Errorf("unmarshal Scheduler config %s failed: %v", DefaultSchedulerConf, err)
			panic("invalid default configuration")
		}
	})

	policies := make(map[string]*framework.SchedulerPolicy)

	for _, file := range files {
		if file.IsDir() || strings.HasPrefix(file.Name(), ".") {
			continue
		}

		fileName := file.Name()
		filePath := filepath.Join(filepath.Dir(pc.schedulerConf), fileName)

		confData, err := os.ReadFile(filePath)
		if err != nil {
			klog.Errorf("Failed to read file %s: %v", filePath, err)
			continue
		}
		config := strings.TrimSpace(string(confData))

		actions, plugins, configurations, metricsConf, err := UnmarshalSchedulerConf(config)
		if err != nil {
			klog.Errorf("Scheduler config %s is invalid: %v", config, err)
			return
		}

		if fileName == "volcano-scheduler.conf" {
			// Update the global configuration.
			pc.mutex.Lock()
			pc.actions = actions
			pc.plugins = plugins
			pc.configurations = configurations
			pc.metricsConf = metricsConf
			pc.mutex.Unlock()
		} else {
			// Create SchedulerPolicy
			policy := framework.NewSchedulerPolicy()
			policy.Actions = actions
			policy.Tiers = plugins
			policies[fileName] = policy
		}
	}

	pc.PolicyMutex.Lock()
	pc.schedulerPolicies = policies
	pc.PolicyMutex.Unlock()
}

func (pc *Scheduler) getSchedulerConf() (actions []string, plugins []string) {
	for _, action := range pc.actions {
		actions = append(actions, action.Name())
	}
	for _, tier := range pc.plugins {
		for _, plugin := range tier.Plugins {
			plugins = append(plugins, plugin.Name)
		}
	}
	return
}

func (pc *Scheduler) getSchedulerPolicyConf() (map[string][]string, map[string][]string) {
	policyActions := make(map[string][]string)
	policyPlugins := make(map[string][]string)

	for policyName, policy := range pc.schedulerPolicies {
		var actions []string
		for _, action := range policy.Actions {
			actions = append(actions, action.Name())
		}
		policyActions[policyName] = actions

		var plugins []string
		for _, tier := range policy.Tiers {
			for _, plugin := range tier.Plugins {
				plugins = append(plugins, plugin.Name)
			}
		}
		policyPlugins[policyName] = plugins
	}

	return policyActions, policyPlugins
}

func (pc *Scheduler) watchSchedulerConf(stopCh <-chan struct{}) {
	if pc.fileWatcher == nil {
		return
	}
	eventCh := pc.fileWatcher.Events()
	errCh := pc.fileWatcher.Errors()
	var debounceTimer *time.Timer
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			klog.V(4).Infof("watch directory event: %v", event)

			// Reload all configurations on any file change.
			if event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Create == fsnotify.Create ||
				event.Op&fsnotify.Remove == fsnotify.Remove {
				// Apply debouncing to avoid frequent reloads on file change events.
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(1*time.Second, func() {
					pc.loadSchedulerConf()
					pc.cache.SetMetricsConf(pc.metricsConf)
					klog.V(4).Infof("Reloaded configurations after file change: %s", event.Name)
				})
			}

		case err, ok := <-errCh:
			if !ok {
				return
			}
			klog.Errorf("watch directory error: %v", err)
		case <-stopCh:
			return
		}
	}
}

// UnionActions obtains the union of global actions and schedulerPolicy actions.
func (pc *Scheduler) UnionActions() []framework.Action {
	if utilfeature.DefaultFeatureGate.Enabled(features.SchedulerPolicy) {
		var actions []framework.Action
		actionMap := make(map[string]framework.Action)

		for _, schedulerPolicy := range pc.schedulerPolicies {
			for _, action := range schedulerPolicy.Actions {
				actionMap[action.Name()] = action
			}
		}

		for _, action := range pc.actions {
			actionMap[action.Name()] = action
		}

		for _, action := range actionMap {
			actions = append(actions, action)
		}

		return actions
	}
	return pc.actions
}
