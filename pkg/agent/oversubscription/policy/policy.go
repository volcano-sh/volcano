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

package policy

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/oversubscription/queue"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

var (
	lock      sync.Mutex
	policyMap = make(map[string]PolicyFunc)
)

const overSubscriptionChangeStep = 0.1

type PolicyFunc func(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, evictor eviction.Eviction, queue *queue.SqQueue, collectorName string) Interface

type Name string

// Interface defines overSubscription resource policy, support overSubscription resource patch annotation currently.
// You can register your own overSubscription policy like patch overSubscription resource on node.Allocatable.
type Interface interface {
	// Name is the policy name.
	Name() string
	// SupportOverSubscription return whether node support over subscription.
	SupportOverSubscription(node *corev1.Node) bool
	// ShouldEvict return whether we should evict low priority pods.
	ShouldEvict(node *corev1.Node, resName corev1.ResourceName, resList *utilnode.ResourceList, hasPressure bool) bool
	// CalOverSubscriptionResources calculate overSubscription resources.
	CalOverSubscriptionResources()
	// ShouldUpdateOverSubscription return whether new overSubscription resources should be patched.
	ShouldUpdateOverSubscription(node *corev1.Node, resource apis.Resource) bool
	// UpdateOverSubscription will update overSubscription resource to node.
	UpdateOverSubscription(resource apis.Resource) error
	// Cleanup reset overSubscription label and evict low priority pods when turn off overSubscription.
	Cleanup() error
	// DisableSchedule disable schedule.
	DisableSchedule() error
	// RecoverSchedule recover schedule.
	RecoverSchedule() error
}

func RegistryPolicy(name string, policyFunc PolicyFunc) {
	lock.Lock()
	defer lock.Unlock()

	if _, exist := policyMap[name]; exist {
		klog.ErrorS(nil, "Policy has already been registered", "name", name)
		return
	}
	policyMap[name] = policyFunc
}

func GetPolicyFunc(name string) PolicyFunc {
	lock.Lock()
	defer lock.Unlock()

	fn, exist := policyMap[name]
	if !exist {
		klog.Fatalf("Policy %s not registered", name)
	}
	return fn
}

type EvictionCtx struct {
	*config.Configuration
	eviction.Eviction
	GracePeriodOverride int64
	EvictMsg            string
	GetPodsFunc         utilpod.ActivePods
	Filter              func(resName corev1.ResourceName, resList *utilnode.ResourceList) bool
}

func EvictPods(ctx *EvictionCtx) error {
	evict := func(node *corev1.Node) (bool, error) {
		keepGoing := false
		for _, res := range apis.OverSubscriptionResourceTypes {
			// Getting pod to be evicted should be executed in every resource for loop,
			// it's important because for every resource we should get the latest pods state.
			preemptablePods, resList, err := utilnode.GetLatestPodsAndResList(node, ctx.GetPodsFunc, res)
			if err != nil {
				klog.ErrorS(err, "Failed to get pods and resource list")
				return true, err
			}

			if !ctx.Filter(res, resList) {
				continue
			}
			for _, pod := range preemptablePods {
				klog.InfoS("Try to evict pod", "pod", klog.KObj(pod))
				if ctx.Evict(context.TODO(), pod, ctx.GenericConfiguration.Recorder, ctx.GracePeriodOverride, ctx.EvictMsg) {
					keepGoing = true
					break
				}
			}
		}
		return keepGoing, nil
	}

	for {
		node, err := ctx.GetNode()
		if err != nil {
			klog.ErrorS(err, "Failed to get node and pods")
			return err
		}

		keepGoing, err := evict(node)
		if err != nil {
			return err
		}
		if !keepGoing {
			break
		}
	}
	klog.InfoS("Successfully cleaned up resources when turn off oversubscription")
	return nil
}

func ShouldUpdateNodeOverSubscription(current, new apis.Resource) bool {
	update := false
	for _, res := range apis.OverSubscriptionResourceTypes {
		delta := new[res] - current[res]
		if delta < 0 {
			delta = -delta
		}

		if float64(delta)/float64(current[res]) > overSubscriptionChangeStep {
			update = true
			break
		}
	}
	return update
}
