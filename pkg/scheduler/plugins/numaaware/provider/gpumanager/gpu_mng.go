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

	maxNUMANodes = 16
)

type gpuMng struct {
}

// NewProvider return a new provider
func NewProvider() policy.HintProvider {
	return &gpuMng{}
}

// Name return the gpu manager name
func (mng *gpuMng) Name() string {
	return "gpuMng"
}

// requestedGPUs return the integer num of request gpu
func requestedGPUs(container *v1.Container) int {
	gpuQuantity, ok := container.Resources.Requests[NvidiaGPUResource]
	if !ok {
		return 0
	}
	return int(gpuQuantity.Value())
}

// generateGPUTopologyHints return the numa topology hints based on
// available GPUs and their NUMA node affinity.
func generateGPUTopologyHints(availableGPUs cpuset.CPUSet, gpuDetail api.GPUDetails, request int) []policy.TopologyHint {
	numaNodes := gpuDetail.NUMANodes().List()
	if len(numaNodes) > maxNUMANodes {
		klog.Warningf("[gpumanager] GPU NUMA node count %d exceeds max %d, skipping hint generation", len(numaNodes), maxNUMANodes)
		return nil
	}

	availablePerNuma := make(map[int]int)
	for _, gpuIdx := range availableGPUs.List() {
		if gpuInfo, ok := gpuDetail[gpuIdx]; ok {
			availablePerNuma[gpuInfo.NUMANodeID]++
		}
	}

	hints := []policy.TopologyHint{}
	minAffinitySize := len(numaNodes) + 1

	bitmask.IterateBitMasks(numaNodes, func(mask bitmask.BitMask) {
		numMatching := 0
		for _, numaID := range mask.GetBits() {
			numMatching += availablePerNuma[numaID]
		}

		// If they don't, then move onto the next combination.
		if numMatching < request {
			return
		}

		// Update minAffinitySize for the current request size.
		if mask.Count() < minAffinitySize {
			minAffinitySize = mask.Count()
		}

		hints = append(hints, policy.TopologyHint{
			NUMANodeAffinity: mask,
			Preferred:        false,
		})
	})

	// Loop back through all hints and update the 'Preferred' field based on
	// the minAffinitySize.
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

	if len(topoInfo.GPUDetail) == 0 {
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
		alignedGPUs := cpuset.New()
		for _, numaNodeID := range bestHit.NUMANodeAffinity.GetBits() {
			alignedGPUs = alignedGPUs.Union(availableGPUSet.Intersection(
				topoInfo.GPUDetail.GPUsInNUMANodes(numaNodeID)))
		}

		numAlignedToAlloc := alignedGPUs.Size()
		if requestNum < numAlignedToAlloc {
			numAlignedToAlloc = requestNum
		}

		allocated := takeGPUs(alignedGPUs, numAlignedToAlloc)
		result = result.Union(allocated)
	}

	// Get any remaining GPUs from what's leftover after attempting to grab aligned ones.
	remaining := requestNum - result.Size()
	if remaining > 0 {
		leftover := availableGPUSet.Difference(result)
		result = result.Union(takeGPUs(leftover, remaining))
	}

	return map[string]cpuset.CPUSet{
		string(NvidiaGPUResource): result,
	}
}

// takeGPUs return first count gpus from the set
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
