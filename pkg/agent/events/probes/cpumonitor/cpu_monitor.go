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

package cpumonitor

import (
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/probes"
	"volcano.sh/volcano/pkg/agent/utils"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	probes.RegisterEventProbeFunc(string(framework.NodeCPUThrottleEventName), NewCPUQuotaMonitor)
}

type cpuQuotaMonitor struct {
	sync.Mutex
	*config.Configuration
	cfgLock                sync.RWMutex
	queue                  workqueue.RateLimitingInterface
	getNodeFunc            utilnode.ActiveNode
	getPodsFunc            utilpod.ActivePods
	cpuThrottlingThreshold int
	lastCPUQuotaMilli      int64
	cpuJitterLimitPercent  int
}

func NewCPUQuotaMonitor(cfg *config.Configuration, _ *metriccollect.MetricCollectorManager, workQueue workqueue.RateLimitingInterface) framework.Probe {
	return &cpuQuotaMonitor{
		Configuration:          cfg,
		queue:                  workQueue,
		getNodeFunc:            cfg.GetNode,
		getPodsFunc:            cfg.GetActivePods,
		lastCPUQuotaMilli:      -1,
		cpuJitterLimitPercent:  1,
		cpuThrottlingThreshold: 80,
	}
}

func (m *cpuQuotaMonitor) ProbeName() string {
	return "NodeCPUThrottleProbe"
}

func (m *cpuQuotaMonitor) Run(stop <-chan struct{}) {
	klog.InfoS("Started nodeCPUThrottle probe")
	go wait.Until(m.detectCPUQuota, 10*time.Second, stop)
}

func (m *cpuQuotaMonitor) RefreshCfg(cfg *api.ColocationConfig) error {
	m.cfgLock.Lock()
	defer m.cfgLock.Unlock()
	if cfg.CPUThrottlingConfig != nil && cfg.CPUThrottlingConfig.Enable != nil && *cfg.CPUThrottlingConfig.Enable {
		m.cpuThrottlingThreshold, m.cpuJitterLimitPercent = utils.SetCPUThrottlingConfig(cfg)
	} else {
		m.cpuThrottlingThreshold = 0
	}

	return nil
}

func (m *cpuQuotaMonitor) detectCPUQuota() {
	m.cfgLock.RLock()
	throttlingThreshold := m.cpuThrottlingThreshold
	m.cfgLock.RUnlock()

	// If CPUThrottle is disabled, throttlingThreshold will be set to 0.
	if throttlingThreshold == 0 {
		return
	}

	node, err := m.getNodeFunc()
	if err != nil {
		klog.ErrorS(err, "CPU Quota Monitor: Failed to get node")
		return
	}
	nodeCopy := node.DeepCopy()

	totalCPU := nodeCopy.Status.Allocatable[v1.ResourceCPU]
	totalMilli := totalCPU.MilliValue()

	pods, err := m.getPodsFunc()
	if err != nil {
		klog.ErrorS(err, "CPU Quota Monitor: Failed to get pods")
	}

	//TODO: GetQosLevel only retrieves the QoS level annotated by Volcano. We should also consider the regular pod.
	var onlineRequestMilli int64
	for _, pod := range pods {
		if extension.GetQosLevel(pod) < 0 {
			continue
		}
		onlineRequestMilli += getPodCPURequestMilli(pod)
	}

	// availableBEMilli represents the CPU quota available to the Best Effort pod defined by Volcano.
	// The calculation is the allocatable Quota of current node minus the CPU requests of the non-BE pods.
	allowedMilli := totalMilli * int64(throttlingThreshold) / 100
	availableBEMilli := allowedMilli - onlineRequestMilli
	if availableBEMilli < 0 {
		availableBEMilli = 0
	}

	lastQuota := m.lastCPUQuotaMilli
	if lastQuota < 0 {
		m.sendNodeCPUThrottleEvent(availableBEMilli)
	} else if lastQuota == 0 {
		if availableBEMilli != 0 {
			m.sendNodeCPUThrottleEvent(availableBEMilli)
		}
	} else {
		diff := availableBEMilli - lastQuota
		if diff < 0 {
			diff = -diff
		}
		if diff >= lastQuota*int64(m.cpuJitterLimitPercent)/100 {
			m.sendNodeCPUThrottleEvent(availableBEMilli)
		}
	}
}

func getPodCPURequestMilli(pod *v1.Pod) int64 {
	var total int64
	for _, container := range pod.Spec.Containers {
		if qty, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
			total += qty.MilliValue()
		}
	}
	return total
}

func (m *cpuQuotaMonitor) sendNodeCPUThrottleEvent(quota int64) {
	event := framework.NodeCPUThrottleEvent{
		TimeStamp:     time.Now(),
		Resource:      v1.ResourceCPU,
		CPUQuotaMilli: quota,
	}
	m.queue.Add(event)
	m.lastCPUQuotaMilli = quota
}
