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
			nodeInfoMap: make(map[string]fwk.NodeInfo),
		},
		volcanoInfo: volcanoInfo{
			nodeInfoMap:  make(map[string]*api.NodeInfo),
			nodeInfoList: make([]*api.NodeInfo, 0),
		},
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
