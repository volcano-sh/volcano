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

package api

import "k8s.io/apimachinery/pkg/util/sets"

// HyperNodeIndex indexes an immutable topology snapshot. Task occupancy is not
// cached here: allocation, pipelining and rollback change it within a Session.
type HyperNodeIndex struct {
	HyperNodes       HyperNodeInfoMap
	NodeToHyperNode  map[string]string
	ancestors        map[string][]string
	ancestorByTier   map[string]map[int]string
	nodesByHyperNode map[string]sets.Set[string]
	overlappingNodes sets.Set[string]
}

// NewHyperNodeIndex builds reverse membership and ancestry for a topology snapshot.
func NewHyperNodeIndex(hyperNodes HyperNodeInfoMap, nodesByHyperNode map[string]sets.Set[string]) *HyperNodeIndex {
	index := &HyperNodeIndex{
		HyperNodes: hyperNodes, NodeToHyperNode: make(map[string]string),
		ancestors: make(map[string][]string), ancestorByTier: make(map[string]map[int]string),
		nodesByHyperNode: nodesByHyperNode, overlappingNodes: sets.New[string](),
	}
	// Infer missing Parent fields from Children once, including synthetic roots.
	parents := make(map[string]string, len(hyperNodes))
	for name, hn := range hyperNodes {
		for child := range hn.Children {
			parents[child] = name
		}
	}
	for name, hn := range hyperNodes {
		if hn.Parent != "" {
			parents[name] = hn.Parent
		}
	}
	for name := range hyperNodes {
		seen := sets.New[string]()
		index.ancestorByTier[name] = make(map[int]string)
		for current := name; current != "" && !seen.Has(current); current = parents[current] {
			hn := hyperNodes[current]
			if hn == nil {
				break
			}
			seen.Insert(current)
			index.ancestors[name] = append(index.ancestors[name], current)
			index.ancestorByTier[name][hn.Tier()] = current
		}
	}
	for name, nodes := range nodesByHyperNode {
		hn := hyperNodes[name]
		if hn == nil {
			continue
		}
		for node := range nodes {
			previous := hyperNodes[index.NodeToHyperNode[node]]
			if previous == nil || hn.Tier() < previous.Tier() || (hn.Tier() == previous.Tier() && name < previous.Name) {
				index.NodeToHyperNode[node] = name
			}
		}
	}
	// A Node selector can overlap another subtree. Such memberships do not
	// form one ancestor chain; retain the legacy common-membership semantics.
	for name, nodes := range nodesByHyperNode {
		if hn := hyperNodes[name]; hn != nil {
			for node := range nodes {
				if index.AncestorAtTier(index.NodeToHyperNode[node], hn.Tier()) != name {
					index.overlappingNodes.Insert(node)
				}
			}
		}
	}
	return index
}

// DomainsContaining returns only HyperNodes whose Node membership covers nodes.
// Normally these lie on one ancestor chain. Overlapping Node selectors retain
// the full membership check for compatibility instead of dropping a valid domain.
func (index *HyperNodeIndex) DomainsContaining(nodes sets.Set[string]) []*HyperNodeInfo {
	if len(nodes) == 0 {
		return nil
	}
	var firstNode string
	for node := range nodes {
		firstNode = node
		break
	}
	names := index.Ancestors(index.NodeToHyperNode[firstNode])
	if index.overlappingNodes.Has(firstNode) {
		names = make([]string, 0, len(index.HyperNodes))
		for name := range index.HyperNodes {
			names = append(names, name)
		}
	}
	var domains []*HyperNodeInfo
	for _, name := range names {
		if index.nodesByHyperNode[name].IsSuperset(nodes) {
			domains = append(domains, index.HyperNodes[name])
		}
	}
	return domains
}

// Ancestors returns the domain followed by its ancestors, finest first.
// The returned slice belongs to the index and must not be modified.
func (index *HyperNodeIndex) Ancestors(name string) []string {
	return index.ancestors[name]
}

// AncestorAtTier resolves a comparison domain without scanning the topology.
func (index *HyperNodeIndex) AncestorAtTier(name string, tier int) string {
	return index.ancestorByTier[name][tier]
}

// LowestCommonAncestor returns the finest domain containing both inputs.
// An empty input contributes no placement constraint.
func (index *HyperNodeIndex) LowestCommonAncestor(first, second string) string {
	if first == "" {
		return second
	}
	if second == "" || first == second {
		return first
	}
	for _, ancestor := range index.ancestors[first] {
		if index.AncestorAtTier(second, index.HyperNodes[ancestor].Tier()) == ancestor {
			return ancestor
		}
	}
	return ""
}

// JobAllocatedHyperNode returns the common domain of the Job's placed tasks.
func (index *HyperNodeIndex) JobAllocatedHyperNode(job *JobInfo) string {
	return index.allocatedHyperNode(collectJobAllocatedTasks(job))
}

// SubJobAllocatedHyperNode returns the common domain of the SubJob's placed tasks.
func (index *HyperNodeIndex) SubJobAllocatedHyperNode(subJob *SubJobInfo) string {
	return index.allocatedHyperNode(collectSubJobAllocatedTasks(subJob))
}

// allocatedHyperNode returns the common domain of all placed tasks. An unmapped
// task makes the result incomplete; callers must not widen persisted placement.
func (index *HyperNodeIndex) allocatedHyperNode(tasks []*TaskInfo) string {
	var result string
	seen := sets.New[string]()
	for _, task := range tasks {
		if index.overlappingNodes.Has(task.NodeName) {
			return getAllocatedHyperNodeFromTasks(tasks, sets.KeySet(index.HyperNodes), index.nodesByHyperNode, index.HyperNodes)
		}
		if task.NodeName == "" || seen.Has(task.NodeName) {
			continue
		}
		seen.Insert(task.NodeName)
		hn := index.NodeToHyperNode[task.NodeName]
		if hn == "" {
			return ""
		}
		result = index.LowestCommonAncestor(result, hn)
		if result == "" {
			return ""
		}
	}
	return result
}

// OccupiedHyperNodes reads live task placement, deduplicating Nodes before
// looking up ancestors. Persisted placement is only a fallback for missing data.
func (index *HyperNodeIndex) OccupiedHyperNodes(job *JobInfo, tier int) sets.Set[string] {
	occupied := sets.New[string]()
	if job == nil {
		return occupied
	}
	seen := sets.New[string]()
	for _, task := range collectJobAllocatedTasks(job) {
		if seen.Has(task.NodeName) {
			continue
		}
		seen.Insert(task.NodeName)
		if hn := index.NodeToHyperNode[task.NodeName]; hn != "" {
			if ancestor := index.AncestorAtTier(hn, tier); ancestor != "" {
				occupied.Insert(ancestor)
			}
		}
	}
	if occupied.Len() == 0 {
		placement := job.AllocatedHyperNode
		if placement == "" {
			for _, subJob := range job.SubJobs {
				placement = index.LowestCommonAncestor(placement, subJob.AllocatedHyperNode)
			}
		}
		for _, hn := range index.HyperNodes.ResolveHyperNodesAtTier(placement, tier) {
			occupied.Insert(hn)
		}
	}
	return occupied
}
