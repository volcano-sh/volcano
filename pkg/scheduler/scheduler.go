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
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

type Scheduler struct {
	cache          schedcache.Cache
	config         *rest.Config
	actions        []framework.Action
	pluginArgs     []*framework.PluginArgs
	schedulerConf  string
	schedulePeriod time.Duration
}

func NewScheduler(
	config *rest.Config,
	schedulerName string,
	conf string,
	period string,
	nsAsQueue bool,
) (*Scheduler, error) {
	sp, _ := time.ParseDuration(period)
	scheduler := &Scheduler{
		config:         config,
		schedulerConf:  conf,
		cache:          schedcache.New(config, schedulerName, nsAsQueue),
		schedulePeriod: sp,
	}

	return scheduler, nil
}

func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	var err error

	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	// Load configuration of scheduler
	conf := defaultSchedulerConf
	if len(pc.schedulerConf) != 0 {
		if conf, err = pc.cache.LoadSchedulerConf(pc.schedulerConf); err != nil {
			glog.Errorf("Failed to load scheduler configuration '%s', using default configuration: %v",
				pc.schedulerConf, err)
		}
	}

	pc.actions, pc.pluginArgs = loadSchedulerConf(conf)

	go wait.Until(pc.runOnce, pc.schedulePeriod, stopCh)
}

func (pc *Scheduler) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	ssn := framework.OpenSession(pc.cache, pc.pluginArgs)
	defer framework.CloseSession(ssn)

	if glog.V(3) {
		glog.V(3).Infof("%v", ssn)
	}

	for _, action := range pc.actions {
		action.Execute(ssn)
	}

}
