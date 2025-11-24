/*
Copyright 2018 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added additional plugin extension points

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
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// AddPredicateFn add Predicate function
func (sc *ScheduleCycle) AddPredicateFn(name string, pf api.PredicateFn) {
	sc.predicateFns[name] = pf
}

// AddPrePredicateFn add PrePredicate function
func (sc *ScheduleCycle) AddPrePredicateFn(name string, pf api.PrePredicateFn) {
	sc.prePredicateFns[name] = pf
}

// AddNodeOrderFn add Node order function
func (sc *ScheduleCycle) AddNodeOrderFn(name string, pf api.NodeOrderFn) {
	sc.nodeOrderFns[name] = pf
}

// AddBatchNodeOrderFn add Batch Node order function
func (sc *ScheduleCycle) AddBatchNodeOrderFn(name string, pf api.BatchNodeOrderFn) {
	sc.batchNodeOrderFns[name] = pf
}

// AddNodeMapFn add Node map function
func (sc *ScheduleCycle) AddNodeMapFn(name string, pf api.NodeMapFn) {
	sc.nodeMapFns[name] = pf
}

// AddNodeReduceFn add Node reduce function
func (sc *ScheduleCycle) AddNodeReduceFn(name string, pf api.NodeReduceFn) {
	sc.nodeReduceFns[name] = pf
}

func isEnabled(enabled *bool) bool {
	return enabled != nil && *enabled
}

// PredicateFn invoke predicate function of the plugins
func (sc *ScheduleCycle) PredicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
	for _, tier := range sc.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledPredicate) {
				continue
			}
			pfn, found := sc.predicateFns[plugin.Name]
			if !found {
				continue
			}
			err := pfn(task, node)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// PrePredicateFn invoke predicate function of the plugins
func (sc *ScheduleCycle) PrePredicateFn(task *api.TaskInfo) error {
	for _, tier := range sc.Tiers {
		for _, plugin := range tier.Plugins {
			// we use same option as predicates for they are
			if !isEnabled(plugin.EnabledPredicate) {
				continue
			}
			pfn, found := sc.prePredicateFns[plugin.Name]
			if !found {
				continue
			}
			err := pfn(task)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// NodeOrderFn invoke node order function of the plugins
func (sc *ScheduleCycle) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
	priorityScore := 0.0
	for _, tier := range sc.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := sc.nodeOrderFns[plugin.Name]
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
func (sc *ScheduleCycle) BatchNodeOrderFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
	priorityScore := make(map[string]float64, len(nodes))
	for _, tier := range sc.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := sc.batchNodeOrderFns[plugin.Name]
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

// func isEnabled(enabled *bool) bool {
// 	return enabled != nil && *enabled
// }

// NodeOrderMapFn invoke node order function of the plugins
func (sc *ScheduleCycle) NodeOrderMapFn(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
	nodeScoreMap := map[string]float64{}
	var priorityScore float64
	for _, tier := range sc.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			if pfn, found := sc.nodeOrderFns[plugin.Name]; found {
				score, err := pfn(task, node)
				if err != nil {
					return nodeScoreMap, priorityScore, err
				}
				priorityScore += score
			}
			if pfn, found := sc.nodeMapFns[plugin.Name]; found {
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
func (sc *ScheduleCycle) NodeOrderReduceFn(task *api.TaskInfo, pluginNodeScoreMap map[string]k8sframework.NodeScoreList) (map[string]float64, error) {
	nodeScoreMap := map[string]float64{}
	for _, tier := range sc.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := sc.nodeReduceFns[plugin.Name]
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

// // RegisterBinder registers the passed binder to the cache, the binder type can be such as pre-binder, post-binder
// func (sc *SchedulePluginRegistry) RegisterBinder(name string, binder interface{}) {
// 	sc.cache.RegisterBinder(name, binder)
// }
