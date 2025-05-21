/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	"k8s.io/klog/v2"
	"time"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// OpenSession start the session
func OpenSession(cache cache.Cache, tiers []conf.Tier, configurations []conf.Configuration, schedulerPolicies map[string]*SchedulerPolicy) *Session {
	ssn := openSession(cache)
	ssn.Tiers = tiers
	ssn.Configurations = configurations
	ssn.NodeMap = GenerateNodeMapAndSlice(ssn.Nodes)
	ssn.PodLister = NewPodLister(ssn)
	ssn.schedulerPolicies = schedulerPolicies

	for name, schedulerPolicy := range schedulerPolicies {
		schedulerPolicy.plugins = make(map[string]Plugin)
		for _, tier := range schedulerPolicy.Tiers {
			for _, plugin := range tier.Plugins {
				if pb, found := GetPluginBuilder(plugin.Name); !found {
					klog.Errorf("SchedulerPolicy: %v Failed to get plugin %s.", name, plugin.Name)
				} else {
					plugin := pb(plugin.Arguments)
					schedulerPolicy.plugins[plugin.Name()] = plugin
					plugin.OnSessionOpen(ssn)
				}
			}
		}
		copyAndClearSessionFunctions(schedulerPolicy, ssn)
	}

	for _, tier := range tiers {
		for _, plugin := range tier.Plugins {
			if pb, found := GetPluginBuilder(plugin.Name); !found {
				klog.Errorf("Failed to get plugin %s.", plugin.Name)
			} else {
				plugin := pb(plugin.Arguments)
				ssn.plugins[plugin.Name()] = plugin
				onSessionOpenStart := time.Now()
				plugin.OnSessionOpen(ssn)
				metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionOpen, metrics.Duration(onSessionOpenStart))
			}
		}
	}
	return ssn
}

// CloseSession close the session
func CloseSession(ssn *Session) {
	for _, plugin := range ssn.plugins {
		onSessionCloseStart := time.Now()
		plugin.OnSessionClose(ssn)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionClose, metrics.Duration(onSessionCloseStart))
	}
	for _, schedulerPolicy := range ssn.schedulerPolicies {
		for _, plugin := range schedulerPolicy.plugins {
			plugin.OnSessionClose(ssn)
		}
	}
	closeSession(ssn)
}
