package ccinuma

import (
	"context"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// PodResourceDecision is resource allocation determinated by scheduler,
// and passed to kubelet through pod annotation.
type PodResourceDecision struct {
	// NUMAResources is resource list with numa info indexed by numa id.
	NUMAResources map[int]v1.ResourceList `json:"numa,omitempty"`
}


type TopologyHint struct {
	NUMANodeAffinity bitmask.BitMask
	// Preferred is set to true when the NUMANodeAffinity encodes a preferred
	// allocation for the Container. It is set to false otherwise.
	Preferred bool
}

type ResNumaSetType map[v1.ResourceName]map[int]float64 // map[resourceName]map[numaId]float64

// GetNodeNumaRes return the assignable Quantity of per resource in per numa node
func GetNodeNumaRes(topoInfo *api.NumatopoInfo) ResNumaSetType {
	nodeNumaMap := make(ResNumaSetType)
	for resName, resInfo := range topoInfo.NumaResMap {
		numaMap := make(map[int]float64)
		for numaId, value := range resInfo.AllocatablePerNuma {
			if _, ok := resInfo.UsedPerNuma[numaId]; !ok {
				numaMap[numaId] = value
			} else {
				numaMap[numaId] = value - resInfo.UsedPerNuma[numaId]
			}
		}

		nodeNumaMap[v1.ResourceName(resName)] = numaMap
	}

	return nodeNumaMap
}

func GenerateTopologyHints(request float64, availableRes map[int]float64) []TopologyHint {
	hints := make([]TopologyHint, 0)
	var numaSlice []int
	for key := range availableRes {
		numaSlice = append(numaSlice, key)
	}

	sort.Ints(numaSlice)

	minAffinitySize := len(numaSlice)
	bitmask.IterateBitMasks(numaSlice, func(mask bitmask.BitMask) {
		var availableInMask float64
		for _, numaID := range mask.GetBits() {
			availableInMask += availableRes[numaID]
		}

		if availableInMask < request {
			return
		}

		if mask.Count() < minAffinitySize {
			minAffinitySize = mask.Count()
		}

		hints = append(hints, TopologyHint{
			NUMANodeAffinity: mask,
			Preferred:        false,
		})
	})

	for i := range hints {
		if hints[i].NUMANodeAffinity.Count() == minAffinitySize {
			hints[i].Preferred = true
		}
	}

	return hints
}

type hitInfo struct {
	hits []TopologyHint
	crossCount int
	Preferred bool
}

func MergeFilteredHints(task *api.TaskInfo, nodeNumaIdleMap ResNumaSetType, numaNodes []int, filteredHints [][]TopologyHint) TopologyHint {
	tmpHitInfo := hitInfo{}
	iterateAllProviderTopologyHints(filteredHints, func(permutation []TopologyHint) {
		// Get the NUMANodeAffinity from each hint in the permutation and see if any
		// of them encode unpreferred allocations.
		mergedHint := mergePermutation(numaNodes, permutation)
		// Only consider mergedHints that result in a NUMANodeAffinity > 0 to
		// replace the current bestHint.
		if mergedHint.NUMANodeAffinity.Count() == 0 {
			return
		}

		if tmpHitInfo.crossCount == 0 {
			tmpHitInfo.hits = append(tmpHitInfo.hits, mergedHint)
			tmpHitInfo.crossCount = mergedHint.NUMANodeAffinity.Count()
			tmpHitInfo.Preferred = mergedHint.Preferred
			return
		}

		// If the current bestHint is preferred and the new mergedHint is
		// non-preferred, never update bestHint, regardless of mergedHint's
		// narowness.
		if !mergedHint.Preferred && tmpHitInfo.Preferred {
			return
		}

		if mergedHint.Preferred && !tmpHitInfo.Preferred {
			tmpHitInfo.hits = []TopologyHint{mergedHint}
			tmpHitInfo.crossCount = mergedHint.NUMANodeAffinity.Count()
			tmpHitInfo.Preferred = mergedHint.Preferred
			return
		}

		if mergedHint.Preferred && tmpHitInfo.Preferred {
			if mergedHint.NUMANodeAffinity.Count() > tmpHitInfo.crossCount {
				tmpHitInfo.hits = []TopologyHint{mergedHint}
				tmpHitInfo.crossCount = mergedHint.NUMANodeAffinity.Count()
				tmpHitInfo.Preferred = mergedHint.Preferred
			} else if mergedHint.NUMANodeAffinity.Count() ==  tmpHitInfo.crossCount {
				tmpHitInfo.hits = append(tmpHitInfo.hits, mergedHint)
			}

			return
		}

		if !mergedHint.Preferred && !tmpHitInfo.Preferred {
			if mergedHint.NUMANodeAffinity.Count() < tmpHitInfo.crossCount {
				tmpHitInfo.hits = []TopologyHint{mergedHint}
				tmpHitInfo.crossCount = mergedHint.NUMANodeAffinity.Count()
				tmpHitInfo.Preferred = mergedHint.Preferred
			} else if mergedHint.NUMANodeAffinity.Count() ==  tmpHitInfo.crossCount {
				tmpHitInfo.hits = append(tmpHitInfo.hits, mergedHint)
			}

			return
		}
	})

	if len(tmpHitInfo.hits) == 1 {
		return tmpHitInfo.hits[0]
	}

	return selectBestHit(nodeNumaIdleMap, task, tmpHitInfo.hits)

}

func selectBestHit(nodeNumaIdleMap ResNumaSetType, task *api.TaskInfo, hits []TopologyHint) TopologyHint {
	var bestScore int64
	var bestHitLabel int
	for i := range hits {
		var externScore int64
		cpuScore := calcResourceScore(task.Resreq.MilliCPU, nodeNumaIdleMap[v1.ResourceCPU], hits[i].NUMANodeAffinity)
		memScore := calcResourceScore(task.Resreq.Memory, nodeNumaIdleMap[v1.ResourceMemory], hits[i].NUMANodeAffinity)
		for rName, rValue := range task.Resreq.ScalarResources {
			externScore += calcResourceScore(rValue, nodeNumaIdleMap[rName], hits[i].NUMANodeAffinity)
		}

		totalScore := cpuScore + memScore + (2 * externScore)
		if totalScore > bestScore {
			bestScore = totalScore
			bestHitLabel = i
		}
	}

	return hits[bestHitLabel]
}

func calcResourceScore(reqRes float64, resNumaSets map[int]float64, NUMANodeAffinity bitmask.BitMask) int64 {
	if len(resNumaSets) == 0 {
		return 0
	}

	var AllocatableTotal float64
	for _, numaId := range NUMANodeAffinity.GetBits() {
		AllocatableTotal += resNumaSets[numaId]
	}

	var requestPercent int64
	if reqRes <= AllocatableTotal {
		requestPercent = 100 + int64(reqRes * 100 / AllocatableTotal)
	}  else {
		requestPercent = 100 - int64((reqRes - AllocatableTotal) * 100 / reqRes)
	}

	return requestPercent
}

// Merge a TopologyHints permutation to a single hint by performing a bitwise-AND
// of their affinity masks. The hint shall be preferred if all hits in the permutation
// are preferred.
func mergePermutation(numaNodes []int, permutation []TopologyHint) TopologyHint {
	// Get the NUMANodeAffinity from each hint in the permutation and see if any
	// of them encode unpreferred allocations.
	preferred := true
	defaultAffinity, _ := bitmask.NewBitMask(numaNodes...)
	var numaAffinities []bitmask.BitMask
	for _, hint := range permutation {
		// Only consider hints that have an actual NUMANodeAffinity set.
		if hint.NUMANodeAffinity == nil {
			numaAffinities = append(numaAffinities, defaultAffinity)
		} else {
			numaAffinities = append(numaAffinities, hint.NUMANodeAffinity)
		}

		if !hint.Preferred {
			preferred = false
		}
	}

	// Merge the affinities using a bitwise-and operation.
	mergedAffinity := bitmask.And(defaultAffinity, numaAffinities...)
	// Build a mergedHint from the merged affinity mask, indicating if an
	// preferred allocation was used to generate the affinity mask or not.
	return TopologyHint{mergedAffinity, preferred}
}

// Iterate over all permutations of hints in 'allProviderHints [][]TopologyHint'.
//
// This procedure is implemented as a recursive function over the set of hints
// in 'allproviderHints[i]'. It applies the function 'callback' to each
// permutation as it is found. It is the equivalent of:
//
// for i := 0; i < len(providerHints[0]); i++
//     for j := 0; j < len(providerHints[1]); j++
//         for k := 0; k < len(providerHints[2]); k++
//             ...
//             for z := 0; z < len(providerHints[-1]); z++
//                 permutation := []TopologyHint{
//                     providerHints[0][i],
//                     providerHints[1][j],
//                     providerHints[2][k],
//                     ...
//                     providerHints[-1][z]
//                 }
//                 callback(permutation)
func iterateAllProviderTopologyHints(allProviderHints [][]TopologyHint, callback func([]TopologyHint)) {
	// Internal helper function to accumulate the permutation before calling the callback.
	var iterate func(i int, accum []TopologyHint)
	iterate = func(i int, accum []TopologyHint) {
		// Base case: we have looped through all providers and have a full permutation.
		if i == len(allProviderHints) {
			callback(accum)
			return
		}

		// Loop through all hints for provider 'i', and recurse to build the
		// the permutation of this hint with all hints from providers 'i++'.
		for j := range allProviderHints[i] {
			iterate(i+1, append(accum, allProviderHints[i][j]))
		}
	}
	iterate(0, []TopologyHint{})
}

func allocateNumaRes(reqRes float64, affinityNuma bitmask.BitMask, otherNuma bitmask.BitMask, resNumaIdleMap map[int]float64) map[int]float64 {
	resLayout := make(map[int]float64)
	resArray := make([][]int, 0)
	resArray = append(resArray, affinityNuma.GetBits(), otherNuma.GetBits())
	for _, numaSlice := range resArray {
		for _, numaID := range numaSlice {
			if reqRes <= 0 {
				break
			}

			if reqRes >= resNumaIdleMap[numaID] {
				reqRes -= resNumaIdleMap[numaID]
				resLayout[numaID] = resNumaIdleMap[numaID]
			} else {
				resLayout[numaID] = reqRes
				reqRes = 0
			}
		}

		if reqRes == 0 {
			return resLayout
		}
	}

	return resLayout
}

func getTaskResRequest(task *api.TaskInfo) map[v1.ResourceName]float64 {
	resMap := make(map[v1.ResourceName]float64)
	if task.Resreq.MilliCPU > 0 {
		resMap[v1.ResourceCPU] = task.Resreq.MilliCPU
	}

	if task.Resreq.Memory > 0 {
		resMap[v1.ResourceMemory] = task.Resreq.Memory
	}

	for resName, quantity := range task.Resreq.ScalarResources {
		if quantity == 0 {
			continue
		}

		resMap[resName] = quantity
	}
	return resMap
}

func AllocateNumaResForTask(task *api.TaskInfo, hit TopologyHint, nodeNumaIdleMap ResNumaSetType) ResNumaSetType {
	taskResMap := getTaskResRequest(task)

	var numaSlice []int
	for key := range nodeNumaIdleMap[v1.ResourceCPU] {
		numaSlice = append(numaSlice, key)
	}

	otherNuma, err := bitmask.NewBitMask(numaSlice...)
	if err != nil {
		return nil
	}

	otherNuma.Remove(hit.NUMANodeAffinity.GetBits()...)
	assignedResMap := make(ResNumaSetType)
	for resName, resValue := range taskResMap {
		assignedResMap[resName] = allocateNumaRes(resValue, hit.NUMANodeAffinity, otherNuma, nodeNumaIdleMap[resName])
	}

	return assignedResMap
}

func getCrossNumaNum(resNumaSet ResNumaSetType) int64 {
	mask, _ := bitmask.NewBitMask()
	for _, resSet := range resNumaSet {
		for numaId := range resSet {
			mask.Add(numaId)
		}
	}

	return int64(mask.Count())
}

func GetNodeNumaNumForTask(nodeInfo []*api.NodeInfo, resAssignMap map[string]ResNumaSetType) map[string]int64 {
	nodeNumaNumMap := make(map[string]int64)
	workqueue.ParallelizeUntil(context.TODO(), 16, len(nodeInfo), func(index int) {
		node := nodeInfo[index]
		nodeNumaNumMap[node.Name] = getCrossNumaNum(resAssignMap[node.Name])
	})

	return nodeNumaNumMap
}

/*
func getPodResourceDecision(resNumaSet ResNumaSetType) *PodResourceDecision {
	decision := PodResourceDecision{
		NUMAResources: make(map[int]v1.ResourceList),
	}

	for resName := range resNumaSet {
		for numaId, quantity := range resNumaSet[resName] {
			if _, ok := decision.NUMAResources[numaId]; !ok {
				decision.NUMAResources[numaId] = make(v1.ResourceList)
			}

			decision.NUMAResources[numaId][resName] = api.ResFloat642Quantity(resName, quantity)
		}
	}

	return &decision
}
*/
