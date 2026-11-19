/*
Copyright 2026 The Volcano Authors.

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

package gangreclaim

import (
	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// setBundleResourceFitScores counts targets gaining a resource-feasible node
// when each bundle is released. It only reads session resources; simulation remains
// responsible for joint gang feasibility, predicates and device allocation.
func setBundleResourceFitScores(bundles []*utils.Bundle, nodes []*api.NodeInfo, targets []*api.TaskInfo) {
	nodesByName := make(map[string]*api.NodeInfo, len(nodes))
	for _, node := range nodes {
		nodesByName[node.Name] = node
	}
	// Unaffected nodes cannot distinguish bundles. In particular, spare capacity
	// on an unrelated or tainted node must not hide improvements on victim nodes.
	futureIdle := make(map[string]*api.Resource)

	for _, bundle := range bundles {
		availableByNode := make(map[string]*api.Resource)
		for _, victim := range bundle.Tasks {
			node, found := nodesByName[victim.NodeName]
			if !found {
				continue
			}
			base, found := futureIdle[victim.NodeName]
			if !found {
				base = node.FutureIdle()
				futureIdle[victim.NodeName] = base
			}
			if _, found := availableByNode[victim.NodeName]; !found {
				availableByNode[victim.NodeName] = base.Clone()
			}
			availableByNode[victim.NodeName].Add(victim.Resreq)
		}
		bundle.ResourceFitScore = 0
		for _, task := range targets {
			for name, available := range availableByNode {
				if !task.InitResreq.LessEqual(futureIdle[name], api.Zero) && task.InitResreq.LessEqual(available, api.Zero) {
					bundle.ResourceFitScore++
					break
				}
			}
		}
	}
}
