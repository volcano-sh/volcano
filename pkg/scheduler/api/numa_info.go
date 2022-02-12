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
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
)

// NumaChgFlag indicate node numainfo changed status
type NumaChgFlag int

const (
	// NumaInfoResetFlag indicate reset operate
	NumaInfoResetFlag NumaChgFlag = 0b00
	// NumaInfoMoreFlag indicate the received allocatable resource is getting more
	NumaInfoMoreFlag NumaChgFlag = 0b11
	// NumaInfoLessFlag indicate the received allocatable resource is getting less
	NumaInfoLessFlag NumaChgFlag = 0b10
)

// PodResourceDecision is resource allocation determinated by scheduler,
// and passed to kubelet through pod annotation.
type PodResourceDecision struct {
	// NUMAResources is resource list with numa info indexed by numa id.
	NUMAResources map[int]v1.ResourceList `json:"numa,omitempty"`
}

// ResourceInfo is the allocatable information for the resource
type ResourceInfo struct {
	Allocatable        cpuset.CPUSet
	Capacity           int
	AllocatablePerNuma map[int]float64 // key: NUMA ID
	UsedPerNuma        map[int]float64 // key: NUMA ID
}

// NumatopoInfo is the information about topology manager on the node
type NumatopoInfo struct {
	Namespace   string
	Name        string
	Policies    map[nodeinfov1alpha1.PolicyName]string
	NumaResMap  map[string]*ResourceInfo
	CPUDetail   topology.CPUDetails
	ResReserved v1.ResourceList
}

// DeepCopy used to copy NumatopoInfo
func (info *NumatopoInfo) DeepCopy() *NumatopoInfo {
	numaInfo := &NumatopoInfo{
		Namespace:   info.Namespace,
		Name:        info.Name,
		Policies:    make(map[nodeinfov1alpha1.PolicyName]string),
		NumaResMap:  make(map[string]*ResourceInfo),
		CPUDetail:   topology.CPUDetails{},
		ResReserved: make(v1.ResourceList),
	}

	policies := info.Policies
	for name, policy := range policies {
		numaInfo.Policies[name] = policy
	}

	for resName, resInfo := range info.NumaResMap {
		tmpInfo := &ResourceInfo{
			AllocatablePerNuma: make(map[int]float64),
			UsedPerNuma:        make(map[int]float64),
		}
		tmpInfo.Capacity = resInfo.Capacity
		tmpInfo.Allocatable = resInfo.Allocatable.Clone()

		for numaID, data := range resInfo.AllocatablePerNuma {
			tmpInfo.AllocatablePerNuma[numaID] = data
		}

		for numaID, data := range resInfo.UsedPerNuma {
			tmpInfo.UsedPerNuma[numaID] = data
		}

		numaInfo.NumaResMap[resName] = tmpInfo
	}

	cpuDetail := info.CPUDetail
	for cpuID, detail := range cpuDetail {
		numaInfo.CPUDetail[cpuID] = detail
	}

	resReserved := info.ResReserved
	for resName, res := range resReserved {
		numaInfo.ResReserved[resName] = res
	}

	return numaInfo
}

// Compare is the function to show the change of the resource on kubelet
// return val:
// - true : the resource on kubelet is getting more or no change
// - false :  the resource on kubelet is getting less
func (info *NumatopoInfo) Compare(newInfo *NumatopoInfo) bool {
	for resName := range info.NumaResMap {
		oldSize := info.NumaResMap[resName].Allocatable.Size()
		newSize := newInfo.NumaResMap[resName].Allocatable.Size()
		if oldSize <= newSize {
			return true
		}
	}

	return false
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

func GetPodResourceNumaInfo(ti *TaskInfo) map[int]v1.ResourceList {
	if ti.NumaInfo != nil && len(ti.NumaInfo.ResMap) > 0 {
		return ti.NumaInfo.ResMap
	}

	if _, ok := ti.Pod.Annotations[topologyDecisionAnnotation]; !ok {
		return nil
	}

	decision := PodResourceDecision{}
	err := json.Unmarshal([]byte(ti.Pod.Annotations[topologyDecisionAnnotation]), &decision)
	if err != nil {
		return nil
	}

	return decision.NUMAResources
}

// AddTask is the function to update the used resource of per numa node
func (info *NumatopoInfo) AddTask(ti *TaskInfo) {
	numaInfo := GetPodResourceNumaInfo(ti)
	if numaInfo == nil {
		return
	}

	for numaID, resList := range numaInfo {
		for resName, quantity := range resList {
			info.NumaResMap[string(resName)].UsedPerNuma[numaID] += ResQuantity2Float64(resName, quantity)
		}
	}
}

// RemoveTask is the function to update the used resource of per numa node
func (info *NumatopoInfo) RemoveTask(ti *TaskInfo) {
	decision := GetPodResourceNumaInfo(ti)
	if decision == nil {
		return
	}

	for numaID, resList := range ti.NumaInfo.ResMap {
		for resName, quantity := range resList {
			info.NumaResMap[string(resName)].UsedPerNuma[numaID] -= ResQuantity2Float64(resName, quantity)
		}
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

		nodeNumaMap[node.Name] = node.NumaSchedulerInfo.CPUDetail.NUMANodes().ToSlice()
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
