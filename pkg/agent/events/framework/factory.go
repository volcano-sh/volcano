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

package framework

import (
	"context"
	"sync"
	"time"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/config/api"
)

type EventQueueFactory struct {
	Mutex  sync.RWMutex
	Queues map[string]*EventQueue
}

func (f *EventQueueFactory) EventQueue(name string) *EventQueue {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	if queue, ok := f.Queues[name]; ok {
		return queue
	}

	eventQueue := NewEventQueue(name)
	f.Queues[name] = eventQueue
	return eventQueue
}

func (f *EventQueueFactory) RegistryEventHandler(name string, handle Handle) Handle {
	f.EventQueue(name).AddHandler(handle)
	klog.InfoS("Successfully registry event handler", "event", name, "handler", handle.HandleName())
	return handle
}

func (f *EventQueueFactory) RegistryEventProbe(name string, probe Probe) Probe {
	f.EventQueue(name).AddProbe(probe)
	klog.InfoS("Successfully registry event probe", "name", name, "probe", probe.ProbeName())
	return probe
}

func (f *EventQueueFactory) SyncConfig(cfg *api.ColocationConfig) error {
	f.Mutex.Lock()
	defer f.Mutex.Unlock()

	var errs []error
	for _, queue := range f.Queues {
		for _, handler := range queue.Handlers {
			if syncErr := handler.RefreshCfg(cfg); syncErr != nil {
				errs = append(errs, syncErr)
			}
		}
		for _, probe := range queue.Probes {
			if syncErr := probe.RefreshCfg(cfg); syncErr != nil {
				errs = append(errs, syncErr)
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

func (f *EventQueueFactory) Start(ctx context.Context) error {
	klog.InfoS("Start event queue factory")
	for _, event := range f.Queues {
		for _, probe := range event.Probes {
			go probe.Run(ctx.Done())
		}
	}

	for _, event := range f.Queues {
		for i := 0; i < event.Workers; i++ {
			go wait.UntilWithContext(ctx, event.ProcessEvent, time.Second)
		}
	}
	klog.InfoS("Successfully started event queue factory")
	return nil
}
