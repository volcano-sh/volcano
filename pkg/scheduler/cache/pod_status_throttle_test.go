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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

var (
	podStatusLowPressureThreshold  = options.DefaultPodStatusLowPressureThreshold
	podStatusHighPressureThreshold = options.DefaultPodStatusHighPressureThreshold
	podStatusLowPressureInterval   = options.DefaultPodStatusLowPressureInterval
	podStatusMidPressureInterval   = options.DefaultPodStatusMidPressureInterval
	podStatusHighPressureInterval  = options.DefaultPodStatusHighPressureInterval
	podEventLowPressureInterval    = options.DefaultPodEventLowPressureInterval
	podEventMidPressureInterval    = options.DefaultPodEventMidPressureInterval
	podEventHighPressureInterval   = options.DefaultPodEventHighPressureInterval
)

func TestBuildPodStatusThrottleConfig(t *testing.T) {
	t.Run("nil options uses defaults", func(t *testing.T) {
		cfg := buildPodStatusThrottleConfig(nil)

		assert.Equal(t, options.DefaultPodStatusLowPressureThreshold, cfg.lowPressureThreshold)
		assert.Equal(t, options.DefaultPodStatusHighPressureThreshold, cfg.highPressureThreshold)
		assert.Equal(t, options.DefaultPodStatusLowPressureInterval, cfg.lowPressureInterval)
		assert.Equal(t, options.DefaultPodStatusMidPressureInterval, cfg.midPressureInterval)
		assert.Equal(t, options.DefaultPodStatusHighPressureInterval, cfg.highPressureInterval)
		assert.Equal(t, options.DefaultPodEventLowPressureInterval, cfg.eventLowInterval)
		assert.Equal(t, options.DefaultPodEventMidPressureInterval, cfg.eventMidInterval)
		assert.Equal(t, options.DefaultPodEventHighPressureInterval, cfg.eventHighInterval)
	})

	t.Run("explicit low pressure threshold zero takes effect", func(t *testing.T) {
		opts := &options.ServerOption{
			PodStatusLowPressureThreshold:  0,
			PodStatusHighPressureThreshold: 3000,
			PodStatusLowPressureInterval:   5 * time.Second,
			PodStatusMidPressureInterval:   30 * time.Second,
			PodStatusHighPressureInterval:  2 * time.Minute,
			PodEventLowPressureInterval:    3 * time.Second,
			PodEventMidPressureInterval:    20 * time.Second,
			PodEventHighPressureInterval:   1 * time.Minute,
		}

		cfg := buildPodStatusThrottleConfig(opts)

		assert.Equal(t, 0, cfg.lowPressureThreshold)
		assert.Equal(t, 3000, cfg.highPressureThreshold)
		assert.Equal(t, 5*time.Second, cfg.lowPressureInterval)
		assert.Equal(t, 30*time.Second, cfg.midPressureInterval)
		assert.Equal(t, 2*time.Minute, cfg.highPressureInterval)
		assert.Equal(t, 3*time.Second, cfg.eventLowInterval)
		assert.Equal(t, 20*time.Second, cfg.eventMidInterval)
		assert.Equal(t, 1*time.Minute, cfg.eventHighInterval)
	})

	t.Run("invalid override values keep defaults", func(t *testing.T) {
		opts := &options.ServerOption{
			PodStatusLowPressureThreshold:  -1,
			PodStatusHighPressureThreshold: 0,
			PodStatusLowPressureInterval:   -1,
			PodStatusMidPressureInterval:   -1,
			PodStatusHighPressureInterval:  0,
			PodEventLowPressureInterval:    -1,
			PodEventMidPressureInterval:    -1,
			PodEventHighPressureInterval:   0,
		}

		cfg := buildPodStatusThrottleConfig(opts)

		assert.Equal(t, options.DefaultPodStatusLowPressureThreshold, cfg.lowPressureThreshold)
		assert.Equal(t, options.DefaultPodStatusHighPressureThreshold, cfg.highPressureThreshold)
		assert.Equal(t, options.DefaultPodStatusLowPressureInterval, cfg.lowPressureInterval)
		assert.Equal(t, options.DefaultPodStatusMidPressureInterval, cfg.midPressureInterval)
		assert.Equal(t, options.DefaultPodStatusHighPressureInterval, cfg.highPressureInterval)
		assert.Equal(t, options.DefaultPodEventLowPressureInterval, cfg.eventLowInterval)
		assert.Equal(t, options.DefaultPodEventMidPressureInterval, cfg.eventMidInterval)
		assert.Equal(t, options.DefaultPodEventHighPressureInterval, cfg.eventHighInterval)
	})
}

func TestGetPodStatusThrottleConfig_DefaultFallback(t *testing.T) {
	sc := &SchedulerCache{}

	cfg := sc.getPodStatusThrottleConfig()

	assert.Equal(t, options.DefaultPodStatusLowPressureThreshold, cfg.lowPressureThreshold)
	assert.Equal(t, options.DefaultPodStatusHighPressureThreshold, cfg.highPressureThreshold)
	assert.Equal(t, options.DefaultPodStatusLowPressureInterval, cfg.lowPressureInterval)
	assert.Equal(t, options.DefaultPodStatusMidPressureInterval, cfg.midPressureInterval)
	assert.Equal(t, options.DefaultPodStatusHighPressureInterval, cfg.highPressureInterval)
	assert.Equal(t, options.DefaultPodEventLowPressureInterval, cfg.eventLowInterval)
	assert.Equal(t, options.DefaultPodEventMidPressureInterval, cfg.eventMidInterval)
	assert.Equal(t, options.DefaultPodEventHighPressureInterval, cfg.eventHighInterval)
}

func TestGetDynamicPodStatusInterval(t *testing.T) {
	sc := &SchedulerCache{}

	tests := []struct {
		name             string
		pendingTaskCount int
		want             time.Duration
	}{
		{
			name:             "below low threshold uses low interval",
			pendingTaskCount: podStatusLowPressureThreshold - 1,
			want:             podStatusLowPressureInterval,
		},
		{
			name:             "equal low threshold uses mid interval",
			pendingTaskCount: podStatusLowPressureThreshold,
			want:             podStatusMidPressureInterval,
		},
		{
			name:             "equal high threshold uses mid interval",
			pendingTaskCount: podStatusHighPressureThreshold,
			want:             podStatusMidPressureInterval,
		},
		{
			name:             "above high threshold uses high interval",
			pendingTaskCount: podStatusHighPressureThreshold + 1,
			want:             podStatusHighPressureInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sc.getDynamicPodStatusInterval(tt.pendingTaskCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShouldThrottlePodStatusUpdate(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID("pod-1"),
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}

	condition := &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  "Unschedulable",
		Message: "0/3 nodes are available",
	}

	tests := []struct {
		name                string
		lastSyncAgo         time.Duration
		lastPhase           v1.PodPhase
		lastReason          string
		lastMessage         string
		updateNominatedNode bool
		pendingTaskCount    int
		expectThrottle      bool
	}{
		{
			name:                "high pressure repeated status should throttle",
			lastSyncAgo:         2 * time.Second,
			lastPhase:           v1.PodPending,
			lastReason:          "Unschedulable",
			lastMessage:         "0/3 nodes are available",
			updateNominatedNode: false,
			pendingTaskCount:    podStatusHighPressureThreshold + 1,
			expectThrottle:      true,
		},
		{
			name:                "reason changed should not throttle",
			lastSyncAgo:         2 * time.Second,
			lastPhase:           v1.PodPending,
			lastReason:          "DifferentReason",
			lastMessage:         "0/3 nodes are available",
			updateNominatedNode: false,
			pendingTaskCount:    podStatusHighPressureThreshold + 1,
			expectThrottle:      false,
		},
		{
			name:                "high pressure message changed should still throttle",
			lastSyncAgo:         2 * time.Second,
			lastPhase:           v1.PodPending,
			lastReason:          "Unschedulable",
			lastMessage:         "old message",
			updateNominatedNode: false,
			pendingTaskCount:    podStatusHighPressureThreshold + 1,
			expectThrottle:      true,
		},
		{
			name:                "low pressure should not throttle",
			lastSyncAgo:         2 * time.Second,
			lastPhase:           v1.PodPending,
			lastReason:          "Unschedulable",
			lastMessage:         "0/3 nodes are available",
			updateNominatedNode: false,
			pendingTaskCount:    podStatusLowPressureThreshold - 1,
			expectThrottle:      false,
		},
		{
			name:                "nominated node update should not throttle",
			lastSyncAgo:         2 * time.Second,
			lastPhase:           v1.PodPending,
			lastReason:          "Unschedulable",
			lastMessage:         "0/3 nodes are available",
			updateNominatedNode: true,
			pendingTaskCount:    podStatusHighPressureThreshold + 1,
			expectThrottle:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &SchedulerCache{
				podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
			}
			sc.podStatusSyncCache[pod.UID] = podStatusSyncMeta{
				LastSyncedAt: time.Now().Add(-tt.lastSyncAgo),
				Phase:        tt.lastPhase,
				Reason:       tt.lastReason,
				Message:      tt.lastMessage,
			}

			got := sc.shouldThrottlePodStatusUpdate(pod, condition, tt.updateNominatedNode, tt.pendingTaskCount)
			assert.Equal(t, tt.expectThrottle, got)
		})
	}
}

func TestShouldEmitUnschedulableEvent(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID("pod-event-1"),
		},
	}

	condition := &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  "Unschedulable",
		Message: "0/3 nodes are available",
	}

	t.Run("emit first event without history", func(t *testing.T) {
		sc := &SchedulerCache{
			podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
		}
		assert.True(t, sc.shouldEmitUnschedulableEvent(pod, condition, podStatusHighPressureThreshold+1))
	})

	t.Run("suppress repeated event inside interval", func(t *testing.T) {
		sc := &SchedulerCache{
			podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
		}
		sc.podStatusSyncCache[pod.UID] = podStatusSyncMeta{
			LastEventAt:  time.Now().Add(-10 * time.Second),
			EventReason:  "Unschedulable",
			EventMessage: "0/3 nodes are available",
		}
		assert.False(t, sc.shouldEmitUnschedulableEvent(pod, condition, podStatusHighPressureThreshold+1))
	})

	t.Run("emit repeated event after interval", func(t *testing.T) {
		sc := &SchedulerCache{
			podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
		}
		sc.podStatusSyncCache[pod.UID] = podStatusSyncMeta{
			LastEventAt:  time.Now().Add(-121 * time.Second),
			EventReason:  "Unschedulable",
			EventMessage: "0/3 nodes are available",
		}
		assert.True(t, sc.shouldEmitUnschedulableEvent(pod, condition, podStatusHighPressureThreshold+1))
	})

	t.Run("emit immediately when message changes", func(t *testing.T) {
		sc := &SchedulerCache{
			podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
		}
		sc.podStatusSyncCache[pod.UID] = podStatusSyncMeta{
			LastEventAt:  time.Now().Add(-10 * time.Second),
			EventReason:  "Unschedulable",
			EventMessage: "old message",
		}
		assert.True(t, sc.shouldEmitUnschedulableEvent(pod, condition, podStatusHighPressureThreshold+1))
	})
}

func TestRemovePodStatusSyncMeta(t *testing.T) {
	uid := types.UID("pod-sync-meta")
	sc := &SchedulerCache{
		podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
	}
	sc.podStatusSyncCache[uid] = podStatusSyncMeta{
		LastSyncedAt: time.Now(),
		Phase:        v1.PodPending,
		Reason:       "Unschedulable",
		Message:      "0/3 nodes are available",
	}

	sc.removePodStatusSyncMeta(uid)
	_, found := sc.podStatusSyncCache[uid]
	assert.False(t, found)
}

func TestGetDynamicPodEventInterval(t *testing.T) {
	sc := &SchedulerCache{}

	tests := []struct {
		name             string
		pendingTaskCount int
		want             time.Duration
	}{
		{
			name:             "low pressure uses independent event low interval",
			pendingTaskCount: podStatusLowPressureThreshold - 1,
			want:             podEventLowPressureInterval,
		},
		{
			name:             "mid pressure uses independent event mid interval",
			pendingTaskCount: podStatusLowPressureThreshold,
			want:             podEventMidPressureInterval,
		},
		{
			name:             "equal high threshold still uses event mid interval",
			pendingTaskCount: podStatusHighPressureThreshold,
			want:             podEventMidPressureInterval,
		},
		{
			name:             "high pressure uses independent event high interval",
			pendingTaskCount: podStatusHighPressureThreshold + 1,
			want:             podEventHighPressureInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sc.getDynamicPodEventInterval(tt.pendingTaskCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEstimateEtcdStorageGrowthForFrequentUnschedulableUpdates(t *testing.T) {
	const (
		podCount            = 8000
		updateEvery         = 1 * time.Second
		observationWindow   = 5 * time.Minute
		sampleRounds        = 5
		pendingTaskCount    = podCount
		reasonUnschedulable = "Unschedulable"
	)

	sc := &SchedulerCache{
		podStatusSyncCache: make(map[types.UID]podStatusSyncMeta),
	}

	pods := make([]*v1.Pod, 0, podCount)
	for i := 0; i < podCount; i++ {
		pod := buildPod(
			"default",
			fmt.Sprintf("estimate-pod-%d", i),
			"",
			v1.PodPending,
			api.BuildResourceList("10m", "32Mi"),
			nil,
			map[string]string{"app": "storage-estimator"},
		)
		pods = append(pods, pod)
	}

	var sampledPatchBytes int64
	var sampledFullPodBytes int64
	var sampledWrites int64

	for round := 0; round < sampleRounds; round++ {
		msg := fmt.Sprintf("0/%d nodes are available in scheduling cycle %d", podCount, round)
		for _, pod := range pods {
			condition := &v1.PodCondition{
				Type:    v1.PodScheduled,
				Status:  v1.ConditionFalse,
				Reason:  reasonUnschedulable,
				Message: msg,
			}

			if !podConditionHaveUpdate(&pod.Status, condition) {
				continue
			}
			podCopy := pod.DeepCopy()
			if !podutil.UpdatePodCondition(&podCopy.Status, condition) {
				continue
			}

			if sc.shouldThrottlePodStatusUpdate(podCopy, condition, false, pendingTaskCount) {
				continue
			}

			patchObj := struct {
				Status v1.PodStatus `json:"status"`
			}{Status: podCopy.Status}

			patchBytes, err := json.Marshal(patchObj)
			if err != nil {
				t.Fatalf("failed to marshal status patch: %v", err)
			}
			fullPodBytes, err := json.Marshal(podCopy)
			if err != nil {
				t.Fatalf("failed to marshal full pod object: %v", err)
			}

			sampledPatchBytes += int64(len(patchBytes))
			sampledFullPodBytes += int64(len(fullPodBytes))
			sampledWrites++

			sc.recordPodStatusSync(podCopy, condition)
			pod.Status = podCopy.Status
		}
	}

	if sampledWrites == 0 {
		t.Fatalf("expected sampled writes > 0")
	}

	totalRounds := int64(observationWindow / updateEvery)
	totalWrites := int64(podCount) * totalRounds
	avgPatchBytesPerWrite := sampledPatchBytes / sampledWrites
	avgFullPodBytesPerWrite := sampledFullPodBytes / sampledWrites
	estimatedPatchTrafficBytes := avgPatchBytesPerWrite * totalWrites
	estimatedEtcdStoredBytes := avgFullPodBytesPerWrite * totalWrites

	t.Logf("Estimated writes in %v: %d pods * %d rounds = %d", observationWindow, podCount, totalRounds, totalWrites)
	t.Logf("Average bytes/write: patch=%d, fullPod=%d", avgPatchBytesPerWrite, avgFullPodBytesPerWrite)
	t.Logf("Estimated API patch traffic increase: %.2f MiB", float64(estimatedPatchTrafficBytes)/1024.0/1024.0)
	t.Logf("Estimated etcd storage growth (full object MVCC approximation): %.2f GiB", float64(estimatedEtcdStoredBytes)/1024.0/1024.0/1024.0)
}

func TestEstimateEtcdStorageGrowthHighPressureWithMessageJitter(t *testing.T) {
	const (
		podCount              = 8000
		scheduleRatePerSecond = 500
		observationWindow     = 5 * time.Minute
		bytesPerWrite         = 100 * 1024
		targetUpperBoundBytes = int64(2 * 1024 * 1024 * 1024) // 2 GiB
	)

	// In round-robin retry, each pod is revisited every ~16s (8000/500).
	// Under high pressure throttling with a long interval, each pod should be
	// written at most a few times within 5 minutes.
	revisitInterval := time.Duration(podCount/scheduleRatePerSecond) * time.Second
	writesPerPod := int64(observationWindow/podStatusHighPressureInterval) + 1
	if revisitInterval > podStatusHighPressureInterval {
		// If revisit interval is already slower than throttle interval, throttling
		// effect is limited; this branch documents the bound conservatively.
		writesPerPod = int64(observationWindow/revisitInterval) + 1
	}

	totalWrites := int64(podCount) * writesPerPod
	estimatedEtcdGrowthBytes := totalWrites * bytesPerWrite

	t.Logf("Estimated writes under throttling: pods=%d, writesPerPod=%d, totalWrites=%d", podCount, writesPerPod, totalWrites)
	t.Logf("Estimated etcd growth with 100KB object size: %.2f GiB", float64(estimatedEtcdGrowthBytes)/1024.0/1024.0/1024.0)

	if estimatedEtcdGrowthBytes > targetUpperBoundBytes {
		t.Fatalf("estimated etcd growth exceeds target: got %.2f GiB, want <= %.2f GiB",
			float64(estimatedEtcdGrowthBytes)/1024.0/1024.0/1024.0,
			float64(targetUpperBoundBytes)/1024.0/1024.0/1024.0)
	}
}
