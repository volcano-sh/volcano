/*
Copyright 2022 The Volcano Authors.

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

package cdp

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// refer to issue https://github.com/volcano-sh/volcano/issues/2075,
	// plugin cdp means cooldown protection, related to elastic scheduler,
	// when we need to enable elastic training or serving,
	// preemptible job's pods can be preempted or back to running repeatedly,
	// if no cooldown protection set, these pods can be preempted again after they just started for a short time,
	// this may cause service stability dropped.
	// cdp plugin here is to ensure vcjob's pods cannot be preempted within cooldown protection conditions.
	// currently cdp plugin only support cooldown time protection.
	PluginName = "cdp"
)

type CooldownProtectionPlugin struct {
}

// New return CooldownProtectionPlugin
func New(arguments framework.Arguments) framework.Plugin {
	return &CooldownProtectionPlugin{}
}

// Name implements framework.Plugin
func (*CooldownProtectionPlugin) Name() string {
	return PluginName
}

func (sp *CooldownProtectionPlugin) podCooldownTime(pod *v1.Pod) (value time.Duration, enabled bool) {
	// check labels and annotations
	v, ok := pod.Labels[v1beta1.CooldownTime]
	if !ok {
		v, ok = pod.Annotations[v1beta1.CooldownTime]
		if !ok {
			return 0, false
		}
	}
	vi, err := time.ParseDuration(v)
	if err != nil {
		klog.Warningf("invalid time duration %s=%s", v1beta1.CooldownTime, v)
		return 0, false
	}
	return vi, true
}

// OnSessionOpen implements framework.Plugin
func (sp *CooldownProtectionPlugin) OnSessionOpen(ssn *framework.Session) {
	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo
		for _, preemptee := range preemptees {
			cooldownTime, enabled := sp.podCooldownTime(preemptee.Pod)
			if !enabled {
				victims = append(victims, preemptee)
				continue
			}
			pod := preemptee.Pod
			// find the time of pod really transform to running
			// only running pod check stable time, others all put into victims
			stableFiltered := false
			if pod.Status.Phase == v1.PodRunning {
				// ensure pod is running and have ready state
				for _, c := range pod.Status.Conditions {
					if c.Type == v1.PodScheduled && c.Status == v1.ConditionTrue {
						if c.LastTransitionTime.Add(cooldownTime).After(time.Now()) {
							stableFiltered = true
						}
						break
					}
				}
			}
			if !stableFiltered {
				victims = append(victims, preemptee)
			}
		}

		klog.V(4).Infof("Victims from cdp plugins are %+v", victims)
		return victims, util.Permit
	}

	klog.V(4).Info("plugin cdp session open")
	ssn.AddPreemptableFn(sp.Name(), preemptableFn)
}

// OnSessionClose implements framework.Plugin
func (*CooldownProtectionPlugin) OnSessionClose(ssn *framework.Session) {}
