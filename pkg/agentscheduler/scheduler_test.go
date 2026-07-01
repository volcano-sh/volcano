package agentscheduler

import (
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	"sync"
	"testing"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	scheduleroptions "volcano.sh/volcano/cmd/scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	agentcache "volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	agentuthelper "volcano.sh/volcano/pkg/agentscheduler/uthelper"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

type noopAction struct{}

func (a *noopAction) Name() string                                                  { return "noop" }
func (a *noopAction) OnActionInit(_ []conf.Configuration)                           {}
func (a *noopAction) Initialize()                                                   {}
func (a *noopAction) Execute(_ *framework.Framework, _ *agentapi.SchedulingContext) {}
func (a *noopAction) UnInitialize()                                                 {}

type failingSnapshotCache struct {
	agentcache.Cache
}

func (c *failingSnapshotCache) UpdateSnapshot(_ *k8sutil.Snapshot) error {
	return errors.New("injected snapshot failure")
}

func TestConcurrentRunOnce(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode

	const workerCount = 8
	testFwk, err := agentuthelper.NewTestFramework(
		"test-scheduler",
		workerCount,
		[]framework.Action{&noopAction{}},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create test framework: %v", err)
	}
	defer testFwk.Close()

	for i := 0; i < workerCount; i++ {
		pod := util.BuildPod("default", fmt.Sprintf("pod-%d", i), "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
		pod.Spec.SchedulerName = "test-scheduler"
		task := schedulingapi.NewTaskInfo(pod)
		testFwk.MockCache.AddTaskInfo(task)
		testFwk.SchedulingQueue.Add(klog.Background(), pod)
	}

	var wg sync.WaitGroup
	panicCh := make(chan interface{}, workerCount)
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		worker := &Worker{
			framework: testFwk.Frameworks[i],
			index:     i,
		}
		go func(w *Worker) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			w.runOnce()
		}(worker)
	}
	wg.Wait()
	close(panicCh)
	for p := range panicCh {
		t.Fatalf("unexpected panic in runOnce: %v", p)
	}
}

func TestRunOnceCleansQueueWhenTaskMissing(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.SchedulerQueueingHints, true)
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode

	testFwk, err := agentuthelper.NewTestFramework(
		"test-scheduler",
		1,
		[]framework.Action{&noopAction{}},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create test framework: %v", err)
	}
	defer testFwk.Close()

	pod := util.BuildPod("default", "missing-task", "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
	pod.Spec.SchedulerName = "test-scheduler"
	testFwk.SchedulingQueue.Add(klog.Background(), pod)

	worker := &Worker{
		framework: testFwk.Frameworks[0],
		index:     0,
	}
	worker.runOnce()

	if pods := testFwk.SchedulingQueue.InFlightPods(); len(pods) != 0 {
		t.Fatalf("expected no in-flight pods after missing task, got %d", len(pods))
	}
}

func TestRunOnceRequeuesWhenSnapshotUpdateFails(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.SchedulerQueueingHints, true)
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode

	testFwk, err := agentuthelper.NewTestFramework(
		"test-scheduler",
		1,
		[]framework.Action{&noopAction{}},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create test framework: %v", err)
	}
	defer testFwk.Close()

	pod := util.BuildPod("default", "snapshot-failure", "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
	pod.Spec.SchedulerName = "test-scheduler"
	task := schedulingapi.NewTaskInfo(pod)
	testFwk.MockCache.AddTaskInfo(task)
	testFwk.SchedulingQueue.Add(klog.Background(), pod)

	testFwk.Frameworks[0].Cache = &failingSnapshotCache{Cache: testFwk.MockCache}
	worker := &Worker{
		framework: testFwk.Frameworks[0],
		index:     0,
	}
	worker.runOnce()

	if pods := testFwk.SchedulingQueue.InFlightPods(); len(pods) != 0 {
		t.Fatalf("expected no in-flight pods after snapshot failure, got %d", len(pods))
	}
	pendingPods, _ := testFwk.SchedulingQueue.PendingPods()
	if len(pendingPods) != 1 {
		t.Fatalf("expected pod to be requeued after snapshot failure, got %d pending pods", len(pendingPods))
	}
}
