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

package nodemonitor

import (
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/probes"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/oversubscription/queue"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/metriccollect/local"
	"volcano.sh/volcano/pkg/resourceusage"
)

func init() {
	probes.RegisterEventProbeFunc(string(framework.NodeMonitorEventName), NewMonitor)
	probes.RegisterEventProbeFunc(string(framework.NodeCPUThrottleEventName), NewMonitor)
}

const (
	highUsageCountLimit = 6
)

type monitor struct {
	sync.Mutex
	*config.Configuration
	policy.Interface
	cfgLock       sync.RWMutex
	queue         workqueue.RateLimitingInterface
	lowWatermark  apis.Watermark
	highWatermark apis.Watermark
	// highUsageCountByResName is used to record whether resources usage are high.
	highUsageCountByResName map[v1.ResourceName]int
	getNodeFunc             utilnode.ActiveNode
	getPodsFunc             utilpod.ActivePods
	usageGetter             resourceusage.Getter
	cpuThrottlingThreshold  int
	cpuProtectionWatermark  int
	cpuThrottlingActive     bool
}

func NewMonitor(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, workQueue workqueue.RateLimitingInterface) framework.Probe {
	evictor := eviction.NewEviction(config.GenericConfiguration.KubeClient, config.GenericConfiguration.KubeNodeName)
	return &monitor{
		Interface:               policy.GetPolicyFunc(config.GenericConfiguration.OverSubscriptionPolicy)(config, mgr, evictor, queue.NewSqQueue(), local.CollectorName),
		queue:                   workQueue,
		getNodeFunc:             config.GetNode,
		getPodsFunc:             config.GetActivePods,
		lowWatermark:            make(apis.Watermark),
		highWatermark:           make(apis.Watermark),
		highUsageCountByResName: make(map[v1.ResourceName]int),
		usageGetter:             resourceusage.NewUsageGetter(mgr, local.CollectorName),
		cpuThrottlingActive:     false,
	}
}

func (m *monitor) ProbeName() string {
	return "NodePressureProbe"
}

func (m *monitor) Run(stop <-chan struct{}) {
	klog.InfoS("Started nodePressure probe")
	go wait.Until(m.utilizationMonitoring, 10*time.Second, stop)
	go wait.Until(m.detect, 10*time.Second, stop)
}

func (m *monitor) RefreshCfg(cfg *api.ColocationConfig) error {
	m.cfgLock.Lock()
	utils.SetEvictionWatermark(cfg, m.lowWatermark, m.highWatermark)
	if cfg.CPUThrottlingConfig != nil && cfg.CPUThrottlingConfig.Enable != nil && *cfg.CPUThrottlingConfig.Enable {
		m.cpuThrottlingThreshold, m.cpuProtectionWatermark = utils.SetCPUThrottlingConfig(cfg)
	}
	m.cfgLock.Unlock()

	m.Lock()
	defer m.Unlock()
	// reset historical statistics
	// TODO: make this more fine-grained, only when new setting is a higher watermark should we reset.
	m.highUsageCountByResName = map[v1.ResourceName]int{}

	m.cpuThrottlingActive = false
	return nil
}

func (m *monitor) utilizationMonitoring() {
	m.Lock()
	defer m.Unlock()

	node, err := m.getNodeFunc()
	if err != nil {
		klog.ErrorS(err, "Eviction: failed to get node")
		m.highUsageCountByResName = map[v1.ResourceName]int{}
		return
	}
	nodeCopy := node.DeepCopy()

	// check if resource usage is high
	usage := m.usageGetter.UsagesByPercentage(nodeCopy)
	for _, res := range apis.OverSubscriptionResourceTypes {
		if m.isHighResourceUsageOnce(nodeCopy, apis.Resource(usage), res) {
			m.highUsageCountByResName[res]++
		} else {
			m.highUsageCountByResName[res] = 0
		}
	}
}

func (m *monitor) detect() {
	node, err := m.getNodeFunc()
	if err != nil {
		klog.ErrorS(err, "Eviction: failed to get node")
		return
	}
	nodeCopy := node.DeepCopy()

	allResourcesAreLowUsage := true
	for _, res := range apis.OverSubscriptionResourceTypes {
		// Getting pod to be evicted should be executed in every resource for loop,
		// it's important because for every resource we should get the latest pods state.
		_, resList, err := utilnode.GetLatestPodsAndResList(nodeCopy, m.getPodsFunc, res)
		if err != nil {
			klog.ErrorS(err, "Failed to get pods and resource list")
			return
		}
		if m.ShouldEvict(nodeCopy, res, resList, m.nodeHasPressure(res)) {
			event := framework.NodeMonitorEvent{
				TimeStamp: time.Now(),
				Resource:  res,
			}
			klog.InfoS("Node pressure detected", "resource", res, "time", event.TimeStamp)
			m.queue.Add(event)
		}

		usage := m.usageGetter.UsagesByPercentage(nodeCopy)
		if !m.isLowResourceUsageOnce(nodeCopy, apis.Resource(usage), res) {
			allResourcesAreLowUsage = false
		}
	}

	m.detectCPUThrottling(nodeCopy)

	// Only remove eviction annotation when all resources are low usage.
	if !allResourcesAreLowUsage {
		return
	}
	if err := m.RecoverSchedule(); err != nil {
		klog.ErrorS(err, "Failed to recover schedule")
	}
}

func (m *monitor) isHighResourceUsageOnce(node *v1.Node, usage apis.Resource, resName v1.ResourceName) bool {
	m.cfgLock.RLock()
	defer m.cfgLock.RUnlock()
	//TODO: set in node config
	_, highWatermark, exists, err := utilnode.WatermarkAnnotationSetting(node)
	if !exists {
		return usage[resName] >= int64(m.highWatermark[resName])
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get watermark in annotation")
		return usage[resName] >= int64(m.highWatermark[resName])
	}
	return usage[resName] >= highWatermark[resName]
}

func (m *monitor) isLowResourceUsageOnce(node *v1.Node, usage apis.Resource, resName v1.ResourceName) bool {
	m.cfgLock.RLock()
	defer m.cfgLock.RUnlock()
	lowWatermark, _, exists, err := utilnode.WatermarkAnnotationSetting(node)
	if !exists {
		return usage[resName] <= int64(m.lowWatermark[resName])
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get watermark in annotation")
		return usage[resName] <= int64(m.lowWatermark[resName])
	}
	return usage[resName] <= lowWatermark[resName]
}

func (m *monitor) nodeHasPressure(resName v1.ResourceName) bool {
	m.Lock()
	defer m.Unlock()

	return m.highUsageCountByResName[resName] >= highUsageCountLimit
}

func (m *monitor) detectCPUThrottling(node *v1.Node) {
	m.cfgLock.RLock()
	throttlingThreshold := m.cpuThrottlingThreshold
	protectionWatermark := m.cpuProtectionWatermark
	m.cfgLock.RUnlock()

	if throttlingThreshold == 0 || protectionWatermark == 0 {
		return
	}

	usage := m.usageGetter.UsagesByPercentage(node)
	cpuUsage := usage[v1.ResourceCPU]

	klog.V(4).InfoS("CPU throttling detection",
		"usage", cpuUsage,
		"throttlingThreshold", throttlingThreshold,
		"protectionWatermark", protectionWatermark,
		"active", m.cpuThrottlingActive)

	// 启动限流：CPU使用率超过阈值且当前未激活限流
	if !m.cpuThrottlingActive && cpuUsage >= int64(throttlingThreshold) {
		m.cpuThrottlingActive = true
		event := framework.NodeCPUThrottleEvent{
			TimeStamp: time.Now(),
			Resource:  v1.ResourceCPU,
			Action:    "start",
			Usage:     cpuUsage,
		}
		m.queue.Add(event)
		klog.InfoS("CPU throttling started",
			"usage", cpuUsage,
			"throttlingThreshold", throttlingThreshold)
		return
	}

	// 持续限流：已激活限流且CPU使用率仍然超过阈值
	if m.cpuThrottlingActive && cpuUsage >= int64(throttlingThreshold) {
		event := framework.NodeCPUThrottleEvent{
			TimeStamp: time.Now(),
			Resource:  v1.ResourceCPU,
			Action:    "continue", // 新增continue动作
			Usage:     cpuUsage,
		}
		m.queue.Add(event)
		klog.V(2).InfoS("CPU throttling continued",
			"usage", cpuUsage,
			"throttlingThreshold", throttlingThreshold)
		return
	}

	if m.cpuThrottlingActive && cpuUsage <= int64(protectionWatermark) {
		m.cpuThrottlingActive = false
		event := framework.NodeCPUThrottleEvent{
			TimeStamp: time.Now(),
			Resource:  v1.ResourceCPU,
			Action:    "stop",
			Usage:     cpuUsage,
		}
		m.queue.Add(event)
		klog.InfoS("CPU throttling stopped",
			"usage", cpuUsage,
			"protectionWatermark", protectionWatermark)
		return
	}

	return
}

func (m *monitor) IsCPUThrottlingActive() bool {
	m.cfgLock.RLock()
	defer m.cfgLock.RUnlock()
	return m.cpuThrottlingActive
}

func (m *monitor) GetCPUThrottlingConfig() (threshold, watermark int) {
	m.cfgLock.RLock()
	defer m.cfgLock.RUnlock()
	return m.cpuThrottlingThreshold, m.cpuProtectionWatermark
}
