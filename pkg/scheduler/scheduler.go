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
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// Scheduler watches for new unscheduled pods for volcano. It attempts to find
// nodes that they fit on and writes bindings back to the api server.
type Scheduler struct {
	cache              schedcache.Cache
	config             *rest.Config
	configFile         string
	schedulePeriod     time.Duration
	schedConfiguration *conf.SchedulerConf
}

// NewScheduler returns a scheduler
func NewScheduler(
	config *rest.Config,
	schedulerName string,
	configFile string,
	period time.Duration,
	defaultQueue string,
) (*Scheduler, error) {
	scheduler := &Scheduler{
		config:         config,
		configFile:     configFile,
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
	if len(pc.configFile) != 0 {
		if schedConf, err = readSchedulerConf(pc.configFile); err != nil {
			glog.Errorf("Failed to read scheduler configuration '%s', using default configuration: %v",
				pc.configFile, err)
			schedConf = defaultSchedulerConf
		}
	}

	// load configuration
	pc.schedConfiguration, err = loadSchedulerConf(schedConf)
	if err != nil {
		panic(err)
	}

	// init all actions
	pc.initActions()

	go wait.Until(pc.runOnce, pc.schedulePeriod, stopCh)
}

func (pc *Scheduler) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	scheduleStartTime := time.Now()
	defer glog.V(4).Infof("End scheduling ...")
	defer metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))

	ssn := framework.OpenSession(pc.cache, *pc.schedConfiguration)
	defer framework.CloseSession(ssn)

	if pc.schedConfiguration.Version == framework.SchedulerConfigVersion1 {
		actionNames := strings.Split(pc.schedConfiguration.V1Conf.Actions, ",")
		for _, actionName := range actionNames {
			if action, found := framework.GetAction(strings.TrimSpace(actionName)); found {
				actionStartTime := time.Now()
				action.Execute(ssn)
				metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
			}
		}
	} else if pc.schedConfiguration.Version == framework.SchedulerConfigVersion2 {

		for _, actionOpt := range pc.schedConfiguration.V2Conf.Actions {

			if action, found := framework.GetAction(strings.TrimSpace(actionOpt.Name)); found {
				actionStartTime := time.Now()
				action.Execute(ssn)
				metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
			}
		}
	}
}

func (pc *Scheduler) initActions() {
	if pc.schedConfiguration.Version == framework.SchedulerConfigVersion1 {
		actionNames := strings.Split(pc.schedConfiguration.V1Conf.Actions, ",")
		for _, actionName := range actionNames {
			if action, found := framework.GetAction(strings.TrimSpace(actionName)); found {
				action.Initialize(pc.schedConfiguration)
			}
		}
	} else if pc.schedConfiguration.Version == framework.SchedulerConfigVersion2 {
		for _, actionOpt := range pc.schedConfiguration.V2Conf.Actions {
			if action, found := framework.GetAction(strings.TrimSpace(actionOpt.Name)); found {
				action.Initialize(pc.schedConfiguration)
			}
		}
	}
}
