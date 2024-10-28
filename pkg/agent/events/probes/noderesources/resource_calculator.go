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

package noderesources

import (
	"fmt"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/probes"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/oversubscription/queue"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/metriccollect/local"
)

func init() {
	probes.RegisterEventProbeFunc(string(framework.NodeResourcesEventName), NewCalculator)
}

// historicalUsageCalculator is used to calculate the overSubscription
type historicalUsageCalculator struct {
	sync.Mutex
	cfgLock sync.Mutex
	policy.Interface
	usages        workqueue.RateLimitingInterface
	queue         *queue.SqQueue
	resourceTypes sets.String
	getNodeFunc   utilnode.ActiveNode
}

// NewCalculator return overSubscription reporter by algorithm
func NewCalculator(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, workQueue workqueue.RateLimitingInterface) framework.Probe {
	sqQueue := queue.NewSqQueue()
	return &historicalUsageCalculator{
		Interface:     policy.GetPolicyFunc(config.GenericConfiguration.OverSubscriptionPolicy)(config, mgr, nil, sqQueue, local.CollectorName),
		usages:        workQueue,
		queue:         sqQueue,
		resourceTypes: sets.NewString(),
		getNodeFunc:   config.GetNode,
	}
}

func (r *historicalUsageCalculator) Run(stop <-chan struct{}) {
	klog.InfoS("Started nodeResources probe")
	go wait.Until(r.CalOverSubscriptionResources, 10*time.Second, stop)
	go wait.Until(r.preProcess, 10*time.Second, stop)
}

func (r *historicalUsageCalculator) ProbeName() string {
	return "NodeResourcesProbe"
}

func (r *historicalUsageCalculator) RefreshCfg(cfg *api.ColocationConfig) error {
	r.cfgLock.Lock()
	defer r.cfgLock.Unlock()

	if cfg == nil || cfg.OverSubscriptionConfig == nil || cfg.OverSubscriptionConfig.OverSubscriptionTypes == nil {
		return fmt.Errorf("nil colocation cfg")
	}
	// refresh overSubscription resource types.
	set := sets.NewString()
	typ := strings.Split(*cfg.OverSubscriptionConfig.OverSubscriptionTypes, ",")
	for _, resType := range typ {
		if resType == "" {
			continue
		}
		set.Insert(strings.TrimSpace(resType))
	}
	r.resourceTypes = set
	return nil
}

func (r *historicalUsageCalculator) preProcess() {
	node, err := r.getNodeFunc()
	if err != nil {
		klog.ErrorS(nil, "OverSubscription: failed to get node")
		return
	}
	nodeCopy := node.DeepCopy()
	if !r.SupportOverSubscription(nodeCopy) {
		return
	}

	overSubRes := r.computeOverSubRes()
	if overSubRes == nil {
		return
	}
	customizationTypes := r.getOverSubscriptionTypes(nodeCopy)
	for _, resType := range apis.OverSubscriptionResourceTypes {
		if !customizationTypes[resType] {
			overSubRes[resType] = 0
		}
	}
	r.usages.Add(framework.NodeResourceEvent{MillCPU: overSubRes[v1.ResourceCPU], MemoryBytes: overSubRes[v1.ResourceMemory]})
}

// computeOverSubRes calculate overSubscription resources
func (r *historicalUsageCalculator) computeOverSubRes() apis.Resource {
	historicalUsages := r.queue.GetAll()
	if len(historicalUsages) == 0 {
		return nil
	}

	overSubRes := make(apis.Resource)
	totalWeight := int64(0)
	initWeight := int64(1)
	for _, usage := range historicalUsages {
		totalWeight += initWeight
		for _, res := range apis.OverSubscriptionResourceTypes {
			overSubRes[res] = overSubRes[res] + usage[res]*initWeight
		}
		initWeight = initWeight * 2
	}
	for _, res := range apis.OverSubscriptionResourceTypes {
		overSubRes[res] = overSubRes[res] / totalWeight
	}
	return overSubRes
}

func (r *historicalUsageCalculator) getOverSubscriptionTypes(node *v1.Node) map[v1.ResourceName]bool {
	r.cfgLock.Lock()
	defer r.cfgLock.Unlock()
	ret := make(map[v1.ResourceName]bool)
	for _, item := range r.resourceTypes.List() {
		ret[v1.ResourceName(item)] = true
	}

	// be compatible with old api.
	value, exists := node.Annotations[apis.OverSubscriptionTypesKey]
	if !exists || value == "" {
		return ret
	}
	ret = make(map[v1.ResourceName]bool)
	typ := strings.Split(value, ",")
	for _, resType := range typ {
		if resType == "" {
			continue
		}
		ret[v1.ResourceName(strings.TrimSpace(resType))] = true
	}
	return ret
}
