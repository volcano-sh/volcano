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

package oversubscription

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/metrics"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

const (
	reSyncPeriod = 6 // report overSubscription resources every 10s, so 6 represents 1 minutes.
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.NodeResourcesEventName), NewReporter)
}

type reporter struct {
	*base.BaseHandle
	policy.Interface
	enabled     bool
	getNodeFunc utilnode.ActiveNode
	getPodsFunc utilpod.ActivePods
	killPodFunc utilpod.KillPod
	reportTimes int64
}

func NewReporter(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	evictor := eviction.NewEviction(config.GenericConfiguration.KubeClient, config.GenericConfiguration.KubeNodeName)
	return &reporter{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.OverSubscriptionFeature),
			Config: config,
		},
		Interface:   policy.GetPolicyFunc(config.GenericConfiguration.OverSubscriptionPolicy)(config, nil, evictor, nil, ""),
		enabled:     true,
		getPodsFunc: config.GetActivePods,
		getNodeFunc: config.GetNode,
	}
}

func (r *reporter) Handle(event interface{}) error {
	nodeResourceEvent, ok := event.(framework.NodeResourceEvent)
	if !ok {
		klog.ErrorS(nil, "Invalid node resource event", "type", reflect.TypeOf(event))
		return nil
	}

	overSubRes := apis.Resource{
		corev1.ResourceCPU:    nodeResourceEvent.MillCPU,
		corev1.ResourceMemory: nodeResourceEvent.MemoryBytes,
	}

	node, err := r.getNodeFunc()
	if err != nil {
		klog.ErrorS(nil, "overSubscription: failed to get node")
		return err
	}
	nodeCopy := node.DeepCopy()

	if !utilnode.IsNodeSupportOverSubscription(nodeCopy) {
		return nil
	}

	r.reportTimes++
	if !r.shouldPatchOverSubscription(nodeCopy, overSubRes) {
		return nil
	}

	if err = r.UpdateOverSubscription(overSubRes); err != nil {
		klog.ErrorS(err, "OverSubscription: failed to update overSubscription resource")
		return nil
	}
	metrics.UpdateOverSubscriptionResourceQuantity(r.Config.GenericConfiguration.KubeNodeName, overSubRes)
	return nil
}

func (r *reporter) RefreshCfg(cfg *api.ColocationConfig) error {
	if err := r.BaseHandle.RefreshCfg(cfg); err != nil {
		return err
	}

	r.Lock.Lock()
	defer r.Lock.Unlock()

	emptyRes := apis.Resource{
		corev1.ResourceCPU:    0,
		corev1.ResourceMemory: 0,
	}
	// OverSubscriptionConfig is disabled.
	if !*cfg.OverSubscriptionConfig.Enable {
		r.enabled = false
		metrics.UpdateOverSubscriptionResourceQuantity(r.Config.GenericConfiguration.KubeNodeName, emptyRes)
		return r.Cleanup()
	}

	// OverSubscriptionConfig is enabled, we should check node label because overSubscription can be turned off by reset node label, too.
	// node colocation and subscription are turned off by nodePool setting, do clean up.
	if r.enabled && !*cfg.NodeLabelConfig.NodeOverSubscriptionEnable {
		metrics.UpdateOverSubscriptionResourceQuantity(r.Config.GenericConfiguration.KubeNodeName, emptyRes)
		return r.Cleanup()
	} else {
		// overSubscription is turned on by OverSubscriptionConfig, set node label.
		if err := utilnode.SetOverSubscriptionLabel(r.Config); err != nil {
			klog.ErrorS(err, "Failed to set overSubscription label")
			return err
		}
		r.enabled = true
		klog.InfoS("Successfully set overSubscription label=true")
		return nil
	}
}

func (r *reporter) shouldPatchOverSubscription(node *corev1.Node, usage apis.Resource) bool {
	// re-sync overSubscription resources every 1 minute.
	if r.reportTimes%reSyncPeriod == 0 {
		return true
	}
	return r.ShouldUpdateOverSubscription(node, usage)
}
