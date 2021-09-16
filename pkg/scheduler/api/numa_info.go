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

// TopoInfo is the detail information for the resource
type TopoInfo struct {
	Set cpuset.CPUSet
	Total float64
}

// ResourceInfo is the allocatable information for the resource
type ResourceInfo struct {
	// the allocatable information in per numa node
	NumaAllocatable map[int]*TopoInfo
	// the total allocatable resource
	Allocatable float64
	// the used information in per numa node
	NumaUsed map[int]*TopoInfo
	// the total used resource
	Used float64
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

// DeepCopy used to copy TopoInfo
func (info *TopoInfo) DeepCopy() *TopoInfo {
	return &TopoInfo{
		Set: info.Set.Clone(),
		Total: info.Total,
	}
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
		tmpInfo := &ResourceInfo {
			NumaAllocatable: make(map[int]*TopoInfo),
			NumaUsed: make(map[int]*TopoInfo),
		}

		for numaId, data := range resInfo.NumaAllocatable {
			topoInfo := &TopoInfo{
				Set: data.Set.Clone(),
				Total: data.Total,
			}
			tmpInfo.NumaAllocatable[numaId] = topoInfo
		}

		for numaId, data := range resInfo.NumaUsed {
			topoInfo := &TopoInfo{
				Set: data.Set.Clone(),
				Total: data.Total,
			}
			tmpInfo.NumaUsed[numaId] = topoInfo
		}

		tmpInfo.Allocatable = resInfo.Allocatable
		tmpInfo.Used = resInfo.Used
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
		oldSize := info.NumaResMap[resName].Allocatable - info.NumaResMap[resName].Used
		newSize := newInfo.NumaResMap[resName].Allocatable - newInfo.NumaResMap[resName].Used
		if oldSize < newSize {
			return true
		}
	}

	return false
}

// Allocate is the function to remove the allocated resource
func (info *NumatopoInfo) Allocate(resSets ResNumaSets) {
	for resName, set := range resSets {
		resNumaInfo := info.NumaResMap[resName]
		resNumaInfo.Used += float64(set.Size() * 1000)

		for _, CPUId := range set.ToSlice() {
			for numaId, topoInfo := range resNumaInfo.NumaAllocatable {
				if topoInfo.Set.Contains(CPUId) {
					if _, ok := resNumaInfo.NumaUsed[numaId]; !ok {
						resNumaInfo.NumaUsed[numaId] = &TopoInfo{
							Set: cpuset.NewCPUSet(),
						}
					}

					resNumaInfo.NumaUsed[numaId].Set = resNumaInfo.NumaUsed[numaId].Set.Union(cpuset.NewCPUSet(CPUId))
					resNumaInfo.NumaUsed[numaId].Total -= 1000
					break
				}
			}
		}
	}
}

// Release is the function to reclaim the allocated resource
func (info *NumatopoInfo) Release(resSets ResNumaSets) {
	for resName, set := range resSets {
		resNumaInfo := info.NumaResMap[resName]
		resNumaInfo.Used -= float64(set.Size() * 1000)

		for _, CPUId := range set.ToSlice() {
			for numaId, topoInfo := range resNumaInfo.NumaAllocatable {
				if topoInfo.Set.Contains(CPUId) {
					if _, ok := resNumaInfo.NumaUsed[numaId]; !ok {
						resNumaInfo.NumaUsed[numaId] = &TopoInfo{
							Set: cpuset.NewCPUSet(),
						}
					}

					resNumaInfo.NumaUsed[numaId].Set = resNumaInfo.NumaUsed[numaId].Set.Difference(cpuset.NewCPUSet(CPUId))
					resNumaInfo.NumaUsed[numaId].Total -= 1000
					break
				}
			}
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
			useSet := cpuset.NewCPUSet()
			for _, topoInfo := range resMap.NumaUsed {
				useSet = useSet.Union(topoInfo.Set)
			}

			AllocatableSet := cpuset.NewCPUSet()
			for _, topoInfo := range resMap.NumaAllocatable {
				AllocatableSet = AllocatableSet.Union(topoInfo.Set)
			}

			resMaps[resName] = AllocatableSet.Difference(useSet)
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
