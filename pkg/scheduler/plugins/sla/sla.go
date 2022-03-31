/*
Copyright 2021 The Volcano Authors.

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

package sla

import (
	"time"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin
	PluginName = "sla"
	// JobWaitingTime is maximum waiting time that a job could stay Pending in service level agreement
	// when job waits longer than waiting time, it should be inqueue at once, and cluster should reserve resources for it
	// Valid time units are “ns”, “us” (or “µs”), “ms”, “s”, “m”, “h”
	JobWaitingTime = "sla-waiting-time"
)

type slaPlugin struct {
	// Arguments given for sla plugin
	pluginArguments framework.Arguments
	jobWaitingTime  *time.Duration
}

// New function returns sla plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &slaPlugin{
		pluginArguments: arguments,
		jobWaitingTime:  nil,
	}
}

func (sp *slaPlugin) Name() string {
	return PluginName
}

// readJobWaitingTime read job waiting time from jobInfo or sla plugin arguments
// Valid time units are “ns”, “us” (or “µs”), “ms”, “s”, “m”, “h”
func (sp *slaPlugin) readJobWaitingTime(jwt *time.Duration) *time.Duration {
	// read individual jobInfo waiting time from jobInfos
	if jwt == nil {
		// if no individual settings, read global jobInfo waiting time from sla plugin arguments
		return sp.jobWaitingTime
	}
	return jwt
}

/*
User should give global job waiting time settings via sla plugin arguments:
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: sla
    arguments:
	  sla-waiting-time: 1h2m3s4ms5µs6ns

Meanwhile, use can give individual job waiting time settings for one job via job annotations:
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  annotations:
    sla-waiting-time: 1h2m3s4ms5us6ns
*/
func (sp *slaPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter sla plugin ...")
	defer klog.V(4).Infof("Leaving sla plugin.")

	// read in sla waiting time for global cluster from sla plugin arguments
	// if not set, job waiting time still can set in job yaml separately, otherwise job have no sla limits
	if _, exist := sp.pluginArguments[JobWaitingTime]; exist {
		waitTime, ok := sp.pluginArguments[JobWaitingTime].(string)
		if !ok {
			waitTime = ""
		}
		jwt, err := time.ParseDuration(waitTime)
		if err != nil {
			klog.Errorf("Error occurs in parsing global job waiting time in sla plugin, err: %s.", err.Error())
		}

		if jwt <= 0 {
			klog.Warningf("Invalid global waiting time setting: %s in sla plugin.", jwt.String())
		} else {
			sp.jobWaitingTime = &jwt
			klog.V(4).Infof("Global job waiting time is %s.", sp.jobWaitingTime.String())
		}
	}

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		var lJobWaitingTime = sp.readJobWaitingTime(lv.WaitingTime)
		var rJobWaitingTime = sp.readJobWaitingTime(rv.WaitingTime)

		if lJobWaitingTime == nil {
			if rJobWaitingTime == nil {
				return 0
			}
			return 1
		}
		if rJobWaitingTime == nil {
			return -1
		}

		lCreationTimestamp := lv.CreationTimestamp
		rCreationTimestamp := rv.CreationTimestamp
		if lCreationTimestamp.Add(*lJobWaitingTime).Before(rCreationTimestamp.Add(*rJobWaitingTime)) {
			return -1
		} else if lCreationTimestamp.Add(*lJobWaitingTime).After(rCreationTimestamp.Add(*rJobWaitingTime)) {
			return 1
		}
		return 0
	}
	ssn.AddJobOrderFn(sp.Name(), jobOrderFn)

	permitableFn := func(obj interface{}) int {
		jobInfo := obj.(*api.JobInfo)
		var jwt = sp.readJobWaitingTime(jobInfo.WaitingTime)

		if jwt == nil {
			return util.Abstain
		}

		if time.Since(jobInfo.CreationTimestamp.Time) < *jwt {
			return util.Abstain
		}

		return util.Permit
	}
	// if job waiting time is over, turn job to be inqueue in enqueue action
	ssn.AddJobEnqueueableFn(sp.Name(), permitableFn)
	// if job waiting time is over, turn job to be pipelined in allocate action
	ssn.AddJobPipelinedFn(sp.Name(), permitableFn)
}

func (sp *slaPlugin) OnSessionClose(ssn *framework.Session) {}
