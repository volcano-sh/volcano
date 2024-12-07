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
	"strings"

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
	cpuSharesTotal, cpuLimitsTotal, memoryLimitsTotal := int64(0), int64(0), int64(0)
	// track if limits were applied for each resource.
	cpuLimitsDeclared := true
	memoryLimitsDeclared := true
	// TODO: support init containers.
	for _, c := range pod.Spec.Containers {
		id := findContainerIDByName(pod, c.Name)
		// set cpu share.
		cpuReq, ok := c.Resources.Requests[apis.ExtendResourceCPU]
		if ok && !cpuReq.IsZero() {
			cpuShares := int64(milliCPUToShares(cpuReq.Value()))
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, ContainerID: id, SubPath: cgroup.CPUShareFileName, Value: cpuShares})
			cpuSharesTotal += cpuShares
		}

		// set cpu quota.
		cpuLimits, ok := c.Resources.Limits[apis.ExtendResourceCPU]
		if ok && !cpuLimits.IsZero() {
			cpuQuota := milliCPUToQuota(cpuLimits.Value(), quotaPeriod)
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, ContainerID: id, SubPath: cgroup.CPUQuotaTotalFile, Value: cpuQuota})
			cpuLimitsTotal += cpuQuota
		} else {
			cpuLimitsDeclared = false
		}

		// set memory limit.
		memoryLimit, ok := c.Resources.Limits[apis.ExtendResourceMemory]
		if ok && !memoryLimit.IsZero() {
			containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupMemorySubsystem, ContainerID: id, SubPath: cgroup.MemoryLimitFile, Value: memoryLimit.Value()})
			memoryLimitsTotal += memoryLimit.Value()
		} else {
			memoryLimitsDeclared = false
		}
	}

	// set pod level cgroup, container id="".
	if cpuSharesTotal == 0 {
		cpuSharesTotal = minShares
	}

	// pod didn't request any extend resources, skip setting cgroup.
	if cpuLimitsTotal == 0 && !cpuLimitsDeclared && !memoryLimitsDeclared {
		return containerRes
	}

	containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, SubPath: cgroup.CPUShareFileName, Value: cpuSharesTotal})

	// pod level should not set limit when exits one container has no cpu limit.
	if cpuLimitsDeclared {
		containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupCpuSubsystem, SubPath: cgroup.CPUQuotaTotalFile, Value: cpuLimitsTotal})
	}
	// pod level should not set limit when exits one container has no memory limit.
	if memoryLimitsDeclared {
		containerRes = append(containerRes, Resources{CgroupSubSystem: cgroup.CgroupMemorySubsystem, SubPath: cgroup.MemoryLimitFile, Value: memoryLimitsTotal})
	}
	return containerRes
}

func findContainerIDByName(pod *v1.Pod, name string) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == name {
			parts := strings.Split(status.ContainerID, "://")
			if len(parts) != 2 {
				klog.ErrorS(nil, "Failed to get container id", "name", name)
				return ""
			}
			return parts[1]
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
