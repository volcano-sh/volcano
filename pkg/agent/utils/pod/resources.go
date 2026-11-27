/*
Copyright 2024 The Volcano Authors.

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

package pod

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

const (
	// These limits are defined in the kernel:
	// https://github.com/torvalds/linux/blob/0bddd227f3dc55975e2b8dfa7fc6f959b062a2c7/kernel/sched/sched.h#L427-L428
	minShares = 2
	maxShares = 262144

	sharesPerCPU  = 1024
	milliCPUToCPU = 1000

	// 100000 is equivalent to 100ms
	quotaPeriod    = 100000
	minQuotaPeriod = 1000
)

// Resources represents container level resources.
type Resources struct {
	CgroupSubSystem cgroup.CgroupSubsystem
	ContainerID     string
	SubPath         string
	Value           int64
}

// CalculateExtendResources calculates pod and container that use extend resource level cgroup resource, include cpu and memory
func CalculateExtendResources(pod *v1.Pod) []Resources {
	containerRes := []Resources{}
	appCpuSharesTotal, appCpuLimitsTotal, appMemoryLimitsTotal := int64(0), int64(0), int64(0)
	initCpuSharesMax, initCpuLimitsMax, initMemoryLimitsMax := int64(0), int64(0), int64(0)
	// track if limits were applied for each resource.
	cpuLimitsDeclared := true
	memoryLimitsDeclared := true

	// process init containers
	for _, ic := range pod.Spec.InitContainers {
		id := FindContainerIDByName(pod, ic.Name)
		// set cpu share.
		cpuReq, ok := ic.Resources.Requests[apis.GetExtendResourceCPU()]
		if ok && !cpuReq.IsZero() {
			cpuShares := int64(milliCPUToShares(cpuReq.Value()))
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, ContainerID: id, SubPath: cgroup.CPUShareFileName, Value: cpuShares})
			if cpuShares > initCpuSharesMax {
				initCpuSharesMax = cpuShares
			}
		}

		// set cpu quota.
		cpuLimits, ok := ic.Resources.Limits[apis.GetExtendResourceCPU()]
		if ok && !cpuLimits.IsZero() {
			cpuQuota := milliCPUToQuota(cpuLimits.Value(), quotaPeriod)
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, ContainerID: id, SubPath: cgroup.CPUQuotaTotalFile, Value: cpuQuota})
			if cpuQuota > initCpuLimitsMax {
				initCpuLimitsMax = cpuQuota
			}
		} else {
			cpuLimitsDeclared = false
		}

		// set memory limit.
		memoryLimit, ok := ic.Resources.Limits[apis.GetExtendResourceMemory()]
		if ok && !memoryLimit.IsZero() {
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupMemorySubsystem, ContainerID: id, SubPath: cgroup.MemoryLimitFile, Value: memoryLimit.Value()})
			if memoryLimit.Value() > initMemoryLimitsMax {
				initMemoryLimitsMax = memoryLimit.Value()
			}
		} else {
			memoryLimitsDeclared = false
		}
	}

	// process app containers
	for _, c := range pod.Spec.Containers {
		id := FindContainerIDByName(pod, c.Name)
		// set cpu share.
		cpuReq, ok := c.Resources.Requests[apis.GetExtendResourceCPU()]
		if ok && !cpuReq.IsZero() {
			cpuShares := int64(milliCPUToShares(cpuReq.Value()))
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, ContainerID: id, SubPath: cgroup.CPUShareFileName, Value: cpuShares})
			appCpuSharesTotal += cpuShares
		}

		// set cpu quota.
		cpuLimits, ok := c.Resources.Limits[apis.GetExtendResourceCPU()]
		if ok && !cpuLimits.IsZero() {
			cpuQuota := milliCPUToQuota(cpuLimits.Value(), quotaPeriod)
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, ContainerID: id, SubPath: cgroup.CPUQuotaTotalFile, Value: cpuQuota})
			appCpuLimitsTotal += cpuQuota
		} else {
			cpuLimitsDeclared = false
		}

		// set memory limit.
		memoryLimit, ok := c.Resources.Limits[apis.GetExtendResourceMemory()]
		if ok && !memoryLimit.IsZero() {
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupMemorySubsystem, ContainerID: id, SubPath: cgroup.MemoryLimitFile, Value: memoryLimit.Value()})
			appMemoryLimitsTotal += memoryLimit.Value()
		} else {
			memoryLimitsDeclared = false
		}
	}

	// pod didn't request any extend resources, skip setting cgroup.
	if len(containerRes) == 0 {
		return containerRes
	}

	cpuSharesTotal := appCpuSharesTotal
	if initCpuSharesMax > cpuSharesTotal {
		cpuSharesTotal = initCpuSharesMax
	}
	if cpuSharesTotal == 0 {
		// Align with min cpu share value in kubelet.
		cpuSharesTotal = minShares
	}

	containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, SubPath: cgroup.CPUShareFileName, Value: cpuSharesTotal})

	// pod level should not set limit when exists one container has no cpu limit.
	if cpuLimitsDeclared {
		cpuLimitsTotal := appCpuLimitsTotal
		if initCpuLimitsMax > cpuLimitsTotal {
			cpuLimitsTotal = initCpuLimitsMax
		}
		containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, SubPath: cgroup.CPUQuotaTotalFile, Value: cpuLimitsTotal})
	}
	// pod level should not set limit when exists one container has no memory limit.
	if memoryLimitsDeclared {
		memoryLimitsTotal := appMemoryLimitsTotal
		if initMemoryLimitsMax > memoryLimitsTotal {
			memoryLimitsTotal = initMemoryLimitsMax
		}
		containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupMemorySubsystem, SubPath: cgroup.MemoryLimitFile, Value: memoryLimitsTotal})
	}
	return containerRes
}

func FindContainerIDByName(pod *v1.Pod, name string) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == name {
			// Return the full container ID with runtime prefix (e.g., "containerd://xxx")
			// This is needed for BuildContainerCgroupName to identify the runtime type
			return status.ContainerID
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == name {
			return status.ContainerID
		}
	}
	return ""
}

// milliCPUToQuota converts milliCPU to CFS quota and period values
func milliCPUToQuota(milliCPU int64, period int64) (quota int64) {
	// we then convert your milliCPU to a value normalized over a period
	quota = (milliCPU * period) / milliCPUToCPU

	// quota needs to be a minimum of 1ms.
	if quota < minQuotaPeriod {
		quota = minQuotaPeriod
	}

	return
}

// milliCPUToShares converts the milliCPU to CFS shares.
func milliCPUToShares(milliCPU int64) uint64 {
	if milliCPU == 0 {
		// Docker converts zero milliCPU to unset, which maps to kernel default
		// for unset: 1024. Return 2 here to really match kernel default for
		// zero milliCPU.
		return minShares
	}
	// Conceptually (milliCPU / milliCPUToCPU) * sharesPerCPU, but factored to improve rounding.
	shares := (milliCPU * sharesPerCPU) / milliCPUToCPU
	if shares < minShares {
		return minShares
	}
	if shares > maxShares {
		return maxShares
	}
	return uint64(shares)
}

func CalculateExtendResourcesV2(pod *v1.Pod) []Resources {
	containerRes := []Resources{}
	appCpuWeightTotal, appCpuMaxTotal, appMemoryMaxTotal := int64(0), int64(0), int64(0)
	initCpuWeightMax, initCpuMaxMax, initMemoryMaxMax := int64(0), int64(0), int64(0)
	cpuLimitsDeclared := true
	memoryLimitsDeclared := true

	// process init containers
	for _, ic := range pod.Spec.InitContainers {
		id := FindContainerIDByName(pod, ic.Name)
		cpuReq, ok := ic.Resources.Requests[apis.GetExtendResourceCPU()]
		if ok && !cpuReq.IsZero() {
			cpuWeight := int64(milliCPUToWeight(cpuReq.Value()))
			containerRes = append(containerRes, Resources{
				CgroupSubSystem: cgroup.CgroupCpuSubsystem,
				ContainerID:     id,
				SubPath:         cgroup.CPUWeightFileV2,
				Value:           cpuWeight,
			})
			if cpuWeight > initCpuWeightMax {
				initCpuWeightMax = cpuWeight
			}
		}

		cpuLimits, ok := ic.Resources.Limits[apis.GetExtendResourceCPU()]
		if ok {
			// In cgroup v2, both zero and non-zero limits are meaningful
			// Zero limit means unlimited, non-zero limit means specific quota
			cpuMaxStr := milliCPUToMax(cpuLimits.Value(), quotaPeriod)
			if cpuMaxStr == "max 100000" {
				containerRes = append(containerRes, Resources{
					CgroupSubSystem: cgroup.CgroupCpuSubsystem,
					ContainerID:     id,
					SubPath:         cgroup.CPUQuotaTotalFileV2,
					Value:           -1,
				})
				initCpuMaxMax = -1
			} else {
				// For cgroup v2, we need to write the full "quota period" string
				// We'll use a special Value and handle it in resources.go
				var cpuQuota, period int64
				n, err := fmt.Sscanf(cpuMaxStr, "%d %d", &cpuQuota, &period)
				if err != nil {
					klog.ErrorS(err, "Failed to parse CPU max string",
						"container", ic.Name,
						"cpuMaxStr", cpuMaxStr)
				} else if n != 2 {
					klog.ErrorS(nil, "Incomplete CPU max string parsing",
						"container", ic.Name,
						"cpuMaxStr", cpuMaxStr,
						"matchedCount", n)
				} else {
					// Validate and use the expected period
					if period != quotaPeriod {
						klog.ErrorS(nil, "Unexpected CPU period value, using default",
							"container", ic.Name,
							"expected", quotaPeriod,
							"actual", period)
						period = quotaPeriod
					}
					containerRes = append(containerRes, Resources{
						CgroupSubSystem: cgroup.CgroupCpuSubsystem,
						ContainerID:     id,
						SubPath:         cgroup.CPUQuotaTotalFileV2,
						Value:           cpuQuota,
					})
					if initCpuMaxMax != -1 && cpuQuota > initCpuMaxMax {
						initCpuMaxMax = cpuQuota
					}
				}
			}
		} else {
			cpuLimitsDeclared = false
		}

		memoryLimits, ok := ic.Resources.Limits[apis.GetExtendResourceMemory()]
		if ok {
			// In cgroup v2, both zero and non-zero limits are meaningful
			// Zero limit means unlimited, non-zero limit means specific limit
			memoryMaxStr := memoryLimitToMax(memoryLimits.Value())
			if memoryMaxStr == "max" {
				containerRes = append(containerRes, Resources{
					CgroupSubSystem: cgroup.CgroupMemorySubsystem,
					ContainerID:     id,
					SubPath:         cgroup.MemoryMaxFileV2,
					Value:           -1,
				})
				initMemoryMaxMax = -1
			} else {
				var memMax int64
				memMax, err := strconv.ParseInt(memoryMaxStr, 10, 64)
				if err == nil {
					containerRes = append(containerRes, Resources{
						CgroupSubSystem: cgroup.CgroupMemorySubsystem,
						ContainerID:     id,
						SubPath:         cgroup.MemoryMaxFileV2,
						Value:           memMax,
					})
					if initMemoryMaxMax != -1 && memMax > initMemoryMaxMax {
						initMemoryMaxMax = memMax
					}
				} else {
					klog.ErrorS(err, "Failed to parse memory limit",
						"container", ic.Name,
						"memoryMaxStr", memoryMaxStr)
				}
			}
		} else {
			memoryLimitsDeclared = false
		}
	}

	// process app containers
	for _, c := range pod.Spec.Containers {
		id := FindContainerIDByName(pod, c.Name)
		cpuReq, ok := c.Resources.Requests[apis.GetExtendResourceCPU()]
		if ok && !cpuReq.IsZero() {
			cpuWeight := int64(milliCPUToWeight(cpuReq.Value()))
			containerRes = append(containerRes, Resources{
				CgroupSubSystem: cgroup.CgroupCpuSubsystem,
				ContainerID:     id,
				SubPath:         cgroup.CPUWeightFileV2,
				Value:           cpuWeight,
			})
			appCpuWeightTotal += cpuWeight
		}

		cpuLimits, ok := c.Resources.Limits[apis.GetExtendResourceCPU()]
		if ok {
			// In cgroup v2, both zero and non-zero limits are meaningful
			// Zero limit means unlimited, non-zero limit means specific quota
			cpuMaxStr := milliCPUToMax(cpuLimits.Value(), quotaPeriod)
			if cpuMaxStr == "max 100000" {
				containerRes = append(containerRes, Resources{
					CgroupSubSystem: cgroup.CgroupCpuSubsystem,
					ContainerID:     id,
					SubPath:         cgroup.CPUQuotaTotalFileV2,
					Value:           -1,
				})
			} else {
				// For cgroup v2, we need to write the full "quota period" string
				// We'll use a special Value and handle it in resources.go
				var cpuQuota, period int64
				n, err := fmt.Sscanf(cpuMaxStr, "%d %d", &cpuQuota, &period)
				if err != nil {
					klog.ErrorS(err, "Failed to parse CPU max string",
						"container", c.Name,
						"cpuMaxStr", cpuMaxStr)
				} else if n != 2 {
					klog.ErrorS(nil, "Incomplete CPU max string parsing",
						"container", c.Name,
						"cpuMaxStr", cpuMaxStr,
						"matchedCount", n)
				} else {
					// Validate and use the expected period
					if period != quotaPeriod {
						klog.ErrorS(nil, "Unexpected CPU period value, using default",
							"container", c.Name,
							"expected", quotaPeriod,
							"actual", period)
						period = quotaPeriod
					}
					containerRes = append(containerRes, Resources{
						CgroupSubSystem: cgroup.CgroupCpuSubsystem,
						ContainerID:     id,
						SubPath:         cgroup.CPUQuotaTotalFileV2,
						Value:           cpuQuota,
					})
					appCpuMaxTotal += cpuQuota
				}
			}
		} else {
			cpuLimitsDeclared = false
		}

		memoryLimits, ok := c.Resources.Limits[apis.GetExtendResourceMemory()]
		if ok {
			// In cgroup v2, both zero and non-zero limits are meaningful
			// Zero limit means unlimited, non-zero limit means specific limit
			memoryMaxStr := memoryLimitToMax(memoryLimits.Value())
			if memoryMaxStr == "max" {
				containerRes = append(containerRes, Resources{
					CgroupSubSystem: cgroup.CgroupMemorySubsystem,
					ContainerID:     id,
					SubPath:         cgroup.MemoryMaxFileV2,
					Value:           -1,
				})
			} else {
				var memMax int64
				memMax, err := strconv.ParseInt(memoryMaxStr, 10, 64)
				if err == nil {
					containerRes = append(containerRes, Resources{
						CgroupSubSystem: cgroup.CgroupMemorySubsystem,
						ContainerID:     id,
						SubPath:         cgroup.MemoryMaxFileV2,
						Value:           memMax,
					})
					appMemoryMaxTotal += memMax
				} else {
					klog.ErrorS(err, "Failed to parse memory limit",
						"container", c.Name,
						"memoryMaxStr", memoryMaxStr)
				}
			}
		} else {
			memoryLimitsDeclared = false
		}
	}

	// pod didn't request any extend resources, skip setting cgroup.
	if len(containerRes) == 0 {
		return containerRes
	}

	cpuWeightTotal := appCpuWeightTotal
	if initCpuWeightMax > cpuWeightTotal {
		cpuWeightTotal = initCpuWeightMax
	}
	if cpuWeightTotal == 0 {
		// Align with min cpu share value in kubelet.
		cpuWeightTotal = 100
	}

	containerRes = append(containerRes, Resources{
		CgroupSubSystem: cgroup.CgroupCpuSubsystem,
		SubPath:         cgroup.CPUWeightFileV2,
		Value:           cpuWeightTotal,
	})

	if cpuLimitsDeclared {
		cpuMaxTotal := appCpuMaxTotal
		if initCpuMaxMax == -1 {
			cpuMaxTotal = 0
		} else if initCpuMaxMax > cpuMaxTotal {
			cpuMaxTotal = initCpuMaxMax
		}
		if cpuMaxTotal == 0 {
			containerRes = append(containerRes, Resources{
				CgroupSubSystem: cgroup.CgroupCpuSubsystem,
				SubPath:         cgroup.CPUQuotaTotalFileV2,
				Value:           -1,
			})
		} else {
			containerRes = append(containerRes, Resources{
				CgroupSubSystem: cgroup.CgroupCpuSubsystem,
				SubPath:         cgroup.CPUQuotaTotalFileV2,
				Value:           cpuMaxTotal,
			})
		}
	}

	if memoryLimitsDeclared {
		memoryMaxTotal := appMemoryMaxTotal
		if initMemoryMaxMax == -1 {
			memoryMaxTotal = 0
		} else if initMemoryMaxMax > memoryMaxTotal {
			memoryMaxTotal = initMemoryMaxMax
		}
		if memoryMaxTotal == 0 {
			containerRes = append(containerRes, Resources{
				CgroupSubSystem: cgroup.CgroupMemorySubsystem,
				SubPath:         cgroup.MemoryMaxFileV2,
				Value:           -1,
			})
		} else {
			containerRes = append(containerRes, Resources{
				CgroupSubSystem: cgroup.CgroupMemorySubsystem,
				SubPath:         cgroup.MemoryMaxFileV2,
				Value:           memoryMaxTotal,
			})
		}
	}

	return containerRes
}

func milliCPUToWeight(milliCPU int64) uint64 {
	weight := uint64(milliCPU / 10)
	if weight < 1 {
		weight = 1
	} else if weight > 10000 {
		weight = 10000
	}
	return weight
}

func milliCPUToMax(milliCPU, period int64) string {
	if milliCPU == 0 {
		return "max 100000"
	}
	quota := (milliCPU * period) / 1000
	if quota < 1000 {
		quota = 1000
	}
	return fmt.Sprintf("%d %d", quota, period)
}

func memoryLimitToMax(memoryBytes int64) string {
	if memoryBytes == 0 {
		return "max"
	}
	return fmt.Sprintf("%d", memoryBytes)
}
