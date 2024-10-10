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

package eviction

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/oversubscription/queue"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.NodeMonitorEventName), NewManager)
}

type manager struct {
	cfg *config.Configuration
	eviction.Eviction
	policy.Interface
	getNodeFunc utilnode.ActiveNode
	getPodsFunc utilpod.ActivePods
}

func NewManager(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	evictor := eviction.NewEviction(config.GenericConfiguration.KubeClient, config.GenericConfiguration.KubeNodeName)
	m := &manager{
		cfg:         config,
		Eviction:    evictor,
		Interface:   policy.GetPolicyFunc(config.GenericConfiguration.OverSubscriptionPolicy)(config, mgr, evictor, queue.NewSqQueue(), ""),
		getNodeFunc: config.GetNode,
		getPodsFunc: config.GetActivePods,
	}
	return m
}

func (m *manager) Handle(event interface{}) error {
	nodeMonitorEvent, ok := event.(framework.NodeMonitorEvent)
	if !ok {
		klog.ErrorS(nil, "Invalid node monitor event", "type", reflect.TypeOf(event))
		return nil
	}

	klog.InfoS("Received node pressure event", "resource", nodeMonitorEvent.Resource, "time", nodeMonitorEvent.TimeStamp)
	node, err := m.getNodeFunc()
	if err != nil {
		klog.ErrorS(err, "Failed to get node")
		return err
	}
	nodeCopy := node.DeepCopy()

	for _, res := range apis.OverSubscriptionResourceTypes {
		if res != nodeMonitorEvent.Resource {
			continue
		}
		preemptablePods, _, err := utilnode.GetLatestPodsAndResList(nodeCopy, m.getPodsFunc, res)
		if err != nil {
			klog.ErrorS(err, "Failed to get pods and resource list")
			return err
		}

		for _, pod := range preemptablePods {
			if err = m.DisableSchedule(); err != nil {
				klog.ErrorS(err, "Failed to add eviction annotation")
			}
			klog.InfoS("Successfully disable schedule")

			klog.InfoS("Try to evict pod", "pod", klog.KObj(pod))
			if m.Evict(context.TODO(), pod, m.cfg.GenericConfiguration.Recorder, 0, fmt.Sprintf("Evict offline pod due to %s resource pressure", res)) {
				break
			}
		}
	}
	return nil
}

func (m *manager) RefreshCfg(cfg *api.ColocationConfig) error {
	return nil
}

func (m *manager) IsActive() bool {
	return true
}

func (m *manager) HandleName() string {
	return string(features.EvictionFeature)
}
