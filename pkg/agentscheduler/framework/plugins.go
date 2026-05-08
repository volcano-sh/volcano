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
	"sync"

	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	volcanofwk "volcano.sh/volcano/pkg/scheduler/framework"
)

var pluginMutex sync.RWMutex

// PluginBuilder plugin management
type PluginBuilder = func(volcanofwk.Arguments) Plugin

// Plugin management
var pluginBuilders = map[string]PluginBuilder{}

// GetPluginBuilder get the pluginbuilder by name
func GetPluginBuilder(name string) (PluginBuilder, bool) {
	pluginMutex.RLock()
	defer pluginMutex.RUnlock()

	pb, found := pluginBuilders[name]
	return pb, found
}

// RegisterPluginBuilder register the plugin
func RegisterPluginBuilder(name string, pc PluginBuilder) {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	pluginBuilders[name] = pc
}

func isEnabled(enabled *bool) bool {
	return enabled != nil && *enabled
}

// Helper methods to register extension points

func (f *Framework) AddPredicateFn(name string, fn api.PredicateFn) {
	f.PredicateFns[name] = fn
}

func (f *Framework) AddPrePredicateFn(name string, fn api.PrePredicateFn) {
	f.PrePredicateFns[name] = fn
}

func (f *Framework) AddNodeOrderFn(name string, fn api.NodeOrderFn) {
	f.NodeOrderFns[name] = fn
}

func (f *Framework) AddBatchNodeOrderFn(name string, fn api.BatchNodeOrderFn) {
	f.BatchNodeOrderFns[name] = fn
}

func (f *Framework) AddNodeMapFn(name string, fn api.NodeMapFn) {
	f.NodeMapFns[name] = fn
}

func (f *Framework) AddNodeReduceFn(name string, fn api.NodeReduceFn) {
	f.NodeReduceFns[name] = fn
}

// PredicateFn invoke predicate function of the plugins
func (f *Framework) PredicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
	for _, tier := range f.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledPredicate) {
				continue
			}
			pfn, found := f.PredicateFns[plugin.Name]
			if !found {
				continue
			}
			err := pfn(task, node)
			if err != nil {
				f.UpdateFailedFns(f.PredicateFailedFns, plugin.Name)
				return err
			}
		}
	}
	return nil
}

// PrePredicateFn invoke predicate function of the plugins
func (f *Framework) PrePredicateFn(task *api.TaskInfo) error {
	for _, tier := range f.Tiers {
		for _, plugin := range tier.Plugins {
			// we use same option as predicates for they are
			if !isEnabled(plugin.EnabledPredicate) {
				continue
			}
			pfn, found := f.PrePredicateFns[plugin.Name]
			if !found {
				continue
			}
			err := pfn(task)
			if err != nil {
				f.UpdateFailedFns(f.PrePredicateFailedFns, plugin.Name)
				return err
			}
		}
	}
	return nil
}

// NodeOrderFn invoke node order function of the plugins
func (f *Framework) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
	priorityScore := 0.0
	for _, tier := range f.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := f.NodeOrderFns[plugin.Name]
			if !found {
				continue
			}
			score, err := pfn(task, node)
			if err != nil {
				return 0, err
			}
			priorityScore += score
		}
	}
	return priorityScore, nil
}

// BatchNodeOrderFn invoke node order function of the plugins
func (f *Framework) BatchNodeOrderFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
	priorityScore := make(map[string]float64, len(nodes))
	for _, tier := range f.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := f.BatchNodeOrderFns[plugin.Name]
			if !found {
				continue
			}
			score, err := pfn(task, nodes)
			if err != nil {
				return nil, err
			}
			for nodeName, score := range score {
				priorityScore[nodeName] += score
			}
		}
	}
	return priorityScore, nil
}

// NodeOrderMapFn invoke node order function of the plugins
func (f *Framework) NodeOrderMapFn(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
	nodeScoreMap := map[string]float64{}
	var priorityScore float64
	for _, tier := range f.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			if pfn, found := f.NodeOrderFns[plugin.Name]; found {
				score, err := pfn(task, node)
				if err != nil {
					return nodeScoreMap, priorityScore, err
				}
				priorityScore += score
			}
			if pfn, found := f.NodeMapFns[plugin.Name]; found {
				score, err := pfn(task, node)
				if err != nil {
					return nodeScoreMap, priorityScore, err
				}
				nodeScoreMap[plugin.Name] = score
			}
		}
	}
	return nodeScoreMap, priorityScore, nil
}

// NodeOrderReduceFn invoke node order function of the plugins
func (f *Framework) NodeOrderReduceFn(task *api.TaskInfo, pluginNodeScoreMap map[string]k8sframework.NodeScoreList) (map[string]float64, error) {
	nodeScoreMap := map[string]float64{}
	for _, tier := range f.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := f.NodeReduceFns[plugin.Name]
			if !found {
				continue
			}
			if err := pfn(task, pluginNodeScoreMap[plugin.Name]); err != nil {
				return nodeScoreMap, err
			}
			for _, hp := range pluginNodeScoreMap[plugin.Name] {
				nodeScoreMap[hp.Name] += float64(hp.Score)
			}
		}
	}
	return nodeScoreMap, nil
}

// Action management
var actionMap = map[string]Action{}

// RegisterAction register action
func RegisterAction(act Action) {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	actionMap[act.Name()] = act
}

// GetAction get the action by name
func GetAction(name string) (Action, bool) {
	pluginMutex.RLock()
	defer pluginMutex.RUnlock()

	act, found := actionMap[name]
	return act, found
}
