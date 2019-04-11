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
	"time"

	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/conf"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/metrics"
)

// OpenSession start the session
func OpenSession(cache cache.Cache, tiers []conf.Tier) *Session {
	ssn := openSession(cache)
	ssn.Tiers = tiers

	for _, tier := range tiers {
		for _, plugin := range tier.Plugins {
			if pb, found := GetPluginBuilder(plugin.Name); !found {
				glog.Errorf("Failed to get plugin %s.", plugin.Name)
			} else {
				plugin := pb(plugin.Arguments)
				ssn.plugins[plugin.Name()] = plugin
			}
		}
	}

	for _, plugin := range ssn.plugins {
		onSessionOpenStart := time.Now()
		plugin.OnSessionOpen(ssn)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionOpen, metrics.Duration(onSessionOpenStart))
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

	closeSession(ssn)
}
