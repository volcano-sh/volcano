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

package framework

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	k8scache "k8s.io/kubernetes/pkg/scheduler/backend/cache"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

// Framework manages the scheduler plugins and their execution points.
type Framework struct {
	*k8sutil.Framework // Embedding Framework to implement framework.Handle interface

	Plugins        map[string]Plugin
	Tiers          []conf.Tier
	Configurations []conf.Configuration

	// Function registries
	PredicateFns      map[string]api.PredicateFn
	PrePredicateFns   map[string]api.PrePredicateFn
	NodeOrderFns      map[string]api.NodeOrderFn
	BatchNodeOrderFns map[string]api.BatchNodeOrderFn
	NodeMapFns        map[string]api.NodeMapFn
	NodeReduceFns     map[string]api.NodeReduceFn

	Cache cache.Cache // TODO: need to replace to agent scheduler's own cache

	// CycleState for the current scheduling cycle
	// Since agent scheduler schedules one pod per cycle, we only need one CycleState.
	// When multiple workers collaborate on scheduling simultaneously in the future,
	// we may need to store cycleState using sync.Map or similar methods.
	CurrentCycleState *k8sframework.CycleState
}

var _ framework.Handle = &Framework{}

// NewFramework initializes the framework with the given plugins.
func NewFramework(tiers []conf.Tier, cache cache.Cache, configurations []conf.Configuration) *Framework {
	utilFwk := k8sutil.NewFramework(
		nil, // fast path scheduler needs to use snapshot shared lister instead
		k8sutil.WithSnapshotSharedLister(k8scache.NewEmptySnapshot()), // TODO: may need to use to volcano scheduler's own snapshot?
		k8sutil.WithSharedDRAManager(cache.SharedDRAManager()),
		k8sutil.WithClientSet(cache.Client()),
		k8sutil.WithInformerFactory(cache.SharedInformerFactory()),
	)

	fwk := &Framework{
		Plugins:           make(map[string]Plugin),
		Tiers:             tiers,
		Configurations:    configurations,
		PredicateFns:      make(map[string]api.PredicateFn),
		PrePredicateFns:   make(map[string]api.PrePredicateFn),
		NodeOrderFns:      make(map[string]api.NodeOrderFn),
		BatchNodeOrderFns: make(map[string]api.BatchNodeOrderFn),
		NodeMapFns:        make(map[string]api.NodeMapFn),
		NodeReduceFns:     make(map[string]api.NodeReduceFn),

		Cache: cache,

		Framework: utilFwk,
	}

	for _, tier := range tiers {
		for _, pluginConf := range tier.Plugins {
			if pb, found := GetPluginBuilder(pluginConf.Name); !found {
				klog.Errorf("Failed to get plugin %s.", pluginConf.Name)
			} else {
				plugin := pb(pluginConf.Arguments)
				fwk.Plugins[plugin.Name()] = plugin
				plugin.OnPluginInit(fwk)
			}
		}
	}

	return fwk
}

// GetCycleState returns the CycleState for the current scheduling cycle.
// Since agent scheduler schedules one pod per cycle, all calls return the same state.
func (f *Framework) GetCycleState(taskUID types.UID) *k8sframework.CycleState {
	if f.CurrentCycleState != nil {
		return f.CurrentCycleState
	}

	// First call in this cycle, create new state
	f.CurrentCycleState = k8sframework.NewCycleState()
	return f.CurrentCycleState
}

// ClearCycleState clears the current CycleState.
func (f *Framework) ClearCycleState() {
	f.CurrentCycleState = nil // Let GC reclaim it
}

// OnCycleStart calls OnCycleStart for all plugins.
func (f *Framework) OnCycleStart() {
	for _, plugin := range f.Plugins {
		plugin.OnCycleStart(f)
	}
}

// OnCycleEnd calls OnCycleEnd for all plugins.
func (f *Framework) OnCycleEnd() {
	for _, plugin := range f.Plugins {
		plugin.OnCycleEnd(f)
	}
}
