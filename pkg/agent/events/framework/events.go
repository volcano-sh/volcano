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
	"time"

	"golang.org/x/time/rate"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type EventQueue struct {
	// Name is the name of event.
	Name string
	// Workers is the number of handle workers that are allowed to sync concurrently.
	Workers int
	// Queue for caching events
	Queue workqueue.RateLimitingInterface
	// List of handlers that need to handle this event.
	Handlers []Handle
	// List of event detectors.
	// Generate an event and put the event in the queue
	Probes []Probe
}

func NewEventQueue(name string) *EventQueue {
	rateLimiter := workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(1*time.Second, 100*time.Second),
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)
	return &EventQueue{
		Name:    name,
		Workers: 30,
		Queue:   workqueue.NewNamedRateLimitingQueue(rateLimiter, name),
	}
}

func (eq *EventQueue) SetWorkers(workers int) {
	eq.Workers = workers
}

func (eq *EventQueue) AddHandler(handle ...Handle) {
	eq.Handlers = append(eq.Handlers, handle...)
}

func (eq *EventQueue) AddProbe(p ...Probe) {
	eq.Probes = append(eq.Probes, p...)
}

func (eq *EventQueue) GetQueue() workqueue.RateLimitingInterface {
	return eq.Queue
}

func (eq *EventQueue) ProcessEvent(ctx context.Context) {
	for {
		if !eq.processNextWorkItem(ctx) {
			klog.ErrorS(nil, "Event queue shut down")
			break
		}
	}
}

func (eq *EventQueue) processNextWorkItem(ctx context.Context) bool {
	key, quit := eq.Queue.Get()
	if quit {
		return false
	}
	defer eq.Queue.Done(key)

	visitErr := false
	defer func() {
		if visitErr {
			eq.Queue.AddRateLimited(key)
		} else {
			eq.Queue.Forget(key)
		}
	}()

	for _, handler := range eq.Handlers {
		if !handler.IsActive() {
			continue
		}
		err := handler.Handle(key)
		if err != nil {
			klog.ErrorS(err, "Handle process failed", "handler", handler.HandleName())
			visitErr = true
		}
	}
	return true
}
