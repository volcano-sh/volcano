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

package cpumanager

import (
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/policy"
)

type cpuMng struct {
}

// NewProvider return a new provider
func NewProvider() policy.HintProvider {
	return &cpuMng{}
}

// Name return the cpu manager name
func (mng *cpuMng) Name() string {
	return "cpuMng"
}

// guaranteedCPUs return the intger num of request cpu
func guaranteedCPUs(container *v1.Container) int {
	cpuQuantity := container.Resources.Requests[v1.ResourceCPU]
	if cpuQuantity.Value()*1000 != cpuQuantity.MilliValue() {
		return 0
	}

	return int(cpuQuantity.Value())
}

// generateCPUTopologyHints return the numa topology hints based on
// - availableCPUs
func generateCPUTopologyHints(availableCPUs cpuset.CPUSet, CPUDetails topology.CPUDetails, request int) []policy.TopologyHint {
	minAffinitySize := CPUDetails.NUMANodes().Size()
	hints := []policy.TopologyHint{}
	bitmask.IterateBitMasks(CPUDetails.NUMANodes().ToSlice(), func(mask bitmask.BitMask) {
		// First, update minAffinitySize for the current request size.
		cpusInMask := CPUDetails.CPUsInNUMANodes(mask.GetBits()...).Size()
		if cpusInMask >= request && mask.Count() < minAffinitySize {
			minAffinitySize = mask.Count()
		}

		// Then check to see if we have enough CPUs available on the current
		// numa node bitmask to satisfy the CPU request.
		numMatching := 0
		// Finally, check to see if enough available CPUs remain on the current
		// NUMA node combination to satisfy the CPU request.
		for _, c := range availableCPUs.ToSlice() {
			if mask.IsSet(CPUDetails[c].NUMANodeID) {
				numMatching++
			}
		}

		// If they don't, then move onto the next combination.
		if numMatching < request {
			return
		}

		// Otherwise, create a new hint from the numa node bitmask and add it to the
		// list of hints.  We set all hint preferences to 'false' on the first
		// pass through.
		hints = append(hints, policy.TopologyHint{
			NUMANodeAffinity: mask,
			Preferred:        false,
		})
	})

	// Loop back through all hints and update the 'Preferred' field based on
	// counting the number of bits sets in the affinity mask and comparing it
	// to the minAffinitySize. Only those with an equal number of bits set (and
	// with a minimal set of numa nodes) will be considered preferred.
	for i := range hints {
		if hints[i].NUMANodeAffinity.Count() == minAffinitySize {
			hints[i].Preferred = true
		}
	}

	return hints
}

// getPhysicalCoresNum return the number of physical cores.
// The resourc-exporter reports core ids only unique in each socket,
// we use the platform unique form to get all physical cores.
func getPhysicalCoresNum(CPUDetails topology.CPUDetails) int {
	uniques := make(map[string]struct{})
	for _, v := range CPUDetails {
		key := fmt.Sprintf("%d/%d", v.SocketID, v.CoreID)
		uniques[key] = struct{}{}
	}
	return len(uniques)
}

func (mng *cpuMng) GetTopologyHints(container *v1.Container,
	topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets) map[string][]policy.TopologyHint {
	if _, ok := container.Resources.Requests[v1.ResourceCPU]; !ok {
		klog.Warningf("container %s has no cpu request", container.Name)
		return nil
	}

	requestNum := guaranteedCPUs(container)
	if requestNum == 0 {
		klog.Warningf(" the cpu request isn't  integer in container %s", container.Name)
		return nil
	}

	cputopo := &topology.CPUTopology{
		NumCPUs:    topoInfo.CPUDetail.CPUs().Size(),
		NumCores:   getPhysicalCoresNum(topoInfo.CPUDetail),
		NumSockets: topoInfo.CPUDetail.Sockets().Size(),
		CPUDetails: topoInfo.CPUDetail,
	}

	reserved := cpuset.NewCPUSet()
	reservedCPUs, ok := topoInfo.ResReserved[v1.ResourceCPU]
	if ok {
		// Take the ceiling of the reservation, since fractional CPUs cannot be
		// exclusively allocated.
		reservedCPUsFloat := float64(reservedCPUs.MilliValue()) / 1000
		numReservedCPUs := int(math.Ceil(reservedCPUsFloat))
		reserved, _ = takeByTopology(cputopo, cputopo.CPUDetails.CPUs(), numReservedCPUs)
		klog.V(4).Infof("[cpumanager] reserve cpuset :%v", reserved)
	}

	availableCPUSet, ok := resNumaSets[string(v1.ResourceCPU)]
	if !ok {
		klog.Warningf("no cpu resource")
		return nil
	}

	availableCPUSet = availableCPUSet.Difference(reserved)
	klog.V(4).Infof("requested: %d, availableCPUSet: %v", requestNum, availableCPUSet)
	return map[string][]policy.TopologyHint{
		string(v1.ResourceCPU): generateCPUTopologyHints(availableCPUSet, topoInfo.CPUDetail, requestNum),
	}
}

func (mng *cpuMng) Allocate(container *v1.Container, bestHit *policy.TopologyHint,
	topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets) map[string]cpuset.CPUSet {
	cputopo := &topology.CPUTopology{
		NumCPUs:    topoInfo.CPUDetail.CPUs().Size(),
		NumCores:   getPhysicalCoresNum(topoInfo.CPUDetail),
		NumSockets: topoInfo.CPUDetail.Sockets().Size(),
		CPUDetails: topoInfo.CPUDetail,
	}

	reserved := cpuset.NewCPUSet()
	reservedCPUs, ok := topoInfo.ResReserved[v1.ResourceCPU]
	if ok {
		// Take the ceiling of the reservation, since fractional CPUs cannot be
		// exclusively allocated.
		reservedCPUsFloat := float64(reservedCPUs.MilliValue()) / 1000
		numReservedCPUs := int(math.Ceil(reservedCPUsFloat))
		reserved, _ = takeByTopology(cputopo, cputopo.CPUDetails.CPUs(), numReservedCPUs)
		klog.V(3).Infof("[cpumanager] reserve cpuset :%v", reserved)
	}

	requestNum := guaranteedCPUs(container)
	availableCPUSet := resNumaSets[string(v1.ResourceCPU)]
	availableCPUSet = availableCPUSet.Difference(reserved)

	klog.V(4).Infof("alignedCPUs: %v requestNum: %v bestHit %v", availableCPUSet, requestNum, bestHit)

	result := cpuset.NewCPUSet()
	if bestHit.NUMANodeAffinity != nil {
		alignedCPUs := cpuset.NewCPUSet()
		for _, numaNodeID := range bestHit.NUMANodeAffinity.GetBits() {
			alignedCPUs = alignedCPUs.Union(availableCPUSet.Intersection(cputopo.CPUDetails.CPUsInNUMANodes(numaNodeID)))
		}

		numAlignedToAlloc := alignedCPUs.Size()
		if requestNum < numAlignedToAlloc {
			numAlignedToAlloc = requestNum
		}

		alignedCPUs, err := takeByTopology(cputopo, alignedCPUs, numAlignedToAlloc)
		if err != nil {
			return map[string]cpuset.CPUSet{
				string(v1.ResourceCPU): cpuset.NewCPUSet(),
			}
		}

		result = result.Union(alignedCPUs)
	}

	// Get any remaining CPUs from what's leftover after attempting to grab aligned ones.
	remainingCPUs, err := takeByTopology(cputopo, availableCPUSet.Difference(result), requestNum-result.Size())
	if err != nil {
		return map[string]cpuset.CPUSet{
			string(v1.ResourceCPU): cpuset.NewCPUSet(),
		}
	}

	result = result.Union(remainingCPUs)

	return map[string]cpuset.CPUSet{
		string(v1.ResourceCPU): result,
	}
}
