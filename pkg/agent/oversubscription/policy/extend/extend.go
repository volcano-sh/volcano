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

package extend

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/oversubscription/queue"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/resourceusage"
)

func init() {
	policy.RegistryPolicy(string(ExtendResource), NewExtendResource)
}

const ExtendResource policy.Name = "extend"

type extendResource struct {
	config      *config.Configuration
	getPodsFunc utilpod.ActivePods
	getNodeFunc utilnode.ActiveNode
	evictor     eviction.Eviction
	queue       *queue.SqQueue
	usageGetter resourceusage.Getter
	ratio       int
}

func NewExtendResource(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, evictor eviction.Eviction, queue *queue.SqQueue, collectorName string) policy.Interface {
	return &extendResource{
		config:      config,
		getPodsFunc: config.GetActivePods,
		getNodeFunc: config.GetNode,
		evictor:     evictor,
		queue:       queue,
		usageGetter: resourceusage.NewUsageGetter(mgr, collectorName),
		ratio:       config.GenericConfiguration.OverSubscriptionRatio,
	}
}

func (e *extendResource) Name() string {
	return string(ExtendResource)
}

func (e *extendResource) SupportOverSubscription(node *corev1.Node) bool {
	return utilnode.IsNodeSupportOverSubscription(node)
}

func (e *extendResource) ShouldEvict(node *corev1.Node, resName corev1.ResourceName, resList *utilnode.ResourceList, hasPressure bool) bool {
	return utilnode.IsNodeSupportOverSubscription(node) && hasPressure
}

func (e *extendResource) ShouldUpdateOverSubscription(node *corev1.Node, resource apis.Resource) bool {
	currentOverSubscription := utilnode.GetNodeStatusOverSubscription(node)
	return policy.ShouldUpdateNodeOverSubscription(currentOverSubscription, resource)
}

func (e *extendResource) UpdateOverSubscription(resource apis.Resource) error {
	return utilnode.UpdateNodeExtendResource(e.config, resource)
}

func (e *extendResource) Cleanup() error {
	if err := utilnode.DeleteNodeOverSoldStatus(e.config); err != nil {
		klog.ErrorS(err, "Failed to reset overSubscription info")
		return err
	}
	klog.InfoS("Successfully reset overSubscription info")
	if err := policy.EvictPods(&policy.EvictionCtx{
		Configuration:       e.config,
		Eviction:            e.evictor,
		GracePeriodOverride: 0,
		EvictMsg:            "Evict offline pod due to node overSubscription is turned off",
		GetPodsFunc:         e.getPodsFunc,
		Filter:              utilnode.UseExtendResource,
	}); err != nil {
		return err
	}
	return nil
}

func (e *extendResource) DisableSchedule() error {
	return utilnode.DisableSchedule(e.config)
}

func (e *extendResource) RecoverSchedule() error {
	return utilnode.RecoverSchedule(e.config)
}

func (e *extendResource) CalOverSubscriptionResources() {
	node, err := e.getNodeFunc()
	if err != nil {
		klog.ErrorS(nil, "overSubscription: failed to get node")
		return
	}
	nodeCopy := node.DeepCopy()

	if !e.SupportOverSubscription(nodeCopy) {
		return
	}

	pods, err := e.getPodsFunc()
	if err != nil {
		klog.ErrorS(err, "Failed to get pods")
		return
	}

	includeGuaranteedPods := utilpod.IncludeGuaranteedPods()
	currentUsage := e.usageGetter.UsagesByValue(includeGuaranteedPods)
	overSubscriptionRes := make(apis.Resource)

	for _, resType := range apis.OverSubscriptionResourceTypes {
		total := int64(0)
		switch resType {
		case corev1.ResourceCPU:
			total = node.Status.Allocatable.Cpu().MilliValue() - utilpod.GuaranteedPodsCPURequest(pods)
		case corev1.ResourceMemory:
			total = node.Status.Allocatable.Memory().Value()
		default:
			klog.InfoS("overSubscription: reporter does not support resource", "resourceType", resType)
		}

		if total >= currentUsage[resType] {
			overSubscriptionRes[resType] = (total - currentUsage[resType]) * int64(e.ratio) / 100
		} else {
			overSubscriptionRes[resType] = 0
		}

		klog.V(4).InfoS("overSubscription:", "resourceType", resType, "total", total, "usage", currentUsage[resType], "delta", overSubscriptionRes[resType])
	}
	e.queue.Enqueue(overSubscriptionRes)
}
