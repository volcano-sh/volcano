/*
Copyright 2021 The Volcano Authors.

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
	"encoding/json"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/utils/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
)

const (
	// DefaultMaxNodeScore indicates the default max node score
	DefaultMaxNodeScore = 100
)

// PodResourceDecision is resource allocation determined by scheduler,
// and passed to kubelet through pod annotation.
type PodResourceDecision struct {
	// NUMAResources is resource list with numa info indexed by numa id.
	NUMAResources map[int]v1.ResourceList `json:"numa,omitempty"`
}

// ResourceInfo is the allocatable information for the resource
type ResourceInfo struct {
	Allocatable cpuset.CPUSet
	Capacity    int
}

// NumatopoInfo is the information about topology manager on the node
type NumatopoInfo struct {
	Namespace      string
	Name           string
	Policies       map[nodeinfov1alpha1.PolicyName]string
	NumaResMap     map[string]*ResourceInfo
	CPUDetail      topology.CPUDetails
	ResReserved    v1.ResourceList
	PodAllocations []nodeinfov1alpha1.PodAllocation
}

// DeepCopy used to copy NumatopoInfo
func (info *NumatopoInfo) DeepCopy() *NumatopoInfo {
	numaInfo := &NumatopoInfo{
		Namespace:      info.Namespace,
		Name:           info.Name,
		Policies:       make(map[nodeinfov1alpha1.PolicyName]string),
		NumaResMap:     make(map[string]*ResourceInfo),
		CPUDetail:      topology.CPUDetails{},
		ResReserved:    make(v1.ResourceList),
		PodAllocations: make([]nodeinfov1alpha1.PodAllocation, 0, len(info.PodAllocations)),
	}

	policies := info.Policies
	for name, policy := range policies {
		numaInfo.Policies[name] = policy
	}

	for resName, resInfo := range info.NumaResMap {
		numaInfo.NumaResMap[resName] = &ResourceInfo{
			Capacity:    resInfo.Capacity,
			Allocatable: resInfo.Allocatable.Clone(),
		}
	}

	cpuDetail := info.CPUDetail
	for cpuID, detail := range cpuDetail {
		numaInfo.CPUDetail[cpuID] = detail
	}

	resReserved := info.ResReserved
	for resName, res := range resReserved {
		numaInfo.ResReserved[resName] = res
	}

	podAllocations := info.PodAllocations
	for _, podAllocation := range podAllocations {
		numaInfo.PodAllocations = append(numaInfo.PodAllocations, *podAllocation.DeepCopy())
	}

	return numaInfo
}

// Allocate is the function to remove the allocated resource
func (info *NumatopoInfo) Allocate(resSets ResNumaSets) {
	for resName := range resSets {
		info.NumaResMap[resName].Allocatable = info.NumaResMap[resName].Allocatable.Difference(resSets[resName])
	}
}

// Release is the function to reclaim the allocated resource
func (info *NumatopoInfo) Release(resSets ResNumaSets) {
	for resName := range resSets {
		info.NumaResMap[resName].Allocatable = info.NumaResMap[resName].Allocatable.Union(resSets[resName])
	}
}

// GenerateNodeResNumaSets return the idle resource sets of all node
func GenerateNodeResNumaSets(nodes map[string]*NodeInfo) map[string]ResNumaSets {
	nodeSlice := make(map[string]ResNumaSets)
	for _, node := range nodes {
		if node.NumaSchedulerInfo == nil {
			continue
		}

		resMaps := make(ResNumaSets)
		for resName, resMap := range node.NumaSchedulerInfo.NumaResMap {
			resMaps[resName] = resMap.Allocatable.Clone()
		}

		nodeSlice[node.Name] = resMaps
	}

	return nodeSlice
}

// GenerateNumaNodes return the numa IDs of all node
func GenerateNumaNodes(nodes map[string]*NodeInfo) map[string][]int {
	nodeNumaMap := make(map[string][]int)

	for _, node := range nodes {
		if node.NumaSchedulerInfo == nil {
			continue
		}

		nodeNumaMap[node.Name] = node.NumaSchedulerInfo.CPUDetail.NUMANodes().List()
	}

	return nodeNumaMap
}

// ResNumaSets is the set map of the resource
type ResNumaSets map[string]cpuset.CPUSet

// Allocate is to remove the allocated resource which is assigned to task
func (resSets ResNumaSets) Allocate(taskSets ResNumaSets) {
	for resName := range taskSets {
		if _, ok := resSets[resName]; !ok {
			continue
		}
		resSets[resName] = resSets[resName].Difference(taskSets[resName])
	}
}

// Release is to reclaim the allocated resource which is assigned to task
func (resSets ResNumaSets) Release(taskSets ResNumaSets) {
	for resName := range taskSets {
		if _, ok := resSets[resName]; !ok {
			continue
		}
		resSets[resName] = resSets[resName].Union(taskSets[resName])
	}
}

// Clone is the copy action
func (resSets ResNumaSets) Clone() ResNumaSets {
	newSets := make(ResNumaSets)
	for resName := range resSets {
		newSets[resName] = resSets[resName].Clone()
	}

	return newSets
}

// ScoredNode is the wrapper for node during Scoring.
type ScoredNode struct {
	NodeName string
	Score    int64
}

type PodMeta struct {
	UID       types.UID `json:"uid"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
}

// MarshalText encodes PodMeta as a compact JSON object so it can be used as a
// map key by encoding/json, which requires map key types to be strings,
// integer types, or implement encoding.TextMarshaler.
func (p PodMeta) MarshalText() ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalText reverses MarshalText.
func (p *PodMeta) UnmarshalText(data []byte) error {
	return json.Unmarshal(data, p)
}

// CheckNumaPodAssigned returns true if the specified pod has been assigned numa resources, returns false otherwise.
// Both UID and the (Namespace, Name) pair are matched because the resource-exporter uses
// UID when reading cpu_manager_state, and Namespace+Name when querying the PodResources API.
func (info *NumatopoInfo) CheckNumaPodAssigned(podMeta PodMeta) bool {
	for _, podAlloc := range info.PodAllocations {
		if types.UID(podAlloc.UID) == podMeta.UID || (podAlloc.Name == podMeta.Name && podAlloc.Namespace == podMeta.Namespace) {
			return true
		}
	}
	return false
}
