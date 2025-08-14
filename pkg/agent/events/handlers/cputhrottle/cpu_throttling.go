package cputhrottle

import (
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"os"
	"sync"
	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.NodeCPUThrottleEventName), NewCPUThrottleHandler)
}

type CPUThrottleHandler struct {
	*base.BaseHandle
	cgroupMgr   cgroup.CgroupManager
	getPodsFunc utilpod.ActivePods

	// Record Pod throttled status
	mutex            sync.RWMutex
	throttlingActive map[string]bool

	throttleStepPercent int
	minCPUQuotaPercent  int
}

func NewCPUThrottleHandler(config *config.Configuration, mgr *metriccollect.MetricCollectorManager,
	cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &CPUThrottleHandler{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.CPUThrottleFeature),
			Config: config,
		},
		cgroupMgr:   cgroupMgr,
		getPodsFunc: config.GetActivePods,

		throttlingActive: make(map[string]bool),

		throttleStepPercent: ThrottleStepPercent,
		minCPUQuotaPercent:  MinCPUQuotaPercent,
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

	pods, err := h.getPodsFunc()

	if err != nil {
		return fmt.Errorf("failed to get active pods: %v", err)
	}

	klog.InfoS("Handling CPU throttling event",
		"action", cpuEvent.Action,
		"usage", cpuEvent.Usage,
		"podCount", len(pods))

	switch cpuEvent.Action {
	case "start", "continue":
		return h.stepThrottleCPU(pods)
	case "stop":
		return h.stopCPUThrottle(pods)
	default:
		return fmt.Errorf("unknown cpu throttle action: %v", cpuEvent.Action)
	}
}

func (h *CPUThrottleHandler) stepThrottleCPU(pods []*v1.Pod) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, pod := range pods {
		qosLevel := extension.GetQosLevel(pod)
		if qosLevel >= 0 {
			continue
		}

		podUID := string(pod.UID)

		currentQuota, err := h.getCurrentCPUQuota(pod)
		if err != nil {
			klog.ErrorS(err, "Failed to get current CPU quota", "pod", pod.Name)
			continue
		}

		newQuota := h.calculateSteppedQuota(pod, currentQuota)

		if newQuota == currentQuota {
			continue
		}

		// Apply the calculated cpu quota for this pod
		if filePath, err := h.applyCPUQuota(pod, newQuota); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup file not existed", "cgroupFile", filePath)
			}
			klog.ErrorS(err, "Failed to apply CPU quota", "pod", pod.Name, "quota", newQuota)
			continue
		}

		h.throttlingActive[podUID] = true

		klog.InfoS("Applied stepped CPU throttling",
			"pod", pod.Name,
			"currentQuota", currentQuota,
			"newQuota", newQuota)
	}

	return nil
}

func (h *CPUThrottleHandler) stopCPUThrottle(pods []*v1.Pod) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, pod := range pods {
		qosLevel := extension.GetQosLevel(pod)
		if qosLevel >= 0 {
			continue
		}

		podUID := string(pod.UID)

		originalQuota := h.getDefaultCPUQuota(pod)

		if filePath, err := h.applyCPUQuota(pod, originalQuota); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup file not existed", "cgroupFile", filePath)
			}
			klog.ErrorS(err, "Failed to recover CPU quota", "pod", pod.Name, "quota", originalQuota)
			continue
		}

		delete(h.throttlingActive, podUID)

		klog.InfoS("Recovered CPU throttling",
			"pod", pod.Name,
			"originalQuota", originalQuota)
	}

	return nil
}

func (h *CPUThrottleHandler) RefreshCfg(cfg *api.ColocationConfig) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	isActive, err := features.DefaultFeatureGate.Enabled(features.CPUThrottleFeature, cfg)
	if err != nil {
		return err
	}

	if !isActive {
		klog.InfoS("CPU throttle feature disabled, recovering all throttled pods")
		return h.recoverAllThrottledPods()
	}

	if cfg.CPUThrottlingConfig != nil {
		if cfg.CPUThrottlingConfig.CPUThrottlingStepPercent != nil &&
			*cfg.CPUThrottlingConfig.CPUThrottlingStepPercent > 0 &&
			*cfg.CPUThrottlingConfig.CPUThrottlingStepPercent <= 100 {
			oldStep := h.throttleStepPercent
			h.throttleStepPercent = *cfg.CPUThrottlingConfig.CPUThrottlingStepPercent
			klog.InfoS("Updated throttle step percentage",
				"oldStep", oldStep,
				"newValue", h.throttleStepPercent)
		}

		if cfg.CPUThrottlingConfig.CPUMinQuotaPercent != nil &&
			*cfg.CPUThrottlingConfig.CPUMinQuotaPercent > 0 &&
			*cfg.CPUThrottlingConfig.CPUMinQuotaPercent <= 100 {
			oldQuota := h.minCPUQuotaPercent
			h.minCPUQuotaPercent = *cfg.CPUThrottlingConfig.CPUMinQuotaPercent
			klog.InfoS("Updated minCPU quota percentage",
				"oldQuota", oldQuota,
				"newValue", h.minCPUQuotaPercent)
		}
	}
	return nil
}

func (h *CPUThrottleHandler) recoverAllThrottledPods() error {
	if len(h.throttlingActive) == 0 {
		return nil
	}

	pods, err := h.getPodsFunc()
	if err != nil {
		return fmt.Errorf("failed to get active pods for recovery: %v", err)
	}

	var recoveredCount int
	for _, pod := range pods {
		podUID := string(pod.UID)
		if h.throttlingActive[podUID] {
			originalQuota := h.getDefaultCPUQuota(pod)
			if filePath, err := h.applyCPUQuota(pod, originalQuota); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					klog.InfoS("Cgroup file not existed", "cgroupFile", filePath)
				}
				klog.ErrorS(err, "Failed to recover CPU quota", "pod", pod.Name, "quota", originalQuota)
				continue
			}
			delete(h.throttlingActive, podUID)
			recoveredCount++
			klog.InfoS("Recovered CPU throttling due to feature disable",
				"pod", pod.Name,
				"restoredQuota", originalQuota)
		}
	}

	klog.InfoS("Recovered all throttled pods due to feature disable", "count", recoveredCount)
	return nil
}
