/*
Copyright 2019 The Kubernetes Authors.

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

package k8s

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"

	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// Snapshot is a snapshot of cache NodeInfo and NodeTree order. The scheduler takes a
// snapshot at the beginning of each scheduling cycle and uses it for its operations in that cycle.
type Snapshot struct {
	// nodeInfoMap a map of node name to a snapshot of its NodeInfo.
	nodeInfoMap map[string]*v1alpha1.NodeInfo
	// nodeInfoList is the list of nodes as ordered in the cache's nodeTree.
	nodeInfoList []*v1alpha1.NodeInfo
	// havePodsWithAffinityNodeInfoList is the list of nodes with at least one pod declaring affinity terms.
	havePodsWithAffinityNodeInfoList []*v1alpha1.NodeInfo
	// havePodsWithRequiredAntiAffinityNodeInfoList is the list of nodes with at least one pod declaring
	// required anti-affinity terms.
	havePodsWithRequiredAntiAffinityNodeInfoList []*v1alpha1.NodeInfo
}

var _ v1alpha1.SharedLister = &Snapshot{}

// NewEmptySnapshot initializes a Snapshot struct and returns it.
func NewEmptySnapshot() *Snapshot {
	return &Snapshot{
		nodeInfoMap: make(map[string]*v1alpha1.NodeInfo),
	}
}

// NewSnapshot initializes a Snapshot struct and returns it.
func NewSnapshot(nodeInfoMap map[string]*v1alpha1.NodeInfo) *Snapshot {
	nodeInfoList := make([]*v1alpha1.NodeInfo, 0, len(nodeInfoMap))
	havePodsWithAffinityNodeInfoList := make([]*v1alpha1.NodeInfo, 0, len(nodeInfoMap))
	havePodsWithRequiredAntiAffinityNodeInfoList := make([]*v1alpha1.NodeInfo, 0, len(nodeInfoMap))
	for _, v := range nodeInfoMap {
		nodeInfoList = append(nodeInfoList, v)
		if len(v.PodsWithAffinity) > 0 {
			havePodsWithAffinityNodeInfoList = append(havePodsWithAffinityNodeInfoList, v)
		}
		if len(v.PodsWithRequiredAntiAffinity) > 0 {
			havePodsWithRequiredAntiAffinityNodeInfoList = append(havePodsWithRequiredAntiAffinityNodeInfoList, v)
		}
	}

	s := NewEmptySnapshot()
	s.nodeInfoMap = nodeInfoMap
	s.nodeInfoList = nodeInfoList
	s.havePodsWithAffinityNodeInfoList = havePodsWithAffinityNodeInfoList
	s.havePodsWithRequiredAntiAffinityNodeInfoList = havePodsWithRequiredAntiAffinityNodeInfoList

	return s
}

// Pods returns a PodLister
func (s *Snapshot) Pods() util.PodsLister {
	return podLister(s.nodeInfoList)
}

// NodeInfos returns a NodeInfoLister.
func (s *Snapshot) NodeInfos() v1alpha1.NodeInfoLister {
	return s
}

type podLister []*v1alpha1.NodeInfo

// List returns the list of pods in the snapshot.
func (p podLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	alwaysTrue := func(*v1.Pod) bool { return true }
	return p.FilteredList(alwaysTrue, selector)
}

// FilteredList returns a filtered list of pods in the snapshot.
func (p podLister) FilteredList(filter util.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	// podFilter is expected to return true for most or all of the pods. We
	// can avoid expensive array growth without wasting too much memory by
	// pre-allocating capacity.
	maxSize := 0
	for _, n := range p {
		maxSize += len(n.Pods)
	}
	pods := make([]*v1.Pod, 0, maxSize)
	for _, n := range p {
		for _, pod := range n.Pods {
			if filter(pod.Pod) && selector.Matches(labels.Set(pod.Pod.Labels)) {
				pods = append(pods, pod.Pod)
			}
		}
	}
	return pods, nil
}

// List returns the list of nodes in the snapshot.
func (s *Snapshot) List() ([]*v1alpha1.NodeInfo, error) {
	return s.nodeInfoList, nil
}

// HavePodsWithAffinityList returns the list of nodes with at least one pods with inter-pod affinity
func (s *Snapshot) HavePodsWithAffinityList() ([]*v1alpha1.NodeInfo, error) {
	return s.havePodsWithAffinityNodeInfoList, nil
}

// HavePodsWithRequiredAntiAffinityList returns the list of NodeInfos of nodes with pods with required anti-affinity terms.
func (s *Snapshot) HavePodsWithRequiredAntiAffinityList() ([]*v1alpha1.NodeInfo, error) {
	return s.havePodsWithRequiredAntiAffinityNodeInfoList, nil
}

// Get returns the NodeInfo of the given node name.
func (s *Snapshot) Get(nodeName string) (*v1alpha1.NodeInfo, error) {
	if v, ok := s.nodeInfoMap[nodeName]; ok && v.Node() != nil {
		return v, nil
	}
	return nil, fmt.Errorf("nodeinfo not found for node name %q", nodeName)
}
