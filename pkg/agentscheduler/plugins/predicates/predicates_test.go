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

package predicates

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"

	agentframework "volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulerpredicates "volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/nodescore"
)

type candidateRecordingScorePlugin struct {
	name          string
	preScoreNodes []string
	scoreNodes    []string
}

func (p *candidateRecordingScorePlugin) Name() string {
	return p.name
}

func (p *candidateRecordingScorePlugin) PreScore(_ context.Context, _ fwk.CycleState, _ *v1.Pod, nodes []fwk.NodeInfo) *fwk.Status {
	for _, node := range nodes {
		p.preScoreNodes = append(p.preScoreNodes, node.Node().Name)
	}
	return nil
}

func (p *candidateRecordingScorePlugin) Score(_ context.Context, _ fwk.CycleState, _ *v1.Pod, node fwk.NodeInfo) (int64, *fwk.Status) {
	p.scoreNodes = append(p.scoreNodes, node.Node().Name)
	return 1, nil
}

func (p *candidateRecordingScorePlugin) ScoreExtensions() fwk.ScoreExtensions {
	return nil
}

func TestBatchNodeOrderUsesOnlyCandidateNodes(t *testing.T) {
	candidate := api.NewNodeInfo(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "candidate"}})
	extra := api.NewNodeInfo(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra"}})
	snapshot := k8sutil.NewEmptySnapshot()
	snapshot.AddOrUpdateNodes([]*api.NodeInfo{candidate, extra})

	agentFwk := &agentframework.Framework{
		Framework: k8sutil.NewFramework(nil, k8sutil.WithSnapshotSharedLister(snapshot)),
	}
	recorder := &candidateRecordingScorePlugin{name: "recording-score"}
	plugin := &predicatesPlugin{PredicatesPlugin: &schedulerpredicates.PredicatesPlugin{
		PreScorePlugins: map[string]fwk.PreScorePlugin{recorder.name: recorder},
		ScorePlugins:    map[string]nodescore.BaseScorePlugin{recorder.name: recorder},
		ScoreWeights:    map[string]int{recorder.name: 1},
		PreScoreOrder:   []string{recorder.name},
		ScoreOrder:      []string{recorder.name},
	}}
	task := &api.TaskInfo{UID: "pod", Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod"}}}

	scores, err := plugin.batchNodeOrder(agentFwk, task, []*api.NodeInfo{candidate})
	if err != nil {
		t.Fatalf("batchNodeOrder returned an error: %v", err)
	}
	if !reflect.DeepEqual(scores, map[string]float64{"candidate": 1}) {
		t.Fatalf("unexpected scores: %v", scores)
	}
	if !reflect.DeepEqual(recorder.preScoreNodes, []string{"candidate"}) {
		t.Fatalf("PreScore nodes: got %v, want only candidate", recorder.preScoreNodes)
	}
	if !reflect.DeepEqual(recorder.scoreNodes, []string{"candidate"}) {
		t.Fatalf("Score nodes: got %v, want only candidate", recorder.scoreNodes)
	}
}
