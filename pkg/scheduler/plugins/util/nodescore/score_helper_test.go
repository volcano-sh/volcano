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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type fakeScorePlugin struct {
	name        string
	scores      map[string]int64
	scoreStatus map[string]*fwk.Status
	extensions  *fakeScoreExtensions
}

func (p *fakeScorePlugin) Name() string {
	return p.name
}

func (p *fakeScorePlugin) Score(_ context.Context, _ fwk.CycleState, _ *v1.Pod, nodeInfo fwk.NodeInfo) (int64, *fwk.Status) {
	nodeName := nodeInfo.Node().Name
	return p.scores[nodeName], p.scoreStatus[nodeName]
}

func (p *fakeScorePlugin) ScoreExtensions() fwk.ScoreExtensions {
	if p.extensions == nil {
		return nil
	}
	return p.extensions
}

type fakePreScoreAndScorePlugin struct {
	*fakeScorePlugin
	preScoreStatus *fwk.Status
}

func (p *fakePreScoreAndScorePlugin) PreScore(_ context.Context, _ fwk.CycleState, _ *v1.Pod, _ []fwk.NodeInfo) *fwk.Status {
	return p.preScoreStatus
}

type fakeScoreExtensions struct {
	normalize func(fwk.NodeScoreList)
	status    *fwk.Status
}

func (e *fakeScoreExtensions) NormalizeScore(_ context.Context, _ fwk.CycleState, _ *v1.Pod, scores fwk.NodeScoreList) *fwk.Status {
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

func TestRunScorePlugins(t *testing.T) {
	plugin1 := &fakePreScoreAndScorePlugin{fakeScorePlugin: &fakeScorePlugin{
		name:   "plugin-1",
		scores: map[string]int64{"node-a": 10, "node-b": 20},
	}}
	plugin2 := &fakePreScoreAndScorePlugin{fakeScorePlugin: &fakeScorePlugin{
		name:   "plugin-2",
		scores: map[string]int64{"node-a": 80, "node-b": 40},
		extensions: &fakeScoreExtensions{normalize: func(scores fwk.NodeScoreList) {
			for i := range scores {
				switch scores[i].Name {
				case "node-a":
					scores[i].Score = 20
				case "node-b":
					scores[i].Score = 60
				}
			}
		}},
	}}
	// Both plugins share the same implementation name to verify that Skip is matched by the entry registration name.
	skippedPlugin := &fakePreScoreAndScorePlugin{
		fakeScorePlugin: &fakeScorePlugin{
			name:   "shared-score-plugin",
			scores: map[string]int64{"node-a": 100, "node-b": 100},
			scoreStatus: map[string]*fwk.Status{
				"node-a": fwk.NewStatus(fwk.Error, "skipped plugin ran"),
				"node-b": fwk.NewStatus(fwk.Error, "skipped plugin ran"),
			},
		},
		preScoreStatus: fwk.NewStatus(fwk.Skip),
	}
	nonSkippedPlugin := &fakePreScoreAndScorePlugin{fakeScorePlugin: &fakeScorePlugin{
		name:   "shared-score-plugin",
		scores: map[string]int64{"node-a": 30, "node-b": 40},
	}}
	preScoreErr := errors.New("pre failed")
	preScoreErrorPlugin := &fakePreScoreAndScorePlugin{
		fakeScorePlugin: &fakeScorePlugin{name: "pre-error"},
		preScoreStatus:  fwk.AsStatus(preScoreErr),
	}
	scoreErrorPlugin := &fakeScorePlugin{
		name:        "score-error",
		scores:      map[string]int64{"node-a": 10},
		scoreStatus: map[string]*fwk.Status{"node-a": fwk.NewStatus(fwk.Error, "score failed")},
	}
	normalizeErrorPlugin := &fakeScorePlugin{
		name:       "normalize-error",
		scores:     map[string]int64{"node-a": 10},
		extensions: &fakeScoreExtensions{status: fwk.NewStatus(fwk.Error, "normalize failed")},
	}
	invalidScorePlugin := &fakeScorePlugin{
		name:   "invalid-score",
		scores: map[string]int64{"node-a": 10},
		extensions: &fakeScoreExtensions{normalize: func(scores fwk.NodeScoreList) {
			scores[0].Score = fwk.MaxNodeScore + 1
		}},
	}
	scoreOnlyPlugin := &fakeScorePlugin{
		name:   "score-only",
		scores: map[string]int64{"node-a": 25},
		extensions: &fakeScoreExtensions{normalize: func(scores fwk.NodeScoreList) {
			scores[0].Score = 40
		}},
	}
	tests := []struct {
		name                  string
		preScorePluginEntries []PreScorePluginEntry
		scorePluginEntries    []ScorePluginEntry
		nodeInfos             []fwk.NodeInfo
		want                  map[string]float64
		wantError             string
		wantCause             error
	}{
		{
			name: "normalizes, weights, and aggregates multiple plugins",
			preScorePluginEntries: []PreScorePluginEntry{
				{Name: plugin1.Name(), Plugin: plugin1},
				{Name: plugin2.Name(), Plugin: plugin2},
			},
			scorePluginEntries: []ScorePluginEntry{
				{Name: plugin1.Name(), Plugin: plugin1, Weight: 2},
				{Name: plugin2.Name(), Plugin: plugin2, Weight: 3},
			},
			nodeInfos: testNodeInfos("node-a", "node-b"),
			want:      map[string]float64{"node-a": 80, "node-b": 220},
		},
		{
			name: "skips matching score plugin",
			preScorePluginEntries: []PreScorePluginEntry{
				{Name: "skipped-registration", Plugin: skippedPlugin},
				{Name: "active-registration", Plugin: nonSkippedPlugin},
			},
			scorePluginEntries: []ScorePluginEntry{
				{Name: "skipped-registration", Plugin: skippedPlugin, Weight: 10},
				{Name: "active-registration", Plugin: nonSkippedPlugin, Weight: 2},
			},
			nodeInfos: testNodeInfos("node-a", "node-b"),
			want:      map[string]float64{"node-a": 60, "node-b": 80},
		},
		{
			name:               "supports score-only plugin",
			scorePluginEntries: []ScorePluginEntry{{Name: scoreOnlyPlugin.Name(), Plugin: scoreOnlyPlugin, Weight: 2}},
			nodeInfos:          testNodeInfos("node-a"),
			want:               map[string]float64{"node-a": 80},
		},
		{
			name:      "returns empty scores when no score plugins are registered",
			nodeInfos: testNodeInfos("node-a"),
			want:      map[string]float64{},
		},
		{
			name:                  "returns PreScore error",
			preScorePluginEntries: []PreScorePluginEntry{{Name: preScoreErrorPlugin.Name(), Plugin: preScoreErrorPlugin}},
			scorePluginEntries:    []ScorePluginEntry{{Name: preScoreErrorPlugin.Name(), Plugin: preScoreErrorPlugin, Weight: 1}},
			nodeInfos:             testNodeInfos("node-a"),
			wantError:             `PreScore plugin "pre-error"`,
			wantCause:             preScoreErr,
		},
		{
			name:               "returns Score error",
			scorePluginEntries: []ScorePluginEntry{{Name: scoreErrorPlugin.Name(), Plugin: scoreErrorPlugin, Weight: 1}},
			nodeInfos:          testNodeInfos("node-a"),
			wantError:          `Score plugin "score-error" for node "node-a"`,
		},
		{
			name:               "returns NormalizeScore error",
			scorePluginEntries: []ScorePluginEntry{{Name: normalizeErrorPlugin.Name(), Plugin: normalizeErrorPlugin, Weight: 1}},
			nodeInfos:          testNodeInfos("node-a"),
			wantError:          `NormalizeScore plugin "normalize-error"`,
		},
		{
			name:               "rejects invalid normalized score",
			scorePluginEntries: []ScorePluginEntry{{Name: invalidScorePlugin.Name(), Plugin: invalidScorePlugin, Weight: 1}},
			nodeInfos:          testNodeInfos("node-a"),
			wantError:          "invalid score",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RunScorePlugins(
				tt.preScorePluginEntries,
				tt.scorePluginEntries,
				k8sframework.NewCycleState(),
				&v1.Pod{},
				tt.nodeInfos,
			)

			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
				if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
					t.Fatalf("expected error to wrap %v, got %v", tt.wantCause, err)
				}
				if got != nil {
					t.Fatalf("expected partial scores to be discarded, got %v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("RunScorePlugins returned an error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("unexpected scores: got %v, want %v", got, tt.want)
			}
		})
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
