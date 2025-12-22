/*
Copyright 2019 The Kubernetes Authors.
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

// Copied from https://github.com/kubernetes/kubernetes/blob/v1.18.3/pkg/scheduler/internal/cache/snapshot.go
// as internal package is not allowed to import
// Modifications by Volcano Authors:
// 1. Added Volcano's own NodeInfo (`api.NodeInfo`) to the snapshot.
// 2. Added methods to retrieve Volcano's NodeInfo.

package k8s

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	scheduler "volcano.sh/volcano/pkg/scheduler/framework"
)

// Snapshot is a snapshot of cache NodeInfo and NodeTree order. The scheduler takes a
// snapshot at the beginning of each scheduling cycle and uses it for its operations in that cycle.
type Snapshot struct {
	fwkInfo
	volcanoInfo

	// generation is the snapshot generation, used to identify whether the snapshot is stale.
	generation int64

	// nodeNameToIndex maps node name to index in both nodeInfoList for O(1) lookup
	nodeNameToIndex map[string]int
	// affinityNodeIndex maps node name to index in havePodsWithAffinityNodeInfoList
	affinityNodeIndex map[string]int
	// antiAffinityNodeIndex maps node name to index in havePodsWithRequiredAntiAffinityNodeInfoList
	antiAffinityNodeIndex map[string]int
}

// fwkInfo holds snapshot information from the kube-scheduler framework.
type fwkInfo struct {
	// nodeInfoMap a map of node name to a snapshot of its NodeInfo.
	nodeInfoMap map[string]fwk.NodeInfo
	// nodeInfoList is the list of nodes as ordered in the cache's nodeTree.
	nodeInfoList []fwk.NodeInfo
	// havePodsWithAffinityNodeInfoList is the list of nodes with at least one pod declaring affinity terms.
	havePodsWithAffinityNodeInfoList []fwk.NodeInfo
	// havePodsWithRequiredAntiAffinityNodeInfoList is the list of nodes with at least one pod declaring
	// required anti-affinity terms.
	havePodsWithRequiredAntiAffinityNodeInfoList []fwk.NodeInfo
}

// volcanoInfo holds snapshot information for Volcano.
type volcanoInfo struct {
	// volcanoNodeInfoMap a map of node name to a snapshot of its volcano NodeInfo.
	nodeInfoMap map[string]*api.NodeInfo
	// volcanoNodeInfoList is the list of volcano nodes
	nodeInfoList []*api.NodeInfo
}

var _ framework.SharedLister = &Snapshot{}

// NewEmptySnapshot initializes a Snapshot struct and returns it.
func NewEmptySnapshot() *Snapshot {
	return &Snapshot{
		fwkInfo: fwkInfo{
			nodeInfoMap:                      make(map[string]fwk.NodeInfo),
			nodeInfoList:                     make([]fwk.NodeInfo, 0),
			havePodsWithAffinityNodeInfoList: make([]fwk.NodeInfo, 0),
			havePodsWithRequiredAntiAffinityNodeInfoList: make([]fwk.NodeInfo, 0),
		},
		volcanoInfo: volcanoInfo{
			nodeInfoMap:  make(map[string]*api.NodeInfo),
			nodeInfoList: make([]*api.NodeInfo, 0),
		},
		generation:            0,
		nodeNameToIndex:       make(map[string]int),
		affinityNodeIndex:     make(map[string]int),
		antiAffinityNodeIndex: make(map[string]int),
	}
}

// NewSnapshot initializes a Snapshot struct and returns it. It's only used in batch scheduler(session scheduling) now.
// For agent scheduler(fast path scheduling), it needs to use NewEmptySnapshot and update the snapshot from cache incrementally.
func NewSnapshot(nodeInfoMap map[string]fwk.NodeInfo) *Snapshot {
	nodeInfoList := make([]fwk.NodeInfo, 0, len(nodeInfoMap))
	havePodsWithAffinityNodeInfoList := make([]fwk.NodeInfo, 0, len(nodeInfoMap))
	havePodsWithRequiredAntiAffinityNodeInfoList := make([]fwk.NodeInfo, 0, len(nodeInfoMap))
	for _, v := range nodeInfoMap {
		nodeInfoList = append(nodeInfoList, v)
		if len(v.GetPodsWithAffinity()) > 0 {
			havePodsWithAffinityNodeInfoList = append(havePodsWithAffinityNodeInfoList, v)
		}
		if len(v.GetPodsWithRequiredAntiAffinity()) > 0 {
			havePodsWithRequiredAntiAffinityNodeInfoList = append(havePodsWithRequiredAntiAffinityNodeInfoList, v)
		}
	}

	s := NewEmptySnapshot()
	s.fwkInfo.nodeInfoMap = nodeInfoMap
	s.fwkInfo.nodeInfoList = nodeInfoList
	s.fwkInfo.havePodsWithAffinityNodeInfoList = havePodsWithAffinityNodeInfoList
	s.fwkInfo.havePodsWithRequiredAntiAffinityNodeInfoList = havePodsWithRequiredAntiAffinityNodeInfoList

	return s
}

func (s *Snapshot) AddOrUpdateNodes(nodes []*api.NodeInfo) {
	for _, node := range nodes {
		s.addOrUpdateNode(node)
	}
}

// addOrUpdateNode adds or updates node information in both fwkInfo and volcanoInfo.
func (s *Snapshot) addOrUpdateNode(nodeInfo *api.NodeInfo) {
	// Create Volcano NodeInfo
	volcanoNodeInfo := nodeInfo.Clone()
	nodeName := volcanoNodeInfo.Node.Name
	// Create k8s NodeInfo from vcNodeInfo
	fwkNodeInfo := framework.NewNodeInfo(volcanoNodeInfo.Pods()...)
	fwkNodeInfo.SetNode(volcanoNodeInfo.Node)

	idx, exists := s.nodeNameToIndex[nodeName]
	if exists {
		// Update existing node in both lists
		s.volcanoInfo.nodeInfoList[idx] = volcanoNodeInfo
		s.fwkInfo.nodeInfoList[idx] = fwkNodeInfo
	} else {
		// New node, add to both lists and update index
		idx = len(s.fwkInfo.nodeInfoList)
		s.nodeNameToIndex[nodeName] = idx
		s.fwkInfo.nodeInfoList = append(s.fwkInfo.nodeInfoList, fwkNodeInfo)
		s.volcanoInfo.nodeInfoList = append(s.volcanoInfo.nodeInfoList, volcanoNodeInfo)
	}
	// Update maps
	s.volcanoInfo.nodeInfoMap[nodeName] = volcanoNodeInfo
	s.fwkInfo.nodeInfoMap[nodeName] = fwkNodeInfo

	// Check if node was in affinity lists
	wasInAffinityList := false
	wasInRequiredAntiAffinityList := false

	// Check if node was in affinity lists
	if oldNodeInfo, exists := s.fwkInfo.nodeInfoMap[nodeName]; exists {
		if len(oldNodeInfo.GetPodsWithAffinity()) > 0 {
			wasInAffinityList = true
		}
		if len(oldNodeInfo.GetPodsWithRequiredAntiAffinity()) > 0 {
			wasInRequiredAntiAffinityList = true
		}
	}

	// Update affinity lists if needed
	hasAffinityPods := len(fwkNodeInfo.GetPodsWithAffinity()) > 0
	hasRequiredAntiAffinityPods := len(fwkNodeInfo.GetPodsWithRequiredAntiAffinity()) > 0

	// Update havePodsWithAffinityNodeInfoList
	s.updateAffinityList(
		&s.fwkInfo.havePodsWithAffinityNodeInfoList,
		s.affinityNodeIndex,
		nodeName, fwkNodeInfo, wasInAffinityList, hasAffinityPods,
	)

	// Update havePodsWithRequiredAntiAffinityNodeInfoList
	s.updateAffinityList(
		&s.fwkInfo.havePodsWithRequiredAntiAffinityNodeInfoList,
		s.antiAffinityNodeIndex,
		nodeName, fwkNodeInfo, wasInRequiredAntiAffinityList, hasRequiredAntiAffinityPods,
	)
	klog.V(5).Infof("Updated node %s in snapshot", nodeInfo.Name)
}

// DeleteNode removes node information from both fwkInfo and volcanoInfo.
func (s *Snapshot) DeleteNode(nodeName string) {
	idx, exists := s.nodeNameToIndex[nodeName]
	if !exists {
		return
	}
	lastIdx := len(s.fwkInfo.nodeInfoList) - 1

	if idx < lastIdx {
		// Swap the node to be deleted with the last node.
		lastNodeName := s.fwkInfo.nodeInfoList[lastIdx].Node().Name

		// Swap fwkInfo.nodeInfoList
		s.fwkInfo.nodeInfoList[idx] = s.fwkInfo.nodeInfoList[lastIdx]
		s.fwkInfo.nodeInfoList = s.fwkInfo.nodeInfoList[:lastIdx]

		// Swap volcanoInfo.nodeInfoList
		s.volcanoInfo.nodeInfoList[idx] = s.volcanoInfo.nodeInfoList[lastIdx]
		s.volcanoInfo.nodeInfoList = s.volcanoInfo.nodeInfoList[:lastIdx]

		s.nodeNameToIndex[lastNodeName] = idx
	} else {
		s.fwkInfo.nodeInfoList = s.fwkInfo.nodeInfoList[:lastIdx]
		s.volcanoInfo.nodeInfoList = s.volcanoInfo.nodeInfoList[:lastIdx]
	}

	delete(s.nodeNameToIndex, nodeName)
	delete(s.fwkInfo.nodeInfoMap, nodeName)
	delete(s.volcanoInfo.nodeInfoMap, nodeName)

	s.removeFromAffinityList(s.affinityNodeIndex, nodeName, &s.fwkInfo.havePodsWithAffinityNodeInfoList)
	s.removeFromAffinityList(s.antiAffinityNodeIndex, nodeName, &s.fwkInfo.havePodsWithRequiredAntiAffinityNodeInfoList)
}

// removeFromAffinityList remove node from the affinity list.
func (s *Snapshot) removeFromAffinityList(indexMap map[string]int, nodeName string, list *[]fwk.NodeInfo) {
	idx, exists := indexMap[nodeName]
	if !exists {
		return
	}

	lastIdx := len(*list) - 1
	if idx < lastIdx {
		lastNodeName := (*list)[lastIdx].Node().Name
		(*list)[idx] = (*list)[lastIdx]
		*list = (*list)[:lastIdx]
		indexMap[lastNodeName] = idx
	} else {
		*list = (*list)[:lastIdx]
	}

	delete(indexMap, nodeName)
}

// RemoveDeletedNodesFromSnapshot removes nodes that are not in the cache from the snapshot
func (s *Snapshot) RemoveDeletedNodesFromSnapshot(currentNodeNames map[string]bool) {
	nodesToDelete := make([]string, 0, len(s.nodeNameToIndex))
	for nodeName := range s.nodeNameToIndex {
		if !currentNodeNames[nodeName] {
			nodesToDelete = append(nodesToDelete, nodeName)
		}
	}
	for _, nodeName := range nodesToDelete {
		s.DeleteNode(nodeName)
	}
}

// updateAffinityList updates an affinity list based on node changes
func (s *Snapshot) updateAffinityList(list *[]fwk.NodeInfo, indexMap map[string]int, nodeName string, nodeInfo fwk.NodeInfo, wasInList, shouldBeInList bool) {
	if wasInList {
		s.removeFromAffinityList(indexMap, nodeName, list)
	}
	if shouldBeInList {
		indexMap[nodeName] = len(*list)
		*list = append(*list, nodeInfo)
	}
}

// Pods returns a PodLister
func (s *Snapshot) Pods() scheduler.PodsLister {
	return podLister(s.fwkInfo.nodeInfoList)
}

// NodeInfos returns a NodeInfoLister.
func (s *Snapshot) NodeInfos() framework.NodeInfoLister {
	return s
}

// StorageInfos returns a StorageInfoLister.
func (s *Snapshot) StorageInfos() framework.StorageInfoLister {
	return s
}

// VolcanoNodeInfos returns a list of volcano NodeInfo.
func (s *Snapshot) GetK8sNodeInfo(nodeName string) (fwk.NodeInfo, error) {
	if v, ok := s.fwkInfo.nodeInfoMap[nodeName]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("nodeinfo not found for node name %q", nodeName)
}

// VolcanoNodeInfos returns a list of volcano NodeInfo.
func (s *Snapshot) VolcanoNodeInfos() []*api.NodeInfo {
	return s.volcanoInfo.nodeInfoList
}

// GetVolcanoNodeInfo returns the volcano NodeInfo of the given node name.
func (s *Snapshot) GetVolcanoNodeInfo(nodeName string) (*api.NodeInfo, error) {
	if v, ok := s.volcanoInfo.nodeInfoMap[nodeName]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("nodeinfo not found for node name %q", nodeName)
}

// GetFwkNodeInfoMap returns internal fwk nodeInfoMap
func (s *Snapshot) GetFwkNodeInfoMap() map[string]fwk.NodeInfo {
	return s.fwkInfo.nodeInfoMap
}

// GetVolcanoNodeInfoMap returns internal volcano nodeInfoMap
func (s *Snapshot) GetVolcanoNodeInfoMap() map[string]*api.NodeInfo {
	return s.volcanoInfo.nodeInfoMap
}

// GetFwkNodeInfoList returns internal fwk nodeInfoList
func (s *Snapshot) GetFwkNodeInfoList() []fwk.NodeInfo {
	return s.fwkInfo.nodeInfoList
}

// GetVolcanoNodeInfoList returns internal volcano nodeInfoList
func (s *Snapshot) GetVolcanoNodeInfoList() []*api.NodeInfo {
	return s.volcanoInfo.nodeInfoList
}

type podLister []fwk.NodeInfo

// List returns the list of pods in the snapshot.
func (p podLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	alwaysTrue := func(*v1.Pod) bool { return true }
	return p.FilteredList(alwaysTrue, selector)
}

// FilteredList returns a filtered list of pods in the snapshot.
func (p podLister) FilteredList(filter scheduler.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	// podFilter is expected to return true for most or all of the pods. We
	// can avoid expensive array growth without wasting too much memory by
	// pre-allocating capacity.
	maxSize := 0
	for _, n := range p {
		maxSize += len(n.GetPods())
	}
	pods := make([]*v1.Pod, 0, maxSize)
	for _, n := range p {
		for _, pod := range n.GetPods() {
			if filter(pod.GetPod()) && selector.Matches(labels.Set(pod.GetPod().Labels)) {
				pods = append(pods, pod.GetPod())
			}
		}
	}
	return pods, nil
}

// List returns the list of nodes in the snapshot.
func (s *Snapshot) List() ([]fwk.NodeInfo, error) {
	return s.fwkInfo.nodeInfoList, nil
}

// HavePodsWithAffinityList returns the list of nodes with at least one pods with inter-pod affinity
func (s *Snapshot) HavePodsWithAffinityList() ([]fwk.NodeInfo, error) {
	return s.fwkInfo.havePodsWithAffinityNodeInfoList, nil
}

// HavePodsWithRequiredAntiAffinityList returns the list of NodeInfos of nodes with pods with required anti-affinity terms.
func (s *Snapshot) HavePodsWithRequiredAntiAffinityList() ([]fwk.NodeInfo, error) {
	return s.fwkInfo.havePodsWithRequiredAntiAffinityNodeInfoList, nil
}

// Get returns the NodeInfo of the given node name.
func (s *Snapshot) Get(nodeName string) (fwk.NodeInfo, error) {
	if v, ok := s.fwkInfo.nodeInfoMap[nodeName]; ok && v.Node() != nil {
		return v, nil
	}
	return nil, fmt.Errorf("nodeinfo not found for node name %q", nodeName)
}

func (s *Snapshot) IsPVCUsedByPods(key string) bool {
	panic("not implemented")
}

func (s *Snapshot) GetGeneration() int64 {
	return s.generation
}

func (s *Snapshot) SetGeneration(currentGeneration int64) {
	s.generation = currentGeneration
}
