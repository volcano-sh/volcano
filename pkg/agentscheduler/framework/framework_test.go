package framework

import (
	"errors"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	agentcache "volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	volcanofwk "volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type pluginState struct {
	initCount       int
	cycleStart      int
	cycleEnd        int
	prePredicateErr error
	predicateErr    error
	nodeScore       float64
	batchNodeScores map[string]float64
}

type testPlugin struct {
	name  string
	state *pluginState
}

func (p *testPlugin) Name() string { return p.name }

func (p *testPlugin) OnPluginInit(fwk *Framework) {
	p.state.initCount++

	fwk.AddPrePredicateFn(p.name, func(*api.TaskInfo) error {
		return p.state.prePredicateErr
	})
	fwk.AddPredicateFn(p.name, func(*api.TaskInfo, *api.NodeInfo) error {
		return p.state.predicateErr
	})
	fwk.AddNodeOrderFn(p.name, func(*api.TaskInfo, *api.NodeInfo) (float64, error) {
		return p.state.nodeScore, nil
	})
	fwk.AddBatchNodeOrderFn(p.name, func(*api.TaskInfo, []*api.NodeInfo) (map[string]float64, error) {
		return p.state.batchNodeScores, nil
	})
}

func (p *testPlugin) OnCycleStart(*Framework) { p.state.cycleStart++ }
func (p *testPlugin) OnCycleEnd(*Framework)   { p.state.cycleEnd++ }

func TestNewFrameworkRegistersPluginsAndInvokesHooks(t *testing.T) {
	cache := agentcache.NewDefaultMockSchedulerCache("test-scheduler")

	pluginName := "framework-test-plugin"
	state := &pluginState{
		nodeScore: 3.5,
		batchNodeScores: map[string]float64{
			"node-1": 3.5,
			"node-2": 1.5,
		},
	}
	RegisterPluginBuilder(pluginName, func(volcanofwk.Arguments) Plugin {
		return &testPlugin{name: pluginName, state: state}
	})

	trueValue := true
	fwk := NewFramework(nil, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             pluginName,
					EnabledPredicate: &trueValue,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}, cache, nil)

	if state.initCount != 1 {
		t.Fatalf("expected OnPluginInit to run once, got %d", state.initCount)
	}
	if _, found := fwk.Plugins[pluginName]; !found {
		t.Fatalf("expected plugin %q to be registered in framework", pluginName)
	}

	task := api.NewTaskInfo(util.BuildPod("default", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "", map[string]string{}, map[string]string{}))
	node1 := api.NewNodeInfo(util.BuildNode("node-1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{}))
	node2 := api.NewNodeInfo(util.BuildNode("node-2", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{}))

	if err := fwk.PrePredicateFn(task); err != nil {
		t.Fatalf("PrePredicateFn returned unexpected error: %v", err)
	}
	if err := fwk.PredicateFn(task, node1); err != nil {
		t.Fatalf("PredicateFn returned unexpected error: %v", err)
	}
	score, err := fwk.NodeOrderFn(task, node1)
	if err != nil {
		t.Fatalf("NodeOrderFn returned unexpected error: %v", err)
	}
	if score != state.nodeScore {
		t.Fatalf("expected node score %.1f, got %.1f", state.nodeScore, score)
	}
	batchScores, err := fwk.BatchNodeOrderFn(task, []*api.NodeInfo{node1, node2})
	if err != nil {
		t.Fatalf("BatchNodeOrderFn returned unexpected error: %v", err)
	}
	if batchScores["node-1"] != 3.5 || batchScores["node-2"] != 1.5 {
		t.Fatalf("unexpected batch scores: %+v", batchScores)
	}

	fwk.OnCycleStart()
	fwk.OnCycleEnd()
	if state.cycleStart != 1 || state.cycleEnd != 1 {
		t.Fatalf("expected cycle hooks to run once, got start=%d end=%d", state.cycleStart, state.cycleEnd)
	}

	firstState := fwk.GetCycleState(task.Pod.UID)
	secondState := fwk.GetCycleState(task.Pod.UID)
	if firstState != secondState {
		t.Fatal("expected GetCycleState to reuse current cycle state")
	}
	fwk.ClearCycleState()
	thirdState := fwk.GetCycleState(task.Pod.UID)
	if thirdState == firstState {
		t.Fatal("expected ClearCycleState to force new cycle state allocation")
	}
}

func TestFrameworkFailedFunctionTracking(t *testing.T) {
	cache := agentcache.NewDefaultMockSchedulerCache("test-scheduler")

	pluginName := "framework-failure-plugin"
	state := &pluginState{
		prePredicateErr: errors.New("pre-predicate failed"),
		predicateErr:    errors.New("predicate failed"),
	}
	RegisterPluginBuilder(pluginName, func(volcanofwk.Arguments) Plugin {
		return &testPlugin{name: pluginName, state: state}
	})

	trueValue := true
	fwk := NewFramework(nil, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             pluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}, cache, nil)

	task := api.NewTaskInfo(util.BuildPod("default", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "", map[string]string{}, map[string]string{}))
	node := api.NewNodeInfo(util.BuildNode("node-1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{}))

	if err := fwk.PrePredicateFn(task); err == nil || err.Error() != state.prePredicateErr.Error() {
		t.Fatalf("expected pre-predicate error %q, got %v", state.prePredicateErr, err)
	}
	if !fwk.PrePredicateFailedFns.Has(pluginName) {
		t.Fatalf("expected pre-predicate failed set to contain %q", pluginName)
	}

	state.prePredicateErr = nil
	if err := fwk.PredicateFn(task, node); err == nil || err.Error() != state.predicateErr.Error() {
		t.Fatalf("expected predicate error %q, got %v", state.predicateErr, err)
	}
	if !fwk.PredicateFailedFns.Has(pluginName) {
		t.Fatalf("expected predicate failed set to contain %q", pluginName)
	}

	fwk.UpdateFailedFns(fwk.PredicateFailedFns, "manual")
	if !fwk.PredicateFailedFns.Equal(sets.New(pluginName, "manual")) {
		t.Fatalf("unexpected predicate failed set: %v", fwk.PredicateFailedFns.UnsortedList())
	}
}

func TestNodeOrderAggregatesAcrossPlugins(t *testing.T) {
	cache := agentcache.NewDefaultMockSchedulerCache("test-scheduler")

	trueValue := true
	var tiers []conf.Tier
	var expectedTotal float64
	for i, score := range []float64{2, 5} {
		name := fmt.Sprintf("framework-order-plugin-%d", i)
		state := &pluginState{
			nodeScore: score,
			batchNodeScores: map[string]float64{
				"node-1": score,
			},
		}
		expectedTotal += score
		RegisterPluginBuilder(name, func(s *pluginState) func(volcanofwk.Arguments) Plugin {
			return func(volcanofwk.Arguments) Plugin {
				return &testPlugin{name: name, state: s}
			}
		}(state))
		tiers = append(tiers, conf.Tier{
			Plugins: []conf.PluginOption{
				{
					Name:             name,
					EnabledNodeOrder: &trueValue,
				},
			},
		})
	}

	fwk := NewFramework(nil, tiers, cache, nil)
	task := api.NewTaskInfo(util.BuildPod("default", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "", map[string]string{}, map[string]string{}))
	node := api.NewNodeInfo(util.BuildNode("node-1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{}))

	score, err := fwk.NodeOrderFn(task, node)
	if err != nil {
		t.Fatalf("NodeOrderFn returned unexpected error: %v", err)
	}
	if score != expectedTotal {
		t.Fatalf("expected aggregated node score %.1f, got %.1f", expectedTotal, score)
	}

	batchScores, err := fwk.BatchNodeOrderFn(task, []*api.NodeInfo{node})
	if err != nil {
		t.Fatalf("BatchNodeOrderFn returned unexpected error: %v", err)
	}
	if batchScores["node-1"] != expectedTotal {
		t.Fatalf("expected aggregated batch score %.1f, got %.1f", expectedTotal, batchScores["node-1"])
	}
}
