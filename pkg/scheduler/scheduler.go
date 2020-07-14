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
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// Scheduler watches for new unscheduled pods for volcano. It attempts to find
// nodes that they fit on and writes bindings back to the api server.
type Scheduler struct {
	cache          schedcache.Cache
	actions        []framework.Action
	plugins        []conf.Tier
	configurations []conf.Configuration
	schedulerConf  string
	schedulePeriod time.Duration
}

// NewScheduler returns a scheduler
func NewScheduler(
	config *rest.Config,
	schedulerName string,
	schedulerConf string,
	period time.Duration,
	defaultQueue string,
) (*Scheduler, error) {
	scheduler := &Scheduler{
		schedulerConf:  schedulerConf,
		cache:          schedcache.New(config, schedulerName, defaultQueue),
		schedulePeriod: period,
	}

	return scheduler, nil
}

// Run runs the Scheduler
func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	go wait.Until(pc.runOnce, pc.schedulePeriod, stopCh)
}

func (pc *Scheduler) runOnce() {
	klog.V(4).Infof("Start scheduling ...")
	scheduleStartTime := time.Now()
	defer klog.V(4).Infof("End scheduling ...")

	pc.loadSchedulerConf()

	ssn := framework.OpenSession(pc.cache, pc.plugins, pc.configurations)
	defer framework.CloseSession(ssn)

	for _, action := range pc.actions {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}
	metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
}

func (pc *Scheduler) loadSchedulerConf() {
	var err error

	// Load configuration of scheduler
	schedConf := defaultSchedulerConf
	if len(pc.schedulerConf) != 0 {
		if schedConf, err = readSchedulerConf(pc.schedulerConf); err != nil {
			klog.Errorf("Failed to read scheduler configuration '%s', using default configuration: %v",
				pc.schedulerConf, err)
			schedConf = defaultSchedulerConf
		}
	}

	pc.actions, pc.plugins, pc.configurations, err = loadSchedulerConf(schedConf)
	if err != nil {
		panic(err)
	}
}
