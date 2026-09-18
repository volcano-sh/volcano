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

package nodescore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type scoreCallTracker struct {
	mu                       sync.Mutex
	preScoreCalls            map[string]int
	scoreCalls               map[string]map[string]int
	normalizeCalls           map[string]int
	totalPreScoreCalls       int
	totalScoreCalls          int
	expectedPreScoreCalls    int
	expectedTotalScoreCalls  int
	scoreBeforePreScoreDone  bool
	normalizeBeforeScoreDone bool
}

func newScoreCallTracker(expectedPreScoreCalls, expectedTotalScoreCalls int) *scoreCallTracker {
	return &scoreCallTracker{
		preScoreCalls:           map[string]int{},
		scoreCalls:              map[string]map[string]int{},
		normalizeCalls:          map[string]int{},
		expectedPreScoreCalls:   expectedPreScoreCalls,
		expectedTotalScoreCalls: expectedTotalScoreCalls,
	}
}

func (t *scoreCallTracker) recordPreScore(plugin string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.preScoreCalls[plugin]++
	t.totalPreScoreCalls++
}

func (t *scoreCallTracker) recordScore(plugin, node string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.totalPreScoreCalls != t.expectedPreScoreCalls {
		t.scoreBeforePreScoreDone = true
	}
	if t.scoreCalls[plugin] == nil {
		t.scoreCalls[plugin] = map[string]int{}
	}
	t.scoreCalls[plugin][node]++
	t.totalScoreCalls++
}

func (t *scoreCallTracker) recordNormalize(plugin string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.totalScoreCalls != t.expectedTotalScoreCalls {
		t.normalizeBeforeScoreDone = true
	}
	t.normalizeCalls[plugin]++
}

type fakeScorePlugin struct {
	name        string
	scores      map[string]int64
	scoreStatus map[string]*fwk.Status
	extensions  *fakeScoreExtensions
	tracker     *scoreCallTracker
}

func (p *fakeScorePlugin) Name() string {
	return p.name
}

func (p *fakeScorePlugin) Score(_ context.Context, _ fwk.CycleState, _ *v1.Pod, nodeInfo fwk.NodeInfo) (int64, *fwk.Status) {
	nodeName := nodeInfo.Node().Name
	if p.tracker != nil {
		p.tracker.recordScore(p.name, nodeName)
	}
	return p.scores[nodeName], p.scoreStatus[nodeName]
}

func (p *fakeScorePlugin) ScoreExtensions() fwk.ScoreExtensions {
	if p.extensions == nil {
		return nil
	}
	return p.extensions
}

type fakePreScorePlugin struct {
	*fakeScorePlugin
	preStatus *fwk.Status
}

func (p *fakePreScorePlugin) PreScore(_ context.Context, _ fwk.CycleState, _ *v1.Pod, _ []fwk.NodeInfo) *fwk.Status {
	if p.tracker != nil {
		p.tracker.recordPreScore(p.name)
	}
	return p.preStatus
}

type fakeScoreExtensions struct {
	plugin    string
	normalize func(fwk.NodeScoreList)
	status    *fwk.Status
	tracker   *scoreCallTracker
}

func (e *fakeScoreExtensions) NormalizeScore(_ context.Context, _ fwk.CycleState, _ *v1.Pod, scores fwk.NodeScoreList) *fwk.Status {
	if e.tracker != nil {
		e.tracker.recordNormalize(e.plugin)
	}
	if e.normalize != nil {
		e.normalize(scores)
	}
	return e.status
}

func TestNodeInfosForCandidateNodes(t *testing.T) {
	nodeA := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}
	nodeB := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}}
	nodeC := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-c"}}
	nodeD := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-d"}}

	k8sNodeInfoA := k8sframework.NewNodeInfo()
	k8sNodeInfoA.SetNode(nodeA)
	k8sNodeInfoB := k8sframework.NewNodeInfo()
	k8sNodeInfoB.SetNode(nodeB)
	k8sNodeInfoC := k8sframework.NewNodeInfo()
	k8sNodeInfoC.SetNode(nodeC)

	got := NodeInfosForCandidateNodes(
		[]*api.NodeInfo{api.NewNodeInfo(nodeA), nil, api.NewNodeInfo(nodeD), api.NewNodeInfo(nodeC)},
		map[string]fwk.NodeInfo{
			"node-a": k8sNodeInfoA,
			"node-b": k8sNodeInfoB,
			"node-c": k8sNodeInfoC,
		},
	)

	if len(got) != 2 {
		t.Fatalf("expected 2 candidate node infos, got %d", len(got))
	}
	if got[0].Node().Name != "node-a" {
		t.Fatalf("expected first node to be node-a, got %s", got[0].Node().Name)
	}
	if got[1].Node().Name != "node-c" {
		t.Fatalf("expected second node to be node-c, got %s", got[1].Node().Name)
	}
}

func TestRunScorePluginsAggregatesNormalizedWeightedScores(t *testing.T) {
	tracker := newScoreCallTracker(2, 4)
	plugin1 := &fakePreScorePlugin{fakeScorePlugin: &fakeScorePlugin{
		name:    "plugin-1",
		scores:  map[string]int64{"node-a": 10, "node-b": 20},
		tracker: tracker,
	}}
	plugin2 := &fakePreScorePlugin{fakeScorePlugin: &fakeScorePlugin{
		name:    "plugin-2",
		scores:  map[string]int64{"node-a": 80, "node-b": 40},
		tracker: tracker,
	}}
	plugin1.extensions = &fakeScoreExtensions{plugin: plugin1.name, tracker: tracker}
	plugin2.extensions = &fakeScoreExtensions{
		plugin:  plugin2.name,
		tracker: tracker,
		normalize: func(scores fwk.NodeScoreList) {
			for i := range scores {
				switch scores[i].Name {
				case "node-a":
					scores[i].Score = 20
				case "node-b":
					scores[i].Score = 60
				}
			}
		},
	}

	got, err := RunScorePlugins(
		context.TODO(),
		[]PreScorePluginSpec{
			{Name: plugin1.name, Plugin: plugin1},
			{Name: plugin2.name, Plugin: plugin2},
		},
		[]ScorePluginSpec{
			{Name: plugin1.name, Plugin: plugin1, Weight: 2},
			{Name: plugin2.name, Plugin: plugin2, Weight: 3},
		},
		k8sframework.NewCycleState(),
		&v1.Pod{},
		testNodeInfos("node-a", "node-b"),
	)
	if err != nil {
		t.Fatalf("RunScorePlugins returned an error: %v", err)
	}
	want := map[string]float64{"node-a": 80, "node-b": 220}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected scores: got %v, want %v", got, want)
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.scoreBeforePreScoreDone {
		t.Error("Score ran before all PreScore plugins completed")
	}
	if tracker.normalizeBeforeScoreDone {
		t.Error("NormalizeScore ran before all Score calls completed")
	}
	for _, plugin := range []string{plugin1.name, plugin2.name} {
		if tracker.preScoreCalls[plugin] != 1 {
			t.Errorf("plugin %s PreScore calls: got %d, want 1", plugin, tracker.preScoreCalls[plugin])
		}
		if tracker.normalizeCalls[plugin] != 1 {
			t.Errorf("plugin %s NormalizeScore calls: got %d, want 1", plugin, tracker.normalizeCalls[plugin])
		}
		for _, node := range []string{"node-a", "node-b"} {
			if tracker.scoreCalls[plugin][node] != 1 {
				t.Errorf("plugin %s Score calls for %s: got %d, want 1", plugin, node, tracker.scoreCalls[plugin][node])
			}
		}
	}
}

func TestRunScorePluginsSkipsPlugin(t *testing.T) {
	tracker := newScoreCallTracker(2, 2)
	skipped := &fakePreScorePlugin{
		fakeScorePlugin: &fakeScorePlugin{name: "skipped", tracker: tracker},
		preStatus:       fwk.NewStatus(fwk.Skip),
	}
	active := &fakePreScorePlugin{fakeScorePlugin: &fakeScorePlugin{
		name:    "active",
		scores:  map[string]int64{"node-a": 30, "node-b": 40},
		tracker: tracker,
	}}
	skipped.extensions = &fakeScoreExtensions{plugin: skipped.name, tracker: tracker}
	active.extensions = &fakeScoreExtensions{plugin: active.name, tracker: tracker}

	got, err := RunScorePlugins(
		context.TODO(),
		[]PreScorePluginSpec{
			{Name: skipped.name, Plugin: skipped},
			{Name: active.name, Plugin: active},
		},
		[]ScorePluginSpec{
			{Name: skipped.name, Plugin: skipped, Weight: 10},
			{Name: active.name, Plugin: active, Weight: 2},
		},
		k8sframework.NewCycleState(),
		&v1.Pod{},
		testNodeInfos("node-a", "node-b"),
	)
	if err != nil {
		t.Fatalf("RunScorePlugins returned an error: %v", err)
	}
	want := map[string]float64{"node-a": 60, "node-b": 80}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected scores: got %v, want %v", got, want)
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.preScoreCalls[skipped.name] != 1 || len(tracker.scoreCalls[skipped.name]) != 0 || tracker.normalizeCalls[skipped.name] != 0 {
		t.Fatalf("skipped plugin calls: PreScore=%d Score=%v NormalizeScore=%d",
			tracker.preScoreCalls[skipped.name], tracker.scoreCalls[skipped.name], tracker.normalizeCalls[skipped.name])
	}
}

func TestRunScorePluginsReturnsErrors(t *testing.T) {
	t.Run("PreScore error", func(t *testing.T) {
		preScoreErr := errors.New("pre failed")
		plugin := &fakePreScorePlugin{
			fakeScorePlugin: &fakeScorePlugin{name: "pre-error"},
			preStatus:       fwk.AsStatus(preScoreErr),
		}
		got, err := RunScorePlugins(
			context.TODO(),
			[]PreScorePluginSpec{{Name: plugin.name, Plugin: plugin}},
			[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 1}},
			k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"),
		)
		assertRunScoreError(t, got, err, "PreScore plugin \"pre-error\"")
		if !errors.Is(err, preScoreErr) {
			t.Fatalf("expected wrapped PreScore error, got %v", err)
		}
	})

	t.Run("Score error", func(t *testing.T) {
		plugin := &fakeScorePlugin{
			name:        "score-error",
			scores:      map[string]int64{"node-a": 10},
			scoreStatus: map[string]*fwk.Status{"node-a": fwk.NewStatus(fwk.Error, "score failed")},
		}
		got, err := RunScorePlugins(
			context.TODO(), nil,
			[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 1}},
			k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"),
		)
		assertRunScoreError(t, got, err, "Score plugin \"score-error\" for node \"node-a\"")
	})

	t.Run("NormalizeScore error", func(t *testing.T) {
		plugin := &fakeScorePlugin{
			name:   "normalize-error",
			scores: map[string]int64{"node-a": 10},
			extensions: &fakeScoreExtensions{
				status: fwk.NewStatus(fwk.Error, "normalize failed"),
			},
		}
		got, err := RunScorePlugins(
			context.TODO(), nil,
			[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 1}},
			k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"),
		)
		assertRunScoreError(t, got, err, "NormalizeScore plugin \"normalize-error\"")
	})

	t.Run("invalid normalized score", func(t *testing.T) {
		plugin := &fakeScorePlugin{
			name:   "invalid-score",
			scores: map[string]int64{"node-a": 10},
			extensions: &fakeScoreExtensions{normalize: func(scores fwk.NodeScoreList) {
				scores[0].Score = fwk.MaxNodeScore + 1
			}},
		}
		got, err := RunScorePlugins(
			context.TODO(), nil,
			[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 1}},
			k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"),
		)
		assertRunScoreError(t, got, err, "invalid score")
	})
}

func TestRunScorePluginsSupportsScoreOnlyPlugin(t *testing.T) {
	plugin := &fakeScorePlugin{
		name:   "score-only",
		scores: map[string]int64{"node-a": 25},
		extensions: &fakeScoreExtensions{normalize: func(scores fwk.NodeScoreList) {
			scores[0].Score = 40
		}},
	}

	got, err := RunScorePlugins(
		context.TODO(), nil,
		[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 2}},
		k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"),
	)
	if err != nil {
		t.Fatalf("RunScorePlugins returned an error: %v", err)
	}
	want := map[string]float64{"node-a": 80}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected scores: got %v, want %v", got, want)
	}
}

func TestRunScorePluginsEmptyInputs(t *testing.T) {
	t.Run("no plugins", func(t *testing.T) {
		got, err := RunScorePlugins(context.TODO(), nil, nil, k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"))
		if err != nil {
			t.Fatalf("RunScorePlugins returned an error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected no scores, got %v", got)
		}
	})

	t.Run("no nodes", func(t *testing.T) {
		tracker := newScoreCallTracker(1, 0)
		plugin := &fakePreScorePlugin{fakeScorePlugin: &fakeScorePlugin{
			name:    "plugin",
			tracker: tracker,
			extensions: &fakeScoreExtensions{
				plugin:  "plugin",
				tracker: tracker,
			},
		}}
		got, err := RunScorePlugins(
			context.TODO(),
			[]PreScorePluginSpec{{Name: plugin.name, Plugin: plugin}},
			[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 1}},
			k8sframework.NewCycleState(), &v1.Pod{}, nil,
		)
		if err != nil {
			t.Fatalf("RunScorePlugins returned an error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected no scores, got %v", got)
		}

		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		if tracker.preScoreCalls[plugin.name] != 1 || len(tracker.scoreCalls[plugin.name]) != 0 || tracker.normalizeCalls[plugin.name] != 1 {
			t.Fatalf("calls with no nodes: PreScore=%d Score=%v NormalizeScore=%d",
				tracker.preScoreCalls[plugin.name], tracker.scoreCalls[plugin.name], tracker.normalizeCalls[plugin.name])
		}
	})
}

func TestRunScorePluginsReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plugin := &fakeScorePlugin{name: "plugin", scores: map[string]int64{"node-a": 10}}

	scores, err := RunScorePlugins(
		ctx, nil,
		[]ScorePluginSpec{{Name: plugin.name, Plugin: plugin, Weight: 1}},
		k8sframework.NewCycleState(), &v1.Pod{}, testNodeInfos("node-a"),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if scores != nil {
		t.Fatalf("expected no scores, got %v", scores)
	}
}

func assertRunScoreError(t *testing.T, scores map[string]float64, err error, wantError string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got scores %v", wantError, scores)
	}
	if !strings.Contains(err.Error(), wantError) {
		t.Fatalf("unexpected error: got %q, want substring %q", err, wantError)
	}
	if scores != nil {
		t.Fatalf("expected partial scores to be discarded, got %v", scores)
	}
}

func testNodeInfos(names ...string) []fwk.NodeInfo {
	nodeInfos := make([]fwk.NodeInfo, 0, len(names))
	for _, name := range names {
		nodeInfo := k8sframework.NewNodeInfo()
		nodeInfo.SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
		nodeInfos = append(nodeInfos, nodeInfo)
	}
	return nodeInfos
}
