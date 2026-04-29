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
	"time"

	v1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
)

type podStatusThrottleConfig struct {
	lowPressureThreshold  int
	highPressureThreshold int
	lowPressureInterval   time.Duration
	midPressureInterval   time.Duration
	highPressureInterval  time.Duration
	eventLowInterval      time.Duration
	eventMidInterval      time.Duration
	eventHighInterval     time.Duration
}

func newDefaultPodStatusThrottleConfig() podStatusThrottleConfig {
	return podStatusThrottleConfig{
		lowPressureThreshold:  options.DefaultPodStatusLowPressureThreshold,
		highPressureThreshold: options.DefaultPodStatusHighPressureThreshold,
		lowPressureInterval:   options.DefaultPodStatusLowPressureInterval,
		midPressureInterval:   options.DefaultPodStatusMidPressureInterval,
		highPressureInterval:  options.DefaultPodStatusHighPressureInterval,
		eventLowInterval:      options.DefaultPodEventLowPressureInterval,
		eventMidInterval:      options.DefaultPodEventMidPressureInterval,
		eventHighInterval:     options.DefaultPodEventHighPressureInterval,
	}
}

func buildPodStatusThrottleConfig(opts *options.ServerOption) podStatusThrottleConfig {
	cfg := newDefaultPodStatusThrottleConfig()
	if opts == nil {
		return cfg
	}
	if opts.PodStatusLowPressureThreshold >= 0 {
		cfg.lowPressureThreshold = opts.PodStatusLowPressureThreshold
	}
	if opts.PodStatusHighPressureThreshold > 0 {
		cfg.highPressureThreshold = opts.PodStatusHighPressureThreshold
	}
	if opts.PodStatusLowPressureInterval >= 0 {
		cfg.lowPressureInterval = opts.PodStatusLowPressureInterval
	}
	if opts.PodStatusMidPressureInterval >= 0 {
		cfg.midPressureInterval = opts.PodStatusMidPressureInterval
	}
	if opts.PodStatusHighPressureInterval > 0 {
		cfg.highPressureInterval = opts.PodStatusHighPressureInterval
	}
	if opts.PodEventLowPressureInterval >= 0 {
		cfg.eventLowInterval = opts.PodEventLowPressureInterval
	}
	if opts.PodEventMidPressureInterval >= 0 {
		cfg.eventMidInterval = opts.PodEventMidPressureInterval
	}
	if opts.PodEventHighPressureInterval > 0 {
		cfg.eventHighInterval = opts.PodEventHighPressureInterval
	}
	return cfg
}

func (sc *SchedulerCache) getPodStatusThrottleConfig() podStatusThrottleConfig {
	if sc.podStatusThrottle == nil {
		return newDefaultPodStatusThrottleConfig()
	}
	return *sc.podStatusThrottle
}

func (sc *SchedulerCache) recordPodStatusSync(pod *v1.Pod, condition *v1.PodCondition) {
	sc.podStatusSyncLock.Lock()
	defer sc.podStatusSyncLock.Unlock()

	if sc.podStatusSyncCache == nil {
		sc.podStatusSyncCache = make(map[k8stypes.UID]podStatusSyncMeta)
	}
	// Missing key is safe here: Go map lookup returns zero-value metadata.
	last := sc.podStatusSyncCache[pod.UID]
	last.LastSyncedAt = time.Now()
	last.Phase = pod.Status.Phase
	last.Reason = condition.Reason
	last.Message = condition.Message
	sc.podStatusSyncCache[pod.UID] = last
}

func (sc *SchedulerCache) recordUnschedulableEventIfNeeded(pod *v1.Pod, condition *v1.PodCondition, pendingTaskCount int) {
	if !sc.shouldEmitUnschedulableEvent(pod, condition, pendingTaskCount) {
		return
	}
	// The event reason should be "FailedScheduling" (same convention as upstream scheduler).
	sc.Recorder.Eventf(pod, v1.EventTypeWarning, "FailedScheduling", condition.Message)
	sc.recordUnschedulableEventSync(pod, condition)
}

func (sc *SchedulerCache) shouldEmitUnschedulableEvent(pod *v1.Pod, condition *v1.PodCondition, pendingTaskCount int) bool {
	sc.podStatusSyncLock.Lock()
	defer sc.podStatusSyncLock.Unlock()

	last, found := sc.podStatusSyncCache[pod.UID]
	// Always emit the first event for this pod from this cache instance.
	if !found {
		return true
	}
	// Emit immediately when reason/message changes to preserve diagnostic signal.
	if last.EventReason != condition.Reason || last.EventMessage != condition.Message {
		return true
	}

	interval := sc.getDynamicPodEventInterval(pendingTaskCount)
	if interval <= 0 {
		return true
	}
	return time.Since(last.LastEventAt) >= interval
}

func (sc *SchedulerCache) recordUnschedulableEventSync(pod *v1.Pod, condition *v1.PodCondition) {
	sc.podStatusSyncLock.Lock()
	defer sc.podStatusSyncLock.Unlock()

	if sc.podStatusSyncCache == nil {
		sc.podStatusSyncCache = make(map[k8stypes.UID]podStatusSyncMeta)
	}

	// Missing key is safe here: Go map lookup returns zero-value metadata.
	last := sc.podStatusSyncCache[pod.UID]
	last.LastEventAt = time.Now()
	last.EventReason = condition.Reason
	last.EventMessage = condition.Message
	sc.podStatusSyncCache[pod.UID] = last
}

func (sc *SchedulerCache) removePodStatusSyncMeta(uid k8stypes.UID) {
	sc.podStatusSyncLock.Lock()
	defer sc.podStatusSyncLock.Unlock()
	delete(sc.podStatusSyncCache, uid)
}

func (sc *SchedulerCache) shouldThrottlePodStatusUpdate(pod *v1.Pod, condition *v1.PodCondition, updateNomiNode bool, pendingTaskCount int) bool {
	cfg := sc.getPodStatusThrottleConfig()
	// Never throttle nominated-node changes: this field is consumed by cluster-autoscaler.
	if updateNomiNode {
		klog.V(5).Infof("Skip status throttling for %s/%s: nominatedNodeName is updated", pod.Namespace, pod.Name)
		return false
	}
	if pendingTaskCount < cfg.lowPressureThreshold {
		klog.V(5).Infof("Skip status throttling for %s/%s: pendingTaskCount=%d below low pressure threshold=%d",
			pod.Namespace, pod.Name, pendingTaskCount, cfg.lowPressureThreshold)
		return false
	}

	sc.podStatusSyncLock.Lock()
	defer sc.podStatusSyncLock.Unlock()

	last, found := sc.podStatusSyncCache[pod.UID]
	if !found {
		klog.V(5).Infof("Skip status throttling for %s/%s: no previous sync history in podStatusSyncCache",
			pod.Namespace, pod.Name)
		return false
	}
	if last.Phase != pod.Status.Phase || last.Reason != condition.Reason {
		klog.V(5).Infof("Skip status throttling for %s/%s: status tuple changed (lastPhase=%s,lastReason=%s,currentPhase=%s,currentReason=%s)",
			pod.Namespace, pod.Name, last.Phase, last.Reason, pod.Status.Phase, condition.Reason)
		return false
	}
	// In high pressure, unstable diagnostic message text can change frequently
	// and trigger unnecessary status writes. Throttle by phase/reason first.
	if pendingTaskCount <= cfg.highPressureThreshold && last.Message != condition.Message {
		klog.V(5).Infof("Skip status throttling for %s/%s: message changed under non-high pressure (pendingTaskCount=%d, highPressureThreshold=%d)",
			pod.Namespace, pod.Name, pendingTaskCount, cfg.highPressureThreshold)
		return false
	}
	interval := sc.getDynamicPodStatusInterval(pendingTaskCount)
	if interval <= 0 {
		klog.V(5).Infof("Skip status throttling for %s/%s: dynamic status interval=%v", pod.Namespace, pod.Name, interval)
		return false
	}
	lastSyncAgo := time.Since(last.LastSyncedAt)
	throttled := lastSyncAgo < interval
	klog.V(5).Infof("Status throttling decision for %s/%s: pendingTaskCount=%d, interval=%v, lastSyncAgo=%v, throttled=%t",
		pod.Namespace, pod.Name, pendingTaskCount, interval, lastSyncAgo, throttled)
	return throttled
}

func (sc *SchedulerCache) getDynamicPodStatusInterval(pendingTaskCount int) time.Duration {
	cfg := sc.getPodStatusThrottleConfig()
	if pendingTaskCount < cfg.lowPressureThreshold {
		return cfg.lowPressureInterval
	}
	if pendingTaskCount > cfg.highPressureThreshold {
		return cfg.highPressureInterval
	}
	return cfg.midPressureInterval
}

func (sc *SchedulerCache) getDynamicPodEventInterval(pendingTaskCount int) time.Duration {
	cfg := sc.getPodStatusThrottleConfig()
	// Event intervals are intentionally independent from status intervals.
	// They are tuned to be more frequent by default for better observability.
	if pendingTaskCount < cfg.lowPressureThreshold {
		return cfg.eventLowInterval
	}
	if pendingTaskCount > cfg.highPressureThreshold {
		return cfg.eventHighInterval
	}
	return cfg.eventMidInterval
}
