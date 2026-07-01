package agentscheduler

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	scheduleroptions "volcano.sh/volcano/cmd/scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	agentcache "volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/predicates"
	agentuthelper "volcano.sh/volcano/pkg/agentscheduler/uthelper"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

type noopAction struct{}

func (a *noopAction) Name() string                                                  { return "noop" }
func (a *noopAction) OnActionInit(_ []conf.Configuration)                           {}
func (a *noopAction) Initialize()                                                   {}
func (a *noopAction) Execute(_ *framework.Framework, _ *agentapi.SchedulingContext) {}
func (a *noopAction) UnInitialize()                                                 {}

type initCountingAction struct {
	name      string
	initCount atomic.Int32
}

func (a *initCountingAction) Name() string { return a.name }
func (a *initCountingAction) OnActionInit(_ []conf.Configuration) {
	a.initCount.Add(1)
}
func (a *initCountingAction) Initialize()                                                   {}
func (a *initCountingAction) Execute(_ *framework.Framework, _ *agentapi.SchedulingContext) {}
func (a *initCountingAction) UnInitialize()                                                 {}

func TestLoadSchedulerConfInitializesDefaultConfig(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	action := &initCountingAction{name: "test-default"}
	framework.RegisterAction(action)

	originalDefaultSchedulerConf := DefaultSchedulerConf
	DefaultSchedulerConf = `
actions: "test-default"
tiers:
- plugins:
  - name: predicates
`
	t.Cleanup(func() {
		DefaultSchedulerConf = originalDefaultSchedulerConf
	})

	sched := &Scheduler{
		cache: agentcache.NewDefaultMockSchedulerCache("test-scheduler"),
	}
	sched.loadSchedulerConf()

	if action.initCount.Load() != 1 {
		t.Fatalf("expected default config action to be initialized once, got %d", action.initCount.Load())
	}
}

func TestLoadSchedulerConfInitializesDefaultConfigWhenConfigFileIsMissing(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	action := &initCountingAction{name: "test-default-missing-file"}
	framework.RegisterAction(action)

	originalDefaultSchedulerConf := DefaultSchedulerConf
	DefaultSchedulerConf = `
actions: "test-default-missing-file"
tiers:
- plugins:
  - name: predicates
`
	t.Cleanup(func() {
		DefaultSchedulerConf = originalDefaultSchedulerConf
	})

	sched := &Scheduler{
		cache:         agentcache.NewDefaultMockSchedulerCache("test-scheduler"),
		schedulerConf: filepath.Join(t.TempDir(), "missing.conf"),
	}
	sched.loadSchedulerConf()

	if action.initCount.Load() != 1 {
		t.Fatalf("expected fallback default config action to be initialized once, got %d", action.initCount.Load())
	}
}

func TestLoadSchedulerConfRefreshesWorkerFramework(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	framework.RegisterAction(&noopAction{})

	confPath := filepath.Join(t.TempDir(), "agent-scheduler.conf")
	writeSchedulerConf(t, confPath, `
actions: "noop"
tiers:
- plugins:
  - name: predicates
  - name: nodeorder
`)

	mockCache := agentcache.NewDefaultMockSchedulerCache("test-scheduler")
	sched := &Scheduler{
		cache:              mockCache,
		schedulerConf:      confPath,
		disableDefaultConf: true,
	}
	sched.loadSchedulerConf()

	worker := sched.newWorker(0)
	sched.addWorker(worker)
	oldFramework := worker.framework

	if enabled := predicateEnabled(t, oldFramework); enabled == nil || !*enabled {
		t.Fatalf("expected predicates to be enabled before reload, got %v", enabled)
	}

	writeSchedulerConf(t, confPath, `
actions: "noop"
tiers:
- plugins:
  - name: predicates
    enablePredicate: false
  - name: nodeorder
`)
	sched.loadSchedulerConf()

	worker.mutex.RLock()
	newFramework := worker.framework
	worker.mutex.RUnlock()

	if newFramework == oldFramework {
		t.Fatal("expected worker framework to be replaced after scheduler config reload")
	}
	if enabled := predicateEnabled(t, newFramework); enabled == nil || *enabled {
		t.Fatalf("expected predicates to be disabled after reload, got %v", enabled)
	}
}

func TestLoadSchedulerConfDoesNotWaitForIdleWorkerPop(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	framework.RegisterAction(&noopAction{})

	confPath := filepath.Join(t.TempDir(), "agent-scheduler.conf")
	writeSchedulerConf(t, confPath, `
actions: "noop"
tiers:
- plugins:
  - name: predicates
`)

	testFwk, err := agentuthelper.NewTestFramework(
		"test-scheduler",
		1,
		[]framework.Action{&noopAction{}},
		agentuthelper.DefaultTiers(),
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create test framework: %v", err)
	}
	defer testFwk.Close()

	sched := &Scheduler{
		cache:              testFwk.MockCache,
		schedulerConf:      confPath,
		disableDefaultConf: true,
	}
	sched.loadSchedulerConf()
	worker := sched.newWorker(0)
	sched.addWorker(worker)

	runOnceStarted := make(chan struct{})
	go func() {
		close(runOnceStarted)
		worker.runOnce()
	}()
	<-runOnceStarted

	reloadDone := make(chan struct{})
	go func() {
		sched.loadSchedulerConf()
		close(reloadDone)
	}()

	select {
	case <-reloadDone:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler config reload blocked while worker was waiting for a pod")
	}
}

func writeSchedulerConf(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write scheduler config: %v", err)
	}
}

func predicateEnabled(t *testing.T, fwk *framework.Framework) *bool {
	t.Helper()
	for _, tier := range fwk.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name == predicates.PluginName {
				return plugin.EnabledPredicate
			}
		}
	}
	t.Fatalf("failed to find %s plugin", predicates.PluginName)
	return nil
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
