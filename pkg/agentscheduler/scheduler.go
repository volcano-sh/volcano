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
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	schedcache "volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/conf"
	"volcano.sh/volcano/pkg/filewatcher"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// Scheduler represents a "Volcano Scheduler".
// Scheduler watches for new unscheduled pods(PodGroup) in Volcano.
// It attempts to find nodes that can accommodate these pods and writes the binding information back to the API server.
type Scheduler struct {
	agentCache     schedcache.Cache
	schedulerConf  string
	fileWatcher    filewatcher.FileWatcher
	schedulePeriod time.Duration
	once           sync.Once

	mutex              sync.Mutex
	configurations     []conf.Configuration
	metricsConf        map[string]string
	dumper             schedcache.Dumper
	disableDefaultConf bool
}

// NewScheduler returns a Scheduler
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

	cache := schedcache.New(config, opt.SchedulerNames, opt.DefaultQueue, opt.NodeSelector, opt.NodeWorkerThreads, opt.IgnoredCSIProvisioners, opt.ResyncPeriod)
	scheduler := &Scheduler{
		schedulerConf:      opt.SchedulerConf,
		fileWatcher:        watcher,
		agentCache:         cache,
		schedulePeriod:     opt.SchedulePeriod,
		dumper:             schedcache.Dumper{Cache: cache, RootDir: opt.CacheDumpFileDir},
		disableDefaultConf: opt.DisableDefaultSchedulerConfig,
	}

	return scheduler, nil
}

// Run initializes and starts the Scheduler. It loads the configuration,
// initializes the agentCache, and begins the scheduling process.
func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	pc.agentCache.Run(stopCh)
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
	// TODO
}

// 基础节点选择
func (pc *Scheduler) selectNode(task *api.TaskInfo, nodes map[string]*api.NodeInfo) *api.NodeInfo {
	for _, node := range nodes {
		// 基础的资源检查
		if node.Allocatable.LessEqual(task.Resreq, api.Zero) {
			continue
		}

		// 基础的状态检查
		if !node.Ready() {
			continue
		}

		return node
	}
	return nil
}

// 基础绑定
func (pc *Scheduler) bindTask(task *api.TaskInfo, node *api.NodeInfo) {
	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      task.Name,
			Namespace: task.Namespace,
		},
		Target: v1.ObjectReference{
			Kind: "Node",
			Name: node.Name,
		},
	}

	pc.agentCache.Client().CoreV1().Pods(task.Namespace).Bind(
		context.TODO(), binding, metav1.CreateOptions{})
}
