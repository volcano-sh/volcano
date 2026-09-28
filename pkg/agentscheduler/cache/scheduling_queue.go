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

package cache

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/schedulinggates"

	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"
)

// NewSchedulingQueue creates an agent scheduling queue with scheduling gate admission
// and the events needed to wake gated pods when their gates are removed.
func NewSchedulingQueue(ctx context.Context, schedulerName string, informerFactory informers.SharedInformerFactory, opts ...k8sschedulingqueue.Option) (k8sschedulingqueue.SchedulingQueue, error) {
	plugin, err := schedulinggates.New(ctx, nil, nil, feature.Features{
		EnableSchedulingQueueHint: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.SchedulerQueueingHints),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize SchedulingGates: %w", err)
	}
	gates := plugin.(*schedulinggates.SchedulingGates)
	events, err := gates.EventsToRegister(ctx)
	if err != nil {
		return nil, fmt.Errorf("register SchedulingGates events: %w", err)
	}
	opts = append(opts,
		k8sschedulingqueue.WithPreEnqueuePluginMap(map[string]map[string]fwk.PreEnqueuePlugin{
			schedulerName: {gates.Name(): gates},
		}),
		k8sschedulingqueue.WithQueueingHintMapPerProfile(k8sschedulingqueue.QueueingHintMapPerProfile{
			schedulerName: buildQueueingHintMap(gates.Name(), events),
		}),
	)
	return k8sschedulingqueue.NewSchedulingQueue(Less, informerFactory, opts...), nil
}

// buildQueueingHintMap preserves the default wildcard hint and adds named plugin events.
func buildQueueingHintMap(pluginName string, events []fwk.ClusterEventWithHint) k8sschedulingqueue.QueueingHintMap {
	hints := k8sschedulingqueue.QueueingHintMap{
		{Resource: fwk.WildCard, ActionType: fwk.All}: {
			{QueueingHintFn: func(_ klog.Logger, _ *v1.Pod, _, _ interface{}) (fwk.QueueingHint, error) {
				return fwk.Queue, nil
			}},
		},
	}
	for _, event := range events {
		hints[event.Event] = append(hints[event.Event], &k8sschedulingqueue.QueueingHintFunction{
			PluginName:     pluginName,
			QueueingHintFn: event.QueueingHintFn,
		})
	}
	return hints
}
