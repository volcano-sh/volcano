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

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	schedcache "github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/conf"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/metrics"
)

// Scheduler watches for new unscheduled pods for kubebatch. It attempts to find
// nodes that they fit on and writes bindings back to the api server.
type Scheduler struct {
	cache          schedcache.Cache
	config         *rest.Config
	actions        []framework.Action
	plugins        []conf.Tier
	schedulerConf  string
	schedulePeriod time.Duration
}

// NewScheduler returns a scheduler
func NewScheduler(
	config *rest.Config,
	schedulerName string,
	conf string,
	period time.Duration,
	defaultQueue string,
) (*Scheduler, error) {
	scheduler := &Scheduler{
		config:         config,
		schedulerConf:  conf,
		cache:          schedcache.New(config, schedulerName, defaultQueue),
		schedulePeriod: period,
	}

	return scheduler, nil
}

// Run runs the Scheduler
func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	var err error

	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	// Load configuration of scheduler
	schedConf := defaultSchedulerConf
	if len(pc.schedulerConf) != 0 {
		if schedConf, err = readSchedulerConf(pc.schedulerConf); err != nil {
			glog.Errorf("Failed to read scheduler configuration '%s', using default configuration: %v",
				pc.schedulerConf, err)
			schedConf = defaultSchedulerConf
		}
	}

	pc.actions, pc.plugins, err = loadSchedulerConf(schedConf)
	if err != nil {
		panic(err)
	}

	go wait.Until(pc.runOnce, pc.schedulePeriod, stopCh)
}

func (pc *Scheduler) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	scheduleStartTime := time.Now()
	defer glog.V(4).Infof("End scheduling ...")
	defer metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))

	ssn := framework.OpenSession(pc.cache, pc.plugins)
	defer framework.CloseSession(ssn)

	for _, action := range pc.actions {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}
}
