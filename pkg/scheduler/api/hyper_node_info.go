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

package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

// HyperNodesInfo stores and manages the hierarchical structure of HyperNodes.
type HyperNodesInfo struct {
	sync.Mutex

	// hyperNodes stores information about all HyperNodes.
	hyperNodes map[string]*HyperNodeInfo
	// hyperNodesSetByTier stores HyperNode names grouped by their tier.
	hyperNodesSetByTier map[int]sets.Set[string]
	// realNodesSet stores the set of real nodes for each HyperNode, eg, s0 and s1 are members of s4,
	// s0->node0, node1, s1->node2, node3, s4->node0, node1, s1->node2, node3
	realNodesSet map[string]sets.Set[string]
	// parentMap stores the parent of each HyperNode.
	parentMap map[string]string
	// nodeLister Lister to list Kubernetes nodes.
	nodeLister listerv1.NodeLister

	// ready indicates whether the HyperNodesInfo is ready (build process is complete).
	ready *atomic.Bool
}

// NewHyperNodesInfo initializes a new HyperNodesInfo instance.
func NewHyperNodesInfo(lister listerv1.NodeLister) *HyperNodesInfo {
	return &HyperNodesInfo{
		hyperNodes:          make(map[string]*HyperNodeInfo),
		hyperNodesSetByTier: make(map[int]sets.Set[string]),
		realNodesSet:        make(map[string]sets.Set[string]),
		parentMap:           make(map[string]string),
		nodeLister:          lister,
		ready:               new(atomic.Bool),
	}
}

// NewHyperNodesInfoWithCache initializes a new HyperNodesInfo instance with cache.
// This is just used for ut.
// TODO: abstract an interface to mock for ut.
func NewHyperNodesInfoWithCache(hyperNodesSetByTier map[int]sets.Set[string], realNodesSet map[string]sets.Set[string], ready *atomic.Bool) *HyperNodesInfo {
	return &HyperNodesInfo{
		hyperNodes:          make(map[string]*HyperNodeInfo),
		hyperNodesSetByTier: hyperNodesSetByTier,
		realNodesSet:        realNodesSet,
		parentMap:           make(map[string]string),
		ready:               ready,
	}
}

// HyperNodeInfo stores information about a single HyperNode.
type HyperNodeInfo struct {
	Name      string
	HyperNode *topologyv1alpha1.HyperNode
	Tier      int

	isDeleting bool
}

// NewHyperNodeInfo creates a new HyperNodeInfo instance.
func NewHyperNodeInfo(hn *topologyv1alpha1.HyperNode, tier int) *HyperNodeInfo {
	return &HyperNodeInfo{
		Name:      hn.Name,
		HyperNode: hn,
		Tier:      tier,
	}
}

// String returns a string representation of the HyperNodeInfo.
func (hni *HyperNodeInfo) String() string {
	return strings.Join([]string{hni.Name, strconv.Itoa(hni.Tier)}, "/")
}

// HyperNodes returns the map of all HyperNode information.
func (hni *HyperNodesInfo) HyperNodes() map[string]*HyperNodeInfo {
	return hni.hyperNodes
}

// HyperNodesSetByTier returns a deep copy of the map that groups HyperNode names by their tier.
// This ensures that the returned map is independent of the original, preventing unintended modifications.
func (hni *HyperNodesInfo) HyperNodesSetByTier() map[int]sets.Set[string] {
	copiedHyperNodesSetByTier := make(map[int]sets.Set[string], len(hni.hyperNodesSetByTier))
	for tier, row := range hni.hyperNodesSetByTier {
		copiedHyperNodesSetByTier[tier] = row.Clone()
	}

	return copiedHyperNodesSetByTier
}

// RealNodesSet returns a deep copy of the map that stores the sets of real nodes for each HyperNode.
// This ensures that the returned map is independent of the original, preventing unintended modifications.
func (hni *HyperNodesInfo) RealNodesSet() map[string]sets.Set[string] {
	copiedRealNodesSet := make(map[string]sets.Set[string], len(hni.realNodesSet))
	for name, value := range hni.realNodesSet {
		copiedRealNodesSet[name] = value.Clone() // Clone the set to ensure a deep copy.
	}

	return copiedRealNodesSet
}

// ParentMap returns the map of parent-child relationships between HyperNodes.
func (hni *HyperNodesInfo) ParentMap() map[string]string {
	return hni.parentMap
}

// DeleteHyperNode deletes a HyperNode from the cache and update hyperNode tree.
func (hni *HyperNodesInfo) DeleteHyperNode(name string) error {
	hni.markHyperNodeIsDeleting(name)
	if err := hni.updateAncestors(name); err != nil {
		return err
	}
	hni.removeParent(name)

	// We can safely delete hyperNode after updated ancestors.
	hni.deleteHyperNode(name)
	return nil
}

func (hni *HyperNodesInfo) UpdateHyperNode(hn *topologyv1alpha1.HyperNode) error {
	hni.updateHyperNode(hn)
	return hni.updateAncestors(hn.Name)
}

func (hni *HyperNodesInfo) BuildHyperNodeCache(hn *HyperNodeInfo, processed sets.Set[string], ancestorsChain sets.Set[string], ancestors sets.Set[string], nodes []*corev1.Node) error {
	// Check for cyclic dependency.
	if ancestorsChain.Has(hn.Name) {
		return fmt.Errorf("cyclic dependency detected: HyperNode %s is already in the ancestor chain %v", hn.Name, sets.List(ancestorsChain))
	}
	// Pruning operation: skip if hyperNode already processed.
	if processed.Has(hn.Name) {
		return nil
	}
	// Only process current hyperNode's ancestor, other hyperNodes are no need to update.
	if !ancestors.Has(hn.Name) {
		return nil
	}

	ancestorsChain.Insert(hn.Name)
	defer ancestorsChain.Delete(hn.Name)

	if hni.HyperNodeIsDeleting(hn.Name) {
		klog.InfoS("HyperNode is being deleted", "name", hn.Name)
		return nil
	}

	for _, member := range hn.HyperNode.Spec.Members {
		switch member.Type {
		case topologyv1alpha1.MemberTypeNode:
			if _, ok := hni.realNodesSet[hn.Name]; !ok {
				hni.realNodesSet[hn.Name] = sets.New[string]()
			}
			members := hni.getMembers(member.Selector, nodes)
			klog.V(5).InfoS("Get members of hyperNode", "name", hn.Name, "members", members)
			hni.realNodesSet[hn.Name] = hni.realNodesSet[hn.Name].Union(members)

		case topologyv1alpha1.MemberTypeHyperNode:
			// HyperNode member does not support regex match.
			memberName := hni.exactMatchMember(member.Selector)
			if memberName == "" {
				continue
			}

			if err := hni.setParent(hn, memberName); err != nil {
				return err
			}

			memberHn, ok := hni.hyperNodes[memberName]
			if !ok {
				klog.InfoS("HyperNode not exists in cache, maybe not created or not be watched", "name", memberName, "parent", hn.Name)
				continue
			}

			// Recursively build cache for the member HyperNode.
			if err := hni.BuildHyperNodeCache(memberHn, processed, ancestorsChain, ancestors, nodes); err != nil {
				return err
			}

			if _, ok := hni.realNodesSet[hn.Name]; !ok {
				hni.realNodesSet[hn.Name] = sets.New[string]()
			}
			hni.realNodesSet[hn.Name] = hni.realNodesSet[hn.Name].Union(hni.realNodesSet[memberName])

		default:
			klog.ErrorS(nil, "Unknown member type", "type", member.Type)
		}
	}

	processed.Insert(hn.Name)
	klog.V(3).InfoS("Successfully built RealNodesSet with members", "name", hn.Name, "nodeSets", hni.realNodesSet[hn.Name])
	return nil
}

// Ready returns whether the HyperNodesInfo is built complete with no err
// and all members are cached and then scheduler can start schedule based on hyperNodes view.
func (hni *HyperNodesInfo) Ready() bool {
	return hni.ready.Load()
}

// setReady sets the ready flag.
func (hni *HyperNodesInfo) setReady(ready bool) {
	hni.ready.Store(ready)
}

// UpdateParentMap updates the parent-child relationships in the parentMap.
func (hni *HyperNodesInfo) UpdateParentMap(hyperNode *topologyv1alpha1.HyperNode) {
	oldMembers := sets.New[string]()
	if oldHyperNodeInfo, ok := hni.hyperNodes[hyperNode.Name]; ok {
		for _, member := range oldHyperNodeInfo.HyperNode.Spec.Members {
			if member.Type == topologyv1alpha1.MemberTypeHyperNode && member.Selector.ExactMatch != nil {
				oldMembers.Insert(member.Selector.ExactMatch.Name)
			}
		}
	}

	newMembers := sets.New[string]()
	for _, member := range hyperNode.Spec.Members {
		if member.Type == topologyv1alpha1.MemberTypeHyperNode && member.Selector.ExactMatch != nil {
			newMembers.Insert(member.Selector.ExactMatch.Name)
		}
	}

	removedMembers := oldMembers.Difference(newMembers)
	if removedMembers.Len() == 0 {
		return
	}

	for member := range removedMembers {
		delete(hni.parentMap, member)
	}
}

// GetAncestors returns all ancestors of a given HyperNode.
func (hni *HyperNodesInfo) GetAncestors(name string) sets.Set[string] {
	ancestors := sets.New[string](name)
	queue := []string{name}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		parent, ok := hni.parentMap[current]
		if !ok {
			parent = hni.getParent(name)
		}
		if parent != "" && !ancestors.Has(parent) {
			ancestors.Insert(parent)
			queue = append(queue, parent)
		}
	}
	return ancestors
}

// GetDescendants returns all descendants of a given HyperNode.
func (hni *HyperNodesInfo) GetDescendants(hyperNodeName string) sets.Set[string] {
	queue := []string{hyperNodeName}
	descendants := sets.New[string]()

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		descendants.Insert(current)
		if hn, ok := hni.hyperNodes[current]; ok {
			for _, member := range hn.HyperNode.Spec.Members {
				if member.Type == topologyv1alpha1.MemberTypeHyperNode && member.Selector.ExactMatch != nil {
					childName := member.Selector.ExactMatch.Name
					if !descendants.Has(childName) {
						queue = append(queue, childName)
					}
				}
			}
		}
	}
	return descendants
}

// getChildren returns the immediate children of a given HyperNode.
func (hni *HyperNodesInfo) getChildren(hyperNodeName string) sets.Set[string] {
	children := sets.New[string]()
	hn, ok := hni.hyperNodes[hyperNodeName]
	if !ok {
		klog.ErrorS(nil, "HyperNode not found", "name", hyperNodeName)
		return children
	}

	for _, member := range hn.HyperNode.Spec.Members {
		if member.Type == topologyv1alpha1.MemberTypeHyperNode && member.Selector.ExactMatch != nil {
			childName := member.Selector.ExactMatch.Name
			children.Insert(childName)
		}
	}
	return children
}

// setParent sets the parent of a HyperNode member.
func (hni *HyperNodesInfo) setParent(hn *HyperNodeInfo, memberName string) error {
	parent, ok := hni.parentMap[memberName]
	if ok && parent != hn.Name {
		return fmt.Errorf("HyperNode %s already has a parent %s, and cannot set another parent %s", memberName, parent, hn.Name)
	} else {
		hni.parentMap[memberName] = hn.Name
	}
	return nil
}

// getParent returns hyperNode's parent, this is usually used when a new hyperNode is added
// but the BuildHyperNodeCache has not built the hyperNode's cache.
func (hni *HyperNodesInfo) getParent(name string) string {
	tier := -1
	if hn, ok := hni.hyperNodes[name]; ok {
		tier = hn.Tier
	}
	for _, hn := range hni.hyperNodes {
		if hn.Tier <= tier {
			continue
		}
		for _, member := range hn.HyperNode.Spec.Members {
			// hyperNode's parent must also be a hyperNode.
			if member.Type == topologyv1alpha1.MemberTypeNode || member.Selector.ExactMatch == nil {
				continue
			}
			memberName := member.Selector.ExactMatch.Name
			if memberName == name {
				// just find one is ok.
				return hn.Name
			}
		}
	}
	return ""
}

// getMembers retrieves the members of a HyperNode based on the selector.
func (hni *HyperNodesInfo) getMembers(selector topologyv1alpha1.MemberSelector, nodes []*corev1.Node) sets.Set[string] {
	if selector.ExactMatch != nil {
		if selector.ExactMatch.Name == "" {
			return sets.New[string]()
		}
		return sets.New[string](selector.ExactMatch.Name)
	}

	members := sets.New[string]()
	if selector.RegexMatch != nil {
		pattern := selector.RegexMatch.Pattern
		reg, err := regexp.Compile(pattern)
		if err != nil {
			klog.ErrorS(err, "Failed to compile regular expression", "pattern", pattern)
			return sets.Set[string]{}
		}
		for _, node := range nodes {
			if reg.MatchString(node.Name) {
				members.Insert(node.Name)
			}
		}
	}
	return members
}

// exactMatchMember retrieves the member name if the selector is an exact match.
func (hni *HyperNodesInfo) exactMatchMember(selector topologyv1alpha1.MemberSelector) string {
	if selector.ExactMatch != nil {
		return selector.ExactMatch.Name
	}
	return ""
}

func (hni *HyperNodesInfo) updateAncestors(name string) error {
	ancestors := hni.GetAncestors(name)
	// Clear expectedRealNodesSet of hyperNode before rebuild hyperNode cache.
	for ancestor := range ancestors {
		delete(hni.realNodesSet, ancestor)
		delete(hni.parentMap, ancestor)
	}

	processed := sets.New[string]()
	ancestorsChain := sets.New[string]()
	nodes, err := hni.nodeLister.List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "Failed to list nodes", "hyperNodeName", name)
		return err
	}

	for ancestor := range ancestors {
		if hn, ok := hni.hyperNodes[ancestor]; ok {
			if err := hni.BuildHyperNodeCache(hn, processed, ancestorsChain, ancestors, nodes); err != nil {
				hni.setReady(false)
				return err
			}
		}
	}
	hni.setReady(true)
	return nil
}

// UpdateHyperNode updates the information of a HyperNode.
func (hni *HyperNodesInfo) updateHyperNode(hyperNode *topologyv1alpha1.HyperNode) {
	tier := hyperNode.Spec.Tier
	old, exists := hni.hyperNodes[hyperNode.Name]
	if exists {
		if old.Tier != hyperNode.Spec.Tier {
			hni.removeFromTierSet(hyperNode.Name, old.Tier)
		}
	}
	hni.hyperNodes[hyperNode.Name] = NewHyperNodeInfo(hyperNode, tier)
	hni.updateHyperNodeListByTier(hyperNode, tier)
}

// removeFromTierSet removes a HyperNode from the tier set.
func (hni *HyperNodesInfo) removeFromTierSet(hyperNodeName string, tier int) {
	set, ok := hni.hyperNodesSetByTier[tier]
	if !ok {
		return
	}

	set.Delete(hyperNodeName)
	if set.Len() == 0 {
		delete(hni.hyperNodesSetByTier, tier)
	}
}

// updateHyperNodeListByTier updates the list of HyperNodes by tier.
func (hni *HyperNodesInfo) updateHyperNodeListByTier(hyperNode *topologyv1alpha1.HyperNode, tier int) {
	if _, ok := hni.hyperNodesSetByTier[tier]; !ok {
		hni.hyperNodesSetByTier[tier] = sets.New[string]()
	}
	hni.hyperNodesSetByTier[tier].Insert(hyperNode.Name)
}

// removeParent removes the parent of a HyperNode.
func (hni *HyperNodesInfo) removeParent(hyperNodeName string) {
	children := hni.getChildren(hyperNodeName)
	for child := range children {
		delete(hni.parentMap, child)
	}
}

func (hni *HyperNodesInfo) deleteHyperNode(name string) {
	hn, ok := hni.hyperNodes[name]
	if !ok {
		klog.ErrorS(nil, "HyperNode not exists in cache", "name", name)
		return
	}
	delete(hni.hyperNodes, name)
	hni.removeFromTierSet(name, hn.Tier)
	delete(hni.parentMap, name)
}

// markHyperNodeIsDeleting marks a HyperNode as being deleted.
func (hni *HyperNodesInfo) markHyperNodeIsDeleting(name string) {
	hn, ok := hni.hyperNodes[name]
	if !ok {
		klog.ErrorS(nil, "HyperNode not exists in cache", "name", name)
		return
	}
	hn.isDeleting = true
}

// HyperNodeIsDeleting checks if a HyperNode is being deleted.
func (hni *HyperNodesInfo) HyperNodeIsDeleting(name string) bool {
	hn, ok := hni.hyperNodes[name]
	if !ok {
		klog.ErrorS(nil, "HyperNode not exists in cache", "name", name)
		return false
	}
	return hn.isDeleting
}
