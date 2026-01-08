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

package cputhrottle

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

const (
	unlimitedQuota = -1
	CPUPeriod      = 100000
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.NodeCPUThrottleEventName), NewCPUThrottleHandler)
}

type CPUThrottleHandler struct {
	*base.BaseHandle
	cgroupMgr cgroup.CgroupManager

	// Record Pod throttled status
	mutex sync.Mutex
}

func NewCPUThrottleHandler(config *config.Configuration, mgr *metriccollect.MetricCollectorManager,
	cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &CPUThrottleHandler{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.CPUThrottleFeature),
			Config: config,
		},
		cgroupMgr: cgroupMgr,
	}
}

func (h *CPUThrottleHandler) Handle(event interface{}) error {
	cpuEvent, ok := event.(framework.NodeCPUThrottleEvent)
	if !ok {
		return fmt.Errorf("invalid event type for CPU Throttle handler")
	}

	if cpuEvent.Resource != v1.ResourceCPU {
		return nil
	}

	quota := h.quotaFromMilliCPU(cpuEvent.CPUQuotaMilli)

	klog.InfoS("Handling CPU throttling event",
		"cpuQuotaMilli", cpuEvent.CPUQuotaMilli,
		"quota", quota)

	return h.applyBEQuota(quota)
}

func (h *CPUThrottleHandler) applyBEQuota(quota int64) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	filePath, err := h.writeBEQuota(quota)
	if err != nil {
		if os.IsNotExist(err) {
			klog.InfoS("Cgroup file not existed", "cgroupFile", filePath)
		}
		return fmt.Errorf("failed to apply BE root cpu quota: %w", err)
	}

	klog.InfoS("Applied BE root CPU quota", "quota", quota, "cgroupFile", filePath)
	return nil
}

func (h *CPUThrottleHandler) quotaFromMilliCPU(milliCPU int64) int64 {
	if milliCPU < 0 {
		return -1
	}

	return milliCPU * CPUPeriod / 1000
}

func (h *CPUThrottleHandler) writeBEQuota(quota int64) (string, error) {
	cgroupPath, err := h.cgroupMgr.GetQoSCgroupPath(v1.PodQOSBestEffort, cgroup.CgroupCpuSubsystem)
	if err != nil {
		return "", err
	}

	quotaFile := cgroup.CPUQuotaTotalFile
	if quota != unlimitedQuota && quota < cgroup.CPUQuotaMin {
		quota = cgroup.CPUQuotaMin
	}
	quotaValue := strconv.FormatInt(quota, 10)
	if h.cgroupMgr.GetCgroupVersion() == cgroup.CgroupV2 {
		quotaFile = cgroup.CPUQuotaTotalFileV2
		if quota == unlimitedQuota {
			quotaValue = "max"
		} else {
			quotaValue = fmt.Sprintf("%d %d", quota, CPUPeriod)
		}
	}

	filePath := path.Join(cgroupPath, quotaFile)
	if err := utils.UpdateFile(filePath, []byte(quotaValue)); err != nil {
		return filePath, err
	}

	return filePath, nil
}

func (h *CPUThrottleHandler) RefreshCfg(cfg *api.ColocationConfig) error {
	if err := h.BaseHandle.RefreshCfg(cfg); err != nil {
		return err
	}

	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.Active {
		klog.InfoS("CPU throttle feature disabled, recovering all throttled pods")
		filePath, err := h.writeBEQuota(unlimitedQuota)
		if err != nil {
			if os.IsNotExist(err) {
				klog.InfoS("Cgroup file not existed", "cgroupFile", filePath)
			}
			return fmt.Errorf("failed to apply BE root cpu quota: %w", err)
		}
		klog.InfoS("Recovered all throttled pods")
	}
	return nil
}
