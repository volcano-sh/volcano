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
	"testing"
	"time"
	"unsafe"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"

	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	scheduleroptions "volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/agentscheduler/metrics"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

// TestFramework wraps common test setup for agent scheduler tests.
type TestFramework struct {
	MockCache       *cache.SchedulerCache
	Frameworks      []*framework.Framework
	Actions         []framework.Action
	Cancel          context.CancelFunc
	SchedulingQueue k8sschedulingqueue.SchedulingQueue
}

// DefaultTiers returns the default plugin tiers used across agent scheduler tests.
func DefaultTiers() []conf.Tier {
	trueValue := true
	return []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
					Arguments: map[string]interface{}{
						"leastrequested.weight": 1,
						"mostrequested.weight":  0,
					},
				},
			},
		},
	}
}

// InitTestEnv registers plugins, initializes global options, and returns a cleanup
// function that restores original option values. Call it at the beginning of each test.
func InitTestEnv(t *testing.T) {
	t.Helper()

	// Register plugins (idempotent — safe to call multiple times).
	framework.RegisterPluginBuilder(predicates.PluginName, predicates.New)
	framework.RegisterPluginBuilder(nodeorder.PluginName, nodeorder.New)

	// Initialize options if nil.
	if options.ServerOpts == nil {
		options.ServerOpts = options.NewServerOption()
	}
	if scheduleroptions.ServerOpts == nil {
		scheduleroptions.ServerOpts = scheduleroptions.NewServerOption()
	}

	// Save and restore original values.
	origAgentShardingMode := options.ServerOpts.ShardingMode
	origAgentShardName := options.ServerOpts.ShardName
	origSchedShardingMode := scheduleroptions.ServerOpts.ShardingMode
	origSchedShardName := scheduleroptions.ServerOpts.ShardName
	origPercentage := scheduleroptions.ServerOpts.PercentageOfNodesToFind

	t.Cleanup(func() {
		options.ServerOpts.ShardingMode = origAgentShardingMode
		options.ServerOpts.ShardName = origAgentShardName
		scheduleroptions.ServerOpts.ShardingMode = origSchedShardingMode
		scheduleroptions.ServerOpts.ShardName = origSchedShardName
		scheduleroptions.ServerOpts.PercentageOfNodesToFind = origPercentage
	})

	scheduleroptions.ServerOpts.PercentageOfNodesToFind = 100
}

// NewTestFramework creates a new test framework with mock cache and scheduling queue.
func NewTestFramework(schedulerName string, workerCount int, actions []framework.Action, tiers []conf.Tier, configurations []conf.Configuration) (*TestFramework, error) {
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

	for _, action := range actions {
		action.OnActionInit(configurations)
	}

	schedulingQueue.Run(klog.Background())

	frameworks := make([]*framework.Framework, workerCount)
	for i := 0; i < workerCount; i++ {
		frameworks[i] = framework.NewFramework(actions, tiers, mockCache, configurations)
	}

	if workerCount > 0 {
		snapshot := frameworks[0].GetSnapshot()
		if err := mockCache.UpdateSnapshot(snapshot); err != nil {
			cancel()
			return nil, err
		}
	}

	return &TestFramework{
		MockCache:       mockCache,
		Frameworks:      frameworks,
		Actions:         actions,
		Cancel:          cancel,
		SchedulingQueue: schedulingQueue,
	}, nil
}

// Close cleans up the test framework.
func (tf *TestFramework) Close() {
	if tf.SchedulingQueue != nil {
		tf.SchedulingQueue.Close()
	}
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
