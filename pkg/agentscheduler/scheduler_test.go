package agentscheduler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	scheduleroptions "volcano.sh/volcano/cmd/scheduler/app/options"
	_ "volcano.sh/volcano/pkg/agentscheduler/actions"
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

type testCustomAction struct {
	name           string
	mu             sync.Mutex
	configurations []conf.Configuration
	executedCount  int
}

func (a *testCustomAction) Name() string {
	return a.name
}

func (a *testCustomAction) OnActionInit(configurations []conf.Configuration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.configurations = configurations
}

func (a *testCustomAction) Initialize() {}

func (a *testCustomAction) Execute(_ *framework.Framework, _ *agentapi.SchedulingContext) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.executedCount++
}

func (a *testCustomAction) UnInitialize() {}

func TestWorkerFrameworkHotReload(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode

	var actionAInstances []*testCustomAction
	var actionBInstances []*testCustomAction
	var instMu sync.Mutex

	framework.RegisterActionBuilder("test-action-a", func() framework.Action {
		inst := &testCustomAction{name: "test-action-a"}
		instMu.Lock()
		actionAInstances = append(actionAInstances, inst)
		instMu.Unlock()
		return inst
	})
	framework.RegisterActionBuilder("test-action-b", func() framework.Action {
		inst := &testCustomAction{name: "test-action-b"}
		instMu.Lock()
		actionBInstances = append(actionBInstances, inst)
		instMu.Unlock()
		return inst
	})

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

	confDir := t.TempDir()
	confFile := filepath.Join(confDir, "agentscheduler.conf")

	confA := `
actions: "test-action-a"
tiers:
- plugins:
  - name: predicates
configurations:
- name: test-action-a
  arguments:
    testKey: valueA
`
	if err := os.WriteFile(confFile, []byte(confA), 0644); err != nil {
		t.Fatalf("failed to write confA: %v", err)
	}

	sched := &Scheduler{
		schedulerConf: confFile,
		cache:         testFwk.MockCache,
		workerCount:   1,
	}
	sched.loadSchedulerConf()

	snap := sched.getConfSnapshot()
	if snap == nil {
		t.Fatalf("expected initial configSnapshot, got nil")
	}
	initialVersion := snap.version
	if len(snap.actions) != 1 || snap.actions[0].Name() != "test-action-a" {
		t.Fatalf("expected action 'test-action-a', got %v", snap.actions)
	}

	worker := &Worker{
		index:       0,
		sched:       sched,
		confVersion: snap.version,
		framework:   framework.NewFramework(snap.actions, snap.tiers, testFwk.MockCache, snap.configurations),
	}

	// Verify worker initial framework configuration
	if len(worker.framework.Actions) != 1 || worker.framework.Actions[0].Name() != "test-action-a" {
		t.Fatalf("expected initial worker action 'test-action-a', got %v", worker.framework.Actions)
	}
	if len(worker.framework.Tiers) != 1 || len(worker.framework.Tiers[0].Plugins) != 1 || worker.framework.Tiers[0].Plugins[0].Name != "predicates" {
		t.Fatalf("expected initial worker plugin 'predicates', got %v", worker.framework.Tiers)
	}
	if len(worker.framework.Configurations) != 1 || worker.framework.Configurations[0].Arguments["testKey"] != "valueA" {
		t.Fatalf("expected initial argument testKey=valueA, got %v", worker.framework.Configurations)
	}

	// Schedule first pod under Config A
	podA := util.BuildPod("default", "pod-a", "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
	podA.Spec.SchedulerName = "test-scheduler"
	taskA := schedulingapi.NewTaskInfo(podA)
	testFwk.MockCache.AddTaskInfo(taskA)
	testFwk.SchedulingQueue.Add(klog.Background(), podA)

	worker.runOnce()

	instMu.Lock()
	if len(actionAInstances) == 0 || actionAInstances[len(actionAInstances)-1].executedCount != 1 {
		t.Fatalf("expected test-action-a executed count 1, got %v", actionAInstances)
	}
	if len(actionBInstances) != 0 {
		t.Fatalf("expected test-action-b not executed, got %d instances", len(actionBInstances))
	}
	instMu.Unlock()

	// Hot reload to Config B
	confB := `
actions: "test-action-b"
tiers:
- plugins:
  - name: nodeorder
configurations:
- name: test-action-b
  arguments:
    testKey: valueB
`
	if err := os.WriteFile(confFile, []byte(confB), 0644); err != nil {
		t.Fatalf("failed to write confB: %v", err)
	}

	sched.loadSchedulerConf()

	snapB := sched.getConfSnapshot()
	if snapB == nil || snapB.version != initialVersion+1 {
		t.Fatalf("expected reloaded configSnapshot version %d, got %+v", initialVersion+1, snapB)
	}

	// Prior to next worker cycle, worker.framework retains Config A (not mutated mid-stream)
	if worker.framework.Actions[0].Name() != "test-action-a" {
		t.Fatalf("expected worker framework to retain test-action-a before next cycle, got %v", worker.framework.Actions[0].Name())
	}

	// Schedule second pod under Config B
	podB := util.BuildPod("default", "pod-b", "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
	podB.Spec.SchedulerName = "test-scheduler"
	taskB := schedulingapi.NewTaskInfo(podB)
	testFwk.MockCache.AddTaskInfo(taskB)
	testFwk.SchedulingQueue.Add(klog.Background(), podB)

	worker.runOnce()

	// Verify worker Framework transitioned to Config B at cycle boundary
	if worker.confVersion != initialVersion+1 {
		t.Fatalf("expected worker confVersion %d, got %d", initialVersion+1, worker.confVersion)
	}
	if len(worker.framework.Actions) != 1 || worker.framework.Actions[0].Name() != "test-action-b" {
		t.Fatalf("expected worker action 'test-action-b' after reload, got %v", worker.framework.Actions)
	}
	if len(worker.framework.Tiers) != 1 || len(worker.framework.Tiers[0].Plugins) != 1 || worker.framework.Tiers[0].Plugins[0].Name != "nodeorder" {
		t.Fatalf("expected worker plugin 'nodeorder' after reload, got %v", worker.framework.Tiers)
	}
	if len(worker.framework.Configurations) != 1 || worker.framework.Configurations[0].Arguments["testKey"] != "valueB" {
		t.Fatalf("expected reloaded argument testKey=valueB, got %v", worker.framework.Configurations)
	}

	instMu.Lock()
	if len(actionBInstances) == 0 || actionBInstances[len(actionBInstances)-1].executedCount != 1 {
		t.Fatalf("expected test-action-b executed count 1, got %v", actionBInstances)
	}
	// Action A count should remain 1 (removed action not executed again)
	if actionAInstances[len(actionAInstances)-1].executedCount != 1 {
		t.Fatalf("expected test-action-a executed count to remain 1, got %d", actionAInstances[len(actionAInstances)-1].executedCount)
	}
	instMu.Unlock()

	// Test reload failure: invalid config leaves old config active
	invalidConf := `actions: [invalid`
	if err := os.WriteFile(confFile, []byte(invalidConf), 0644); err != nil {
		t.Fatalf("failed to write invalidConf: %v", err)
	}
	sched.loadSchedulerConf()

	snapAfterInvalid := sched.getConfSnapshot()
	if snapAfterInvalid.version != initialVersion+1 {
		t.Fatalf("expected version to remain %d after failed reload, got %d", initialVersion+1, snapAfterInvalid.version)
	}

	podC := util.BuildPod("default", "pod-c", "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
	podC.Spec.SchedulerName = "test-scheduler"
	taskC := schedulingapi.NewTaskInfo(podC)
	testFwk.MockCache.AddTaskInfo(taskC)
	testFwk.SchedulingQueue.Add(klog.Background(), podC)

	worker.runOnce()

	if worker.framework.Actions[0].Name() != "test-action-b" {
		t.Fatalf("expected worker framework to retain test-action-b after failed reload, got %v", worker.framework.Actions[0].Name())
	}
}

func TestWorkerFrameworkHotReloadConcurrent(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode

	const workerCount = 4
	const podCount = 20

	framework.RegisterActionBuilder("test-concurrent-action-a", func() framework.Action {
		return &noopAction{}
	})
	framework.RegisterActionBuilder("test-concurrent-action-b", func() framework.Action {
		return &noopAction{}
	})

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

	confDir := t.TempDir()
	confFile := filepath.Join(confDir, "agentscheduler.conf")

	confA := `
actions: "test-concurrent-action-a"
tiers:
- plugins:
  - name: predicates
`
	confB := `
actions: "test-concurrent-action-b"
tiers:
- plugins:
  - name: nodeorder
`
	if err := os.WriteFile(confFile, []byte(confA), 0644); err != nil {
		t.Fatalf("failed to write conf: %v", err)
	}

	sched := &Scheduler{
		schedulerConf: confFile,
		cache:         testFwk.MockCache,
		workerCount:   workerCount,
	}
	sched.loadSchedulerConf()

	snap := sched.getConfSnapshot()
	workers := make([]*Worker, workerCount)
	for i := 0; i < workerCount; i++ {
		workers[i] = &Worker{
			index:       i,
			sched:       sched,
			confVersion: snap.version,
			framework:   framework.NewFramework(snap.actions, snap.tiers, testFwk.MockCache, snap.configurations),
		}
	}

	for i := 0; i < podCount; i++ {
		pod := util.BuildPod("default", fmt.Sprintf("concurrent-pod-%d", i), "", v1.PodPending, v1.ResourceList{}, "", map[string]string{}, map[string]string{})
		pod.Spec.SchedulerName = "test-scheduler"
		task := schedulingapi.NewTaskInfo(pod)
		testFwk.MockCache.AddTaskInfo(task)
		testFwk.SchedulingQueue.Add(klog.Background(), pod)
	}

	stopCh := make(chan struct{})
	var reloadWg sync.WaitGroup
	var workerWg sync.WaitGroup

	// Reload goroutine repeatedly reloading config
	reloadWg.Add(1)
	go func() {
		defer reloadWg.Done()
		toggle := false
		for {
			select {
			case <-stopCh:
				return
			default:
				if toggle {
					_ = os.WriteFile(confFile, []byte(confA), 0644)
				} else {
					_ = os.WriteFile(confFile, []byte(confB), 0644)
				}
				toggle = !toggle
				sched.loadSchedulerConf()
			}
		}
	}()

	// Workers running runOnce concurrently
	for i := 0; i < workerCount; i++ {
		workerWg.Add(1)
		go func(w *Worker) {
			defer workerWg.Done()
			for j := 0; j < podCount/workerCount; j++ {
				w.runOnce()
			}
		}(workers[i])
	}

	workerWg.Wait()
	close(stopCh)
	reloadWg.Wait()
}
