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

package events

import (
	"context"

	"k8s.io/klog/v2"

	coloconfig "volcano.sh/volcano/pkg/agent/config"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/probes"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

type EventManager struct {
	eventQueueFactory    *framework.EventQueueFactory
	config               *config.Configuration
	metricCollectManager *metriccollect.MetricCollectorManager
	configMgr            *coloconfig.ConfigManager
}

func NewEventManager(config *config.Configuration, metricCollectManager *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) *EventManager {
	factory := &framework.EventQueueFactory{}
	factory.Queues = make(map[string]*framework.EventQueue)
	mgr := &EventManager{
		config:               config,
		metricCollectManager: metricCollectManager,
		eventQueueFactory:    factory,
		configMgr:            coloconfig.NewManager(config, []coloconfig.Listener{factory}),
	}

	for eventName, newProbeFuncs := range probes.GetEventProbeFuncs() {
		eventQueue := mgr.eventQueueFactory.EventQueue(eventName)
		for _, newProbeFunc := range newProbeFuncs {
			prob := newProbeFunc(config, metricCollectManager, eventQueue.GetQueue())
			mgr.eventQueueFactory.RegistryEventProbe(eventName, prob)
		}
	}

	for eventName, newHandleFuncs := range handlers.GetEventHandlerFuncs() {
		for _, newHandleFunc := range newHandleFuncs {
			handle := newHandleFunc(config, metricCollectManager, cgroupMgr)
			mgr.eventQueueFactory.RegistryEventHandler(eventName, handle)
		}
	}
	return mgr
}

func (m *EventManager) Run(ctx context.Context) error {
	klog.InfoS("Start event manager")
	if err := m.configMgr.Start(ctx); err != nil {
		return err
	}
	if err := m.eventQueueFactory.Start(ctx); err != nil {
		return err
	}
	klog.InfoS("Successfully started event manager")
	return nil
}
