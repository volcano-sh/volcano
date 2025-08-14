package cputhrottle

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"os"
	"path"
	"strconv"
	"strings"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

const (
	// CPUPeriod CPU schedule period
	CPUPeriod = 100000 // 100ms in microseconds

	// ThrottleStepPercent step set cpu throttle
	ThrottleStepPercent = 10
	MinCPUQuotaPercent  = 20

	// RecoverStepPercent pod recover step
	RecoverStepPercent = 15
)

// getCurrentCPUQuota retrieves the current CPU quota from cgroup filesystem.
// This reflects the actual runtime CPU quota that is currently enforced by kernel,
// which may differ from the original Pod specification if it has been modified by
// throttling mechanisms or other runtime adjustments.
func (h *CPUThrottleHandler) getCurrentCPUQuota(pod *v1.Pod) (int64, error) {
	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(pod.Status.QOSClass, cgroup.CgroupCpuSubsystem, pod.UID)
	if err != nil {
		return 0, fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	// Read from cpu.cfs_quota_us file which contains the CPU quota in microseconds.
	quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)

	data, err := os.ReadFile(quotaFile)
	if err != nil {
		// If cgroup file doesn't exist, fall back to the default quota calculated from Pod spec.
		if os.IsNotExist(err) {
			return h.getDefaultCPUQuota(pod), nil
		}
		return 0, fmt.Errorf("failed to read CPU quota file: %v", err)
	}

	quotaStr := strings.TrimSpace(string(data))
	// Value of "-1" in cpu.cfs_quota_us means no quota limit is set, allowing unlimited CPU
	// usage within the available cores.
	if quotaStr == "-1" {
		return h.getDefaultCPUQuota(pod), nil
	}

	quota, err := strconv.ParseInt(quotaStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU quota: %v", err)
	}

	return quota, nil
}

// getDefaultCPUQuota calculates the CPU quota based on the original Pod specification,
// This represents the intended CPU limit as defined in the Pod's resource requirements,
// before any runtime modifications like throttling.
func (h *CPUThrottleHandler) getDefaultCPUQuota(pod *v1.Pod) int64 {
	// Calculate default cpu quota based on containers' cpu limits.
	// CPU limits in Kubernetes are specified in CPU units(cores), where:
	// - 1 core = 1000 millicores = 1000m
	// - The quota is calculated as: (millicores * period) / 1000
	// For example: 2 cores = 2000m = (2000 * 100000) / 1000 = 200000 microseconds
	for _, container := range pod.Spec.Containers {
		if cpuLimit := container.Resources.Limits.Cpu(); cpuLimit != nil {
			cpuMillis := cpuLimit.MilliValue()
			quota := cpuMillis * CPUPeriod / 1000
			return quota
		}
	}

	// If cpu has no limit, return default as 1 CPU core
	return 1 * CPUPeriod
}

// calculateSteppedQuota computes the new CPU quota after applying throttling step.
// The throttling uses a gradual approach to avoid sudden performance drops that
// could destabilize running applications.
func (h *CPUThrottleHandler) calculateSteppedQuota(pod *v1.Pod, currentQuota int64) int64 {
	stepPercent := h.throttleStepPercent
	// Calculate the quota for the tiered limit
	stepReduction := currentQuota * int64(stepPercent) / 100
	newQuota := currentQuota - stepReduction

	// Calculate the minimum quota to prevent complete CPU starvation.
	minQuota := h.calculateMinCPUQuota(pod)

	if newQuota < minQuota {
		return minQuota
	}

	return newQuota
}

// calculateMinCPUQuota determines the minimum CPU quota that should be preserved
// This handler aims to throttle pods to a configurable percentage of their original limits
func (h *CPUThrottleHandler) calculateMinCPUQuota(pod *v1.Pod) int64 {
	// Always use percentage of the original CPU limit as the minimum quota
	// This ensures consistent throttling behavior regardless of requests
	// TODO: should respect resources.requests?
	originalQuota := h.getDefaultCPUQuota(pod)

	minPercent := int64(h.minCPUQuotaPercent)

	minQuota := originalQuota * minPercent / 100

	// Ensure we have an absolute minimum (at least 20% of one CPU core)
	absoluteMin := int64(CPUPeriod * 20 / 100)
	if minQuota < absoluteMin {
		minQuota = absoluteMin
	}

	return minQuota
}

// applyCPUQuota writes the new CPU quota to the cgroup filesystem to enforce
// the CPU limit at the kernel level. This is how Kubernetes actually implements
// CPU resources.
func (h *CPUThrottleHandler) applyCPUQuota(pod *v1.Pod, quota int64) (string, error) {
	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(pod.Status.QOSClass, cgroup.CgroupCpuSubsystem, pod.UID)
	if err != nil {
		return "", err
	}

	quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)
	quotaByte := []byte(fmt.Sprintf("%d", quota))

	err = utils.UpdatePodCgroup(quotaFile, quotaByte)
	if err != nil {
		return quotaFile, err
	}

	return "", nil
}
