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

package uthelper

import (
	"context"
	"reflect"
	"time"
	"unsafe"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"

	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"

	"volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/agentscheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

// TestFramework wraps common test setup for agent scheduler tests.
type TestFramework struct {
	MockCache       *cache.SchedulerCache
	Framework       *framework.Framework
	Action          framework.Action
	Cancel          context.CancelFunc
	SchedulingQueue k8sschedulingqueue.SchedulingQueue
}

// NewTestFramework creates a new test framework with mock cache and scheduling queue.
func NewTestFramework(schedulerName string, actions []framework.Action, tiers []conf.Tier, configurations []conf.Configuration) (*TestFramework, error) {
	// Initialize metrics.
	metrics.InitKubeSchedulerRelatedMetrics()

	// Create mock cache.
	mockCache := cache.NewDefaultMockSchedulerCache(schedulerName)

	// Initialize scheduling queue.
	ctx, cancel := context.WithCancel(context.Background())
	metricsRecorder := k8smetrics.NewMetricsAsyncRecorder(1000, time.Second, ctx.Done())
	queueingHintMapPerProfile := make(k8sschedulingqueue.QueueingHintMapPerProfile)
	queueingHintMap := make(k8sschedulingqueue.QueueingHintMap)
	defaultQueueingHintFn := func(_ klog.Logger, _ *v1.Pod, _, _ interface{}) (fwk.QueueingHint, error) {
		return fwk.Queue, nil
	}
	wildCardEvent := fwk.ClusterEvent{Resource: fwk.WildCard, ActionType: fwk.All}
	queueingHintMap[wildCardEvent] = append(queueingHintMap[wildCardEvent], &k8sschedulingqueue.QueueingHintFunction{
		QueueingHintFn: defaultQueueingHintFn,
	})
	queueingHintMapPerProfile[schedulerName] = queueingHintMap
	schedulingQueue := k8sschedulingqueue.NewSchedulingQueue(
		cache.Less,
		mockCache.SharedInformerFactory(),
		k8sschedulingqueue.WithMetricsRecorder(metricsRecorder),
		k8sschedulingqueue.WithQueueingHintMapPerProfile(queueingHintMapPerProfile),
	)

	// Set schedulingQueue using unsafe pointer since it's unexported.
	rv := reflect.ValueOf(mockCache).Elem()
	schedulingQueueField := rv.FieldByName("schedulingQueue")
	if !schedulingQueueField.IsValid() {
		cancel()
		return nil, &reflect.ValueError{Method: "schedulingQueue", Kind: reflect.Invalid}
	}
	fieldAddr := unsafe.Pointer(schedulingQueueField.UnsafeAddr())
	*(*k8sschedulingqueue.SchedulingQueue)(fieldAddr) = schedulingQueue
	mockCache.ConflictAwareBinder = cache.NewConflictAwareBinder(mockCache, schedulingQueue)

	// Create framework.
	var action framework.Action
	if len(actions) > 0 {
		action = actions[0]
		action.OnActionInit(configurations)
	}
	fwk := framework.NewFramework(actions, tiers, mockCache, configurations)

	// Update snapshot.
	snapshot := fwk.GetSnapshot()
	if err := mockCache.UpdateSnapshot(snapshot); err != nil {
		cancel()
		return nil, err
	}

	return &TestFramework{
		MockCache:       mockCache,
		Framework:       fwk,
		Action:          action,
		Cancel:          cancel,
		SchedulingQueue: schedulingQueue,
	}, nil
}

// Close cleans up the test framework.
func (tf *TestFramework) Close() {
	if tf.Cancel != nil {
		tf.Cancel()
	}
}

// VerifySchedulingResult verifies the scheduling result from BindCheckChannel.
func VerifySchedulingResult(t interface {
	Fatalf(format string, args ...interface{})
}, mockCache *cache.SchedulerCache, expectedNode string) {
	select {
	case result := <-mockCache.ConflictAwareBinder.BindCheckChannel:
		if result == nil {
			t.Fatalf("Expected schedule result, got nil")
			return
		}
		if len(result.SuggestedNodes) == 0 {
			t.Fatalf("Expected at least one suggested node, got 0")
			return
		}
		if result.SuggestedNodes[0].Name != expectedNode {
			t.Fatalf("Expected node %s, got %s", expectedNode, result.SuggestedNodes[0].Name)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Timeout waiting for schedule result in BindCheckChannel")
	}
}

// CreateSchedulingContext creates a QueuedPodInfo for testing.
func CreateSchedulingContext(pod *v1.Pod, nodesInShard sets.Set[string]) (*k8sframework.QueuedPodInfo, error) {
	podInfo, err := k8sframework.NewPodInfo(pod)
	if err != nil {
		return nil, err
	}
	return &k8sframework.QueuedPodInfo{PodInfo: podInfo}, nil
}
