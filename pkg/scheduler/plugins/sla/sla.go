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
)

const (
	// PluginName indicates name of volcano scheduler plugin
	PluginName = "sla"
	// JobWaitingTime is maximum waiting time that a job could stay Pending in service level agreement
	// when job waits longer than waiting time, it should be inqueue at once, and cluster should reserve resources for it
	JobWaitingTime = "SLA-WaitingTime"
)

type slaPlugin struct {
	// Arguments given for sla plugin
	pluginArguments framework.Arguments
	jobWaitingTime  time.Duration
}

// New function returns sla plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &slaPlugin{
		pluginArguments: arguments,
		jobWaitingTime:  time.Duration(-1),
	}
}

func (sp *slaPlugin) Name() string {
	return PluginName
}

// readJobWaitingTime read in job waiting time for job
// TODO: should also read from given field in volcano job spec
func (sp *slaPlugin) readJobWaitingTime(jobInfo *api.JobInfo) (time.Duration, error) {
	// read individual jobInfo waiting time from jobInfo annotations
	if _, exist := jobInfo.PodGroup.Annotations[JobWaitingTime]; !exist {
		klog.V(5).Infof("no individual jobInfo waiting time settings for jobInfo <%s/%s>", jobInfo.Namespace, jobInfo.Name)
		// if no individual settings, read global jobInfo waiting time from sla plugin arguments
		return sp.jobWaitingTime, nil
	}

	jobWaitingTime, err := time.ParseDuration(jobInfo.PodGroup.Annotations[JobWaitingTime])
	if err != nil {
		klog.Errorf("error occurs in parsing jobInfo waiting time for jobInfo <%s/%s>, err: %s",
			jobInfo.Namespace, jobInfo.Name, err.Error())
		return time.Duration(-1), err
	}
	if jobWaitingTime < 0 {
		klog.Warningf("invalid individual jobInfo waiting time settings: %f minutes for jobInfo <%s/%s>",
			jobWaitingTime.Minutes(), jobInfo.Namespace, jobInfo.Name)
		return time.Duration(-1), nil
	}
	return jobWaitingTime, nil
}

/*
   User should give global job waiting time settings via sla plugin arguments:

   actions: "enqueue, allocate, backfill"
   tiers:
   - plugins:
     - name: sla
       arguments:
         SLA-WaitingTime: 1h

   Meanwhile, use can give individual job waiting time settings for one job via job annotations:

    apiVersion: batch.volcano.sh/v1alpha1
    kind: Job
    metadata:
      annotations:
        SLA-WaitingTime: 30m
*/
func (sp *slaPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter sla plugin ...")
	defer klog.V(4).Infof("Leaving sla plugin.")

	// read in job waiting time of global cluster
	jwt, err := time.ParseDuration(sp.pluginArguments[JobWaitingTime])
	if err != nil {
		klog.Errorf("error occurs in parsing global job waiting time in sla plugin, err: %s", err.Error())
		return
	}
	sp.jobWaitingTime = jwt
	if sp.jobWaitingTime < 0 {
		// if not set, job waiting time should be set in yaml separately, otherwise job have no sla limits
		klog.V(4).Infof("No valid global job waiting time settings in sla plugin.")
	} else {
		klog.V(4).Infof("global job waiting time is %f minutes.", sp.jobWaitingTime.Minutes())
	}

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		lJobWaitingTime, err := sp.readJobWaitingTime(lv)
		if err != nil {
			klog.Errorf("failed to read job waiting time for job <%s/%s>, error: %f",
				lv.Namespace, lv.Name, err.Error())
			return 0
		}
		rJobWaitingTime, err := sp.readJobWaitingTime(rv)
		if err != nil {
			klog.Errorf("failed to read job waiting time for job <%s/%s>, error: %f",
				rv.Namespace, rv.Name, err.Error())
			return 0
		}

		if lJobWaitingTime < 0 {
			if rJobWaitingTime < 0 {
				return 0
			}
			return 1
		}

		if rJobWaitingTime < 0 {
			return -1
		}

		lCreationTimestamp := lv.PodGroup.CreationTimestamp
		rCreationTimestamp := rv.PodGroup.CreationTimestamp
		if lCreationTimestamp.Add(lJobWaitingTime).Before(rCreationTimestamp.Add(rJobWaitingTime)) {
			return -1
		} else if lCreationTimestamp.Add(lJobWaitingTime).After(rCreationTimestamp.Add(rJobWaitingTime)) {
			return 1
		}
		return 0
	}
	ssn.AddJobOrderFn(sp.Name(), jobOrderFn)

	permitableFn := func(obj interface{}) int {
		jobInfo := obj.(*api.JobInfo)
		jobWaitingTime, err := sp.readJobWaitingTime(jobInfo)
		if err != nil {
			klog.Errorf("failed to read job waiting time for job <%s/%s>, error: %f",
				jobInfo.Namespace, jobInfo.Name, err.Error())
			return 0
		}
		if jobWaitingTime < 0 {
			klog.V(4).Infof("no valid job waiting time settings for job <%s/%s>, skip sla plugin",
				jobInfo.Namespace, jobInfo.Name)
			return 0
		}
		if time.Now().Sub(jobInfo.PodGroup.CreationTimestamp.Time) < jobWaitingTime {
			klog.V(4).Infof("job <%s/%s> waiting time <%f minutes> is not over yet",
				jobWaitingTime.Minutes(), jobInfo.Namespace, jobInfo.Name)
			return 0
		}
		return 1
	}
	// if job waiting time is over, turn job to be inqueue in enqueue action
	ssn.AddJobEnqueueableFn(sp.Name(), permitableFn)
	// if job waiting time is over, turn job to be pipelined in allocate action
	ssn.AddJobPipelinedFn(sp.Name(), permitableFn)
}

func (sp *slaPlugin) OnSessionClose(ssn *framework.Session) {}
