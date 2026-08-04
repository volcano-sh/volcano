/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2023 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced session initialization with configuration support

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

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

type sessionOptions struct {
	unschedulableCache cache.UnschedulableCache
}

// SessionOption configures optional Session dependencies.
type SessionOption func(*sessionOptions)

// WithUnschedulableCache provides the cache used by the UnschedulableJobCache
// feature. Keeping it separate from cache.Cache avoids runtime type assertions
// and lets each interface have its own implementation.
func WithUnschedulableCache(unschedulableCache cache.UnschedulableCache) SessionOption {
	return func(options *sessionOptions) {
		options.unschedulableCache = unschedulableCache
	}
}

// OpenSession start the session
func OpenSession(schedulerCache cache.Cache, tiers []conf.Tier, configurations []conf.Configuration, opts ...SessionOption) *Session {
	openStart := time.Now()
	options := &sessionOptions{}
	for _, opt := range opts {
		opt(options)
	}
	ssn := openSession(schedulerCache, options.unschedulableCache)
	ssn.Tiers = tiers
	ssn.Configurations = configurations
	ssn.NodeMap = GenerateNodeMapAndSlice(ssn.Nodes)
	ssn.PodLister = NewPodLister(ssn)

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

	ssn.InitCycleState()
	ssn.applyCachedSkips()
	metrics.UpdateOpenSessionDuration(time.Since(openStart))

	return ssn
}

// CloseSession close the session
func CloseSession(ssn *Session) {
	for _, plugin := range ssn.plugins {
		onSessionCloseStart := time.Now()
		plugin.OnSessionClose(ssn)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionClose, metrics.Duration(onSessionCloseStart))
	}

	ssn.reconcileUnschedulableCache()

	closeSession(ssn)
}
