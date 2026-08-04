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

package hintprovider

import (
	"context"

	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// KubeHintProvider adapts a wrapped kube-scheduler scheduling plugin
// (implementing fwk.EnqueueExtensions) into an api.HintProvider.
type KubeHintProvider struct {
	Ext fwk.EnqueueExtensions
}

// EventsToRegister implements api.HintProvider. It adapts the wrapped plugin's
// QueueingHint declarations into Volcano cluster events.
func (f *KubeHintProvider) EventsToRegister(ctx context.Context) ([]api.ClusterEventWithHint, error) {
	events, err := f.Ext.EventsToRegister(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]api.ClusterEventWithHint, 0, len(events))
	for _, e := range events {
		result = append(result, api.ClusterEventWithHint{
			Event: api.ClusterEvent{
				Resource:   e.Event.Resource,
				ActionType: e.Event.ActionType,
			},
			HintFn: wrapPodHint(e.QueueingHintFn),
		})
	}
	return result, nil
}

// wrapPodHint adapts a kube-scheduler QueueingHintFn (which reasons about a
// single Pod) into a Volcano JobHintFn. The event wakes the Job if the wrapped
// hint says any task the plugin rejected may now be schedulable. Waking is only
// a cache-invalidation signal: the gang/minAvailable decision is still made when
// the Job is re-evaluated in the next session.
func wrapPodHint(hintFn fwk.QueueingHintFn) api.JobHintFn {
	return func(job *api.JobInfo, rejection api.Rejection, oldObj, newObj any) (api.HintResult, error) {
		if hintFn == nil {
			// No hint means "always retry on this event".
			return api.HintWakeup, nil
		}

		// Predicate rejections always carry the tasks that failed the filter. If
		// none were recorded there is nothing to test, so wake conservatively
		// rather than leave the Job cached until the watchdog.
		if len(rejection.Tasks) == 0 {
			return api.HintWakeup, nil
		}

		for _, tid := range rejection.Tasks {
			task, ok := job.Tasks[tid]
			if !ok || task.Pod == nil {
				continue
			}
			hint, err := hintFn(klog.Background(), task.Pod, oldObj, newObj)
			if err != nil {
				// On error, upstream semantics treat the hint as Queue.
				return api.HintWakeup, err
			}
			// Upstream fwk.Queue means "this Pod may now be schedulable", so we treat it as a wake-up.
			// Notice: if one of the rejected tasks is now schedulable, we wake the Job; we do not require all of them to be schedulable.
			// This is used to currently avoid overly conservative and inaccurate hintFn from preventing job rescheduling from being triggered
			if hint == fwk.Queue {
				return api.HintWakeup, nil
			}
		}
		return api.HintSkip, nil
	}
}
