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

package gpumanager

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"
	"k8s.io/utils/cpuset"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/policy"
)

const (
	// NvidiaGPUResource is the extended resource name for NVIDIA GPUs.
	NvidiaGPUResource v1.ResourceName = "nvidia.com/gpu"
)

type gpuMng struct {
}

// NewProvider return a new GPU topology hint provider
func NewProvider() policy.HintProvider {
	return &gpuMng{}
}

// Name return the gpu manager name
func (mng *gpuMng) Name() string {
	return "gpuMng"
}

// requestedGPUs returns the integer number of GPUs requested by a container.
func requestedGPUs(container *v1.Container) int {
	gpuQuantity, ok := container.Resources.Requests[NvidiaGPUResource]
	if !ok {
		return 0
	}
	return int(gpuQuantity.Value())
}

// generateGPUTopologyHints returns topology hints for the requested GPU count
// based on the available GPUs and their NUMA node affinity.
func generateGPUTopologyHints(availableGPUs cpuset.CPUSet, gpuDetail api.GPUDetails, request int) []policy.TopologyHint {
	minAffinitySize := gpuDetail.NUMANodes().Size()
	hints := []policy.TopologyHint{}

	bitmask.IterateBitMasks(gpuDetail.NUMANodes().List(), func(mask bitmask.BitMask) {
		// Count the total GPUs in the NUMA nodes covered by this mask.
		gpusInMask := gpuDetail.GPUsInNUMANodes(mask.GetBits()...).Size()
		if gpusInMask >= request && mask.Count() < minAffinitySize {
			minAffinitySize = mask.Count()
		}

		// Count how many available GPUs fall within this NUMA node combination.
		numMatching := 0
		for _, gpuIdx := range availableGPUs.List() {
			if gpuInfo, ok := gpuDetail[gpuIdx]; ok {
				if mask.IsSet(gpuInfo.NUMANodeID) {
					numMatching++
				}
			}
		}

		// If not enough GPUs are available in this NUMA combination, skip it.
		if numMatching < request {
			return
		}

		hints = append(hints, policy.TopologyHint{
			NUMANodeAffinity: mask,
			Preferred:        false,
		})
	})

	// Mark hints with the minimal number of NUMA nodes as preferred.
	for i := range hints {
		if hints[i].NUMANodeAffinity.Count() == minAffinitySize {
			hints[i].Preferred = true
		}
	}

	return hints
}

func (mng *gpuMng) GetTopologyHints(container *v1.Container,
	topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets) map[string][]policy.TopologyHint {
	if _, ok := container.Resources.Requests[NvidiaGPUResource]; !ok {
		return nil
	}

	requestNum := requestedGPUs(container)
	if requestNum == 0 {
		klog.Warningf("container %s requests 0 GPUs", container.Name)
		return nil
	}

	if topoInfo.GPUDetail == nil || len(topoInfo.GPUDetail) == 0 {
		klog.V(4).Infof("[gpumanager] no GPU topology info available on node %s/%s", topoInfo.Namespace, topoInfo.Name)
		return nil
	}

	availableGPUSet, ok := resNumaSets[string(NvidiaGPUResource)]
	if !ok {
		klog.Warningf("no GPU resource in resNumaSets")
		return nil
	}

	klog.V(4).Infof("[gpumanager] requested: %d, availableGPUs: %v", requestNum, availableGPUSet)
	return map[string][]policy.TopologyHint{
		string(NvidiaGPUResource): generateGPUTopologyHints(availableGPUSet, topoInfo.GPUDetail, requestNum),
	}
}

func (mng *gpuMng) Allocate(container *v1.Container, bestHit *policy.TopologyHint,
	topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets) map[string]cpuset.CPUSet {
	requestNum := requestedGPUs(container)
	if requestNum == 0 {
		return nil
	}

	availableGPUSet, ok := resNumaSets[string(NvidiaGPUResource)]
	if !ok {
		return map[string]cpuset.CPUSet{
			string(NvidiaGPUResource): cpuset.New(),
		}
	}

	klog.V(4).Infof("[gpumanager] availableGPUs: %v requestNum: %v bestHit: %v", availableGPUSet, requestNum, bestHit)

	result := cpuset.New()
	if bestHit.NUMANodeAffinity != nil {
		// First, try to allocate GPUs from the preferred NUMA nodes.
		alignedGPUs := cpuset.New()
		for _, numaNodeID := range bestHit.NUMANodeAffinity.GetBits() {
			alignedGPUs = alignedGPUs.Union(availableGPUSet.Intersection(
				topoInfo.GPUDetail.GPUsInNUMANodes(numaNodeID)))
		}

		numAlignedToAlloc := alignedGPUs.Size()
		if requestNum < numAlignedToAlloc {
			numAlignedToAlloc = requestNum
		}

		// Take the first N aligned GPUs.
		allocated := takeGPUs(alignedGPUs, numAlignedToAlloc)
		result = result.Union(allocated)
	}

	// If we still need more GPUs, take from remaining available.
	remaining := requestNum - result.Size()
	if remaining > 0 {
		leftover := availableGPUSet.Difference(result)
		result = result.Union(takeGPUs(leftover, remaining))
	}

	return map[string]cpuset.CPUSet{
		string(NvidiaGPUResource): result,
	}
}

// takeGPUs selects the first 'count' GPUs from the given set.
func takeGPUs(gpuSet cpuset.CPUSet, count int) cpuset.CPUSet {
	result := cpuset.New()
	for _, gpuIdx := range gpuSet.List() {
		if result.Size() >= count {
			break
		}
		result = result.Union(cpuset.New(gpuIdx))
	}
	return result
}
