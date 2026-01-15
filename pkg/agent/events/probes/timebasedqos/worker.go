/*
Copyright 2025 The Volcano Authors.

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

package timebasedqos

import (
	"context"
	"k8s.io/klog/v2"
	"time"
	"volcano.sh/volcano/pkg/agent/events/framework"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"

	"volcano.sh/volcano/pkg/agent/config/api"
)

type policyWorker struct {
	policy   *api.TimeBasedQoSPolicy
	queue    workqueue.RateLimitingInterface
	stopFunc context.CancelFunc
	// active indicates whether the worker is in time range and actively sending events.
	active bool
}

func newPolicyWorker(policy *api.TimeBasedQoSPolicy, queue workqueue.RateLimitingInterface) *policyWorker {
	return &policyWorker{
		policy: policy,
		queue:  queue,
	}
}

func (p *policyWorker) run() {
	ctx, cancel := context.WithCancel(context.Background())
	p.stopFunc = cancel

	wait.Until(func() {
		inTimeRange := p.checkIsInTimeRange()
		if inTimeRange {
			if !p.active {
				klog.InfoS("TimeBasedQoS policy is now active", "policy", p.policy.Name)
				p.active = true
			}
			// Send the active event to the queue
			p.queue.Add(framework.TimeBasedQoSPolicyEvent{
				Policy: p.policy,
				Action: framework.TimeBasedQoSPolicyActive,
			})
		} else {
			if p.active {
				klog.InfoS("TimeBasedQoS policy is no longer active", "policy", p.policy.Name)
				p.active = false
				// Send the expired event to the queue
				p.queue.Add(framework.TimeBasedQoSPolicyEvent{
					Policy: p.policy,
					Action: framework.TimeBasedQoSPolicyExpired,
				})
			}
		}
	}, *p.policy.CheckInterval, ctx.Done())
}

func (p *policyWorker) stop() {
	if p.stopFunc != nil {
		p.stopFunc()
	}
}

func (p *policyWorker) checkIsInTimeRange() bool {
	loc := time.Local
	if p.policy.TimeZone != nil {
		if tz, err := time.LoadLocation(*p.policy.TimeZone); err == nil {
			loc = tz
		}
	}
	now := time.Now().In(loc)

	startMinutes := p.parseTimeToMinutes(p.policy.StartTime)
	endMinutes := p.parseTimeToMinutes(p.policy.EndTime)
	currentMinutes := now.Hour()*60 + now.Minute()

	if endMinutes == startMinutes {
		return false
	}

	// Case that span across days, e.g.: 22:00-06:00
	if endMinutes < startMinutes {
		return currentMinutes >= startMinutes || currentMinutes < endMinutes
	}

	return currentMinutes >= startMinutes && currentMinutes < endMinutes
}

func (p *policyWorker) parseTimeToMinutes(timeStr *string) int {
	t, err := time.Parse("15:04", *timeStr)
	if err != nil {
		return 0
	}
	return t.Hour()*60 + t.Minute()
}
