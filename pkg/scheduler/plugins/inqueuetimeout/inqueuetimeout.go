/*
Copyright 2025 The Volcano Authors.

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

package inqueuetimeout

import (
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin
	PluginName = "inqueuetimeout"
	// InqueueTimeout is the key for global inqueue timeout in plugin arguments
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h"
	InqueueTimeout = "inqueue-timeout"
	// AnnotationInqueueTimeout is the per-PodGroup annotation key to override global inqueue timeout
	AnnotationInqueueTimeout = "volcano.sh/inqueue-timeout"
)

type inqueueTimeoutPlugin struct {
	pluginArguments framework.Arguments
	globalTimeout   *time.Duration
}

// New returns inqueuetimeout plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &inqueueTimeoutPlugin{
		pluginArguments: arguments,
		globalTimeout:   nil,
	}
}

func (itp *inqueueTimeoutPlugin) Name() string {
	return PluginName
}

// getTimeout returns the inqueue timeout for a job.
// Per-PodGroup annotation overrides the global timeout.
func (itp *inqueueTimeoutPlugin) getTimeout(job *api.JobInfo) *time.Duration {
	if job.PodGroup != nil && job.PodGroup.Annotations != nil {
		if val, exist := job.PodGroup.Annotations[AnnotationInqueueTimeout]; exist {
			d, err := time.ParseDuration(val)
			if err != nil {
				klog.Errorf("Error parsing inqueue timeout annotation for job <%s/%s>: %v", job.Namespace, job.Name, err)
			} else if d > 0 {
				return &d
			} else {
				klog.Warningf("Invalid inqueue timeout annotation value %q for job <%s/%s>", val, job.Namespace, job.Name)
			}
		}
	}
	return itp.globalTimeout
}

// getInqueueTimestamp returns the time when the PodGroup entered Inqueue state
// by reading the PodGroupInqueuedType condition's LastTransitionTime.
func getInqueueTimestamp(job *api.JobInfo) time.Time {
	if job.PodGroup == nil {
		return time.Time{}
	}
	for _, cond := range job.PodGroup.Status.Conditions {
		if cond.Type == scheduling.PodGroupInqueuedType {
			return cond.LastTransitionTime.Time
		}
	}
	return time.Time{}
}

/*
User can configure global inqueue timeout via plugin arguments:

	actions: "enqueue, allocate, backfill"
	tiers:
	- plugins:
	  - name: inqueuetimeout
	    enableJobEnqueued: true
	    arguments:
	      inqueue-timeout: 5m

Per-PodGroup override via annotation:

	apiVersion: scheduling.volcano.sh/v1beta1
	kind: PodGroup
	metadata:
	  annotations:
	    volcano.sh/inqueue-timeout: 10m
*/
func (itp *inqueueTimeoutPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter inqueuetimeout plugin ...")
	defer klog.V(4).Infof("Leaving inqueuetimeout plugin.")

	if _, exist := itp.pluginArguments[InqueueTimeout]; exist {
		timeoutStr, ok := itp.pluginArguments[InqueueTimeout].(string)
		if !ok {
			timeoutStr = ""
		}
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			klog.Errorf("Error parsing global inqueue timeout in inqueuetimeout plugin: %v", err)
		} else if d <= 0 {
			klog.Warningf("Invalid global inqueue timeout setting: %s in inqueuetimeout plugin.", d.String())
		} else {
			itp.globalTimeout = &d
			klog.V(4).Infof("Global inqueue timeout is %s.", itp.globalTimeout.String())
		}
	}

	ssn.AddJobDequeueableFn(itp.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)

		timeout := itp.getTimeout(job)
		if timeout == nil {
			return util.Abstain
		}

		inqueueTime := getInqueueTimestamp(job)
		if inqueueTime.IsZero() {
			return util.Abstain
		}

		if time.Since(inqueueTime) > *timeout {
			klog.V(3).Infof("Job <%s/%s> exceeded inqueue timeout (%v), voting to dequeue",
				job.Namespace, job.Name, *timeout)
			return util.Permit
		}

		return util.Abstain
	})
}

func (itp *inqueueTimeoutPlugin) OnSessionClose(ssn *framework.Session) {}
