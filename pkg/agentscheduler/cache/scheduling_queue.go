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

// queuePlugin provides admission checks and the events that can unblock a pod.
type queuePlugin interface {
	fwk.PreEnqueuePlugin
	fwk.EnqueueExtensions
}

var _ queuePlugin = (*schedulinggates.SchedulingGates)(nil)

// NewSchedulingQueue creates an agent scheduling queue with its default plugins.
func NewSchedulingQueue(ctx context.Context, schedulerName string, informerFactory informers.SharedInformerFactory, opts ...k8sschedulingqueue.Option) (k8sschedulingqueue.SchedulingQueue, error) {
	plugins, err := newDefaultQueuePlugins(ctx)
	if err != nil {
		return nil, err
	}
	preEnqueue := make(map[string]fwk.PreEnqueuePlugin, len(plugins))
	hints := buildQueueingHintMap()
	for _, plugin := range plugins {
		preEnqueue[plugin.Name()] = plugin
		events, err := plugin.EventsToRegister(ctx)
		if err != nil {
			return nil, fmt.Errorf("register %s events: %w", plugin.Name(), err)
		}
		for _, event := range events {
			hints[event.Event] = append(hints[event.Event], &k8sschedulingqueue.QueueingHintFunction{
				PluginName:     plugin.Name(),
				QueueingHintFn: event.QueueingHintFn,
			})
		}
	}
	opts = append(opts,
		k8sschedulingqueue.WithPreEnqueuePluginMap(map[string]map[string]fwk.PreEnqueuePlugin{
			schedulerName: preEnqueue,
		}),
		k8sschedulingqueue.WithQueueingHintMapPerProfile(k8sschedulingqueue.QueueingHintMapPerProfile{
			schedulerName: hints,
		}),
	)
	return k8sschedulingqueue.NewSchedulingQueue(Less, informerFactory, opts...), nil
}

func newDefaultQueuePlugins(ctx context.Context) ([]queuePlugin, error) {
	plugin, err := schedulinggates.New(ctx, nil, nil, feature.Features{
		EnableSchedulingQueueHint: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.SchedulerQueueingHints),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize SchedulingGates: %w", err)
	}
	gates, ok := plugin.(queuePlugin)
	if !ok {
		return nil, fmt.Errorf("SchedulingGates does not implement queuePlugin")
	}
	return []queuePlugin{gates}, nil
}

// buildQueueingHintMap initializes the existing wildcard hint for scheduling failures.
func buildQueueingHintMap() k8sschedulingqueue.QueueingHintMap {
	return k8sschedulingqueue.QueueingHintMap{
		{Resource: fwk.WildCard, ActionType: fwk.All}: {
			{QueueingHintFn: func(_ klog.Logger, _ *v1.Pod, _, _ interface{}) (fwk.QueueingHint, error) {
				return fwk.Queue, nil
			}},
		},
	}
}
