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
	"maps"
	"testing"

	v1 "k8s.io/api/core/v1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestUpdateSnapshot(t *testing.T) {
	tests := []struct {
		name              string
		nodesInBinder     map[string]int
		setup             func(sc *SchedulerCache)
		prepareSnapshot   func(sc *SchedulerCache, snapshot *k8sutil.Snapshot)
		wantPresent       []string
		wantAbsent        []string
		wantNodesInBinder map[string]int
	}{
		{
			name: "clone NodesInBinder into snapshot",
			nodesInBinder: map[string]int{
				"node-a": 2,
				"node-b": 1,
			},
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "node-a")
			},
			wantPresent: []string{"node-a"},
			wantNodesInBinder: map[string]int{
				"node-a": 2,
				"node-b": 1,
			},
		},
		{
			name: "publish ready node and advance snapshot generation",
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "ready-node")
			},
			wantPresent: []string{"ready-node"},
		},
		{
			name: "skip node when Node is nil",
			setup: func(sc *SchedulerCache) {
				addPlaceholderNode(t, sc, "missing-node")
			},
			wantAbsent: []string{"missing-node"},
		},
		{
			name: "publish ready node and skip nil Node",
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "ready-node")
				addPlaceholderNode(t, sc, "missing-node")
			},
			wantPresent: []string{"ready-node"},
			wantAbsent:  []string{"missing-node"},
		},
		{
			name: "only update nodes newer than snapshot generation",
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "older-node")
				addReadyNode(t, sc, "newer-node")
			},
			prepareSnapshot: func(sc *SchedulerCache, snapshot *k8sutil.Snapshot) {
				older := sc.Nodes["older-node"]
				if older == nil {
					t.Fatal("expected older-node in cache")
				}
				// Walk starts from head(newer). newer is updated, then older stops the traversal.
				snapshot.SetGeneration(older.info.Generation)
			},
			wantPresent: []string{"newer-node"},
			wantAbsent:  []string{"older-node"},
		},
		{
			name: "skip update when generation is not newer",
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "ready-node")
			},
			prepareSnapshot: func(sc *SchedulerCache, snapshot *k8sutil.Snapshot) {
				if sc.headNode == nil {
					t.Fatal("expected headNode after setup")
				}
				snapshot.SetGeneration(sc.headNode.info.Generation)
			},
			wantAbsent: []string{"ready-node"},
		},
		{
			name: "update existing ready node already in snapshot",
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "ready-node")
			},
			prepareSnapshot: func(sc *SchedulerCache, snapshot *k8sutil.Snapshot) {
				if err := sc.UpdateSnapshot(snapshot); err != nil {
					t.Fatalf("seed UpdateSnapshot failed: %v", err)
				}
				// Bump generation again so the next UpdateSnapshot refreshes the existing entry.
				node := util.BuildNode("ready-node", schedulingapi.BuildResourceList("8", "16Gi"), nil)
				if err := sc.AddOrUpdateNode(node); err != nil {
					t.Fatalf("AddOrUpdateNode(ready-node) failed: %v", err)
				}
			},
			wantPresent: []string{"ready-node"},
		},
		{
			name: "remove deleted node from snapshot",
			setup: func(sc *SchedulerCache) {
				addReadyNode(t, sc, "alive-node")
			},
			prepareSnapshot: func(_ *SchedulerCache, snapshot *k8sutil.Snapshot) {
				deleted := schedulingapi.NewNodeInfo(util.BuildNode("deleted-node",
					schedulingapi.BuildResourceList("4", "8Gi"), nil))
				snapshot.AddOrUpdateNodes([]*schedulingapi.NodeInfo{deleted})
			},
			wantPresent: []string{"alive-node"},
			wantAbsent:  []string{"deleted-node"},
		},
		{
			name: "empty cache keeps empty snapshot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, _ := mockForTest()
			defer sc.cancel()

			if tt.nodesInBinder != nil {
				sc.BinderMutex.Lock()
				sc.NodesInBinder = maps.Clone(tt.nodesInBinder)
				sc.BinderMutex.Unlock()
			}

			if tt.setup != nil {
				tt.setup(sc)
			}

			snapshot := k8sutil.NewEmptySnapshot()
			if tt.prepareSnapshot != nil {
				tt.prepareSnapshot(sc, snapshot)
			}

			if err := sc.UpdateSnapshot(snapshot); err != nil {
				t.Fatalf("UpdateSnapshot failed: %v", err)
			}

			fwkNodes := snapshot.GetFwkNodeInfoMap()
			for _, name := range tt.wantPresent {
				if _, ok := fwkNodes[name]; !ok {
					t.Errorf("expected node %q in snapshot", name)
				}
			}
			for _, name := range tt.wantAbsent {
				if _, ok := fwkNodes[name]; ok {
					t.Errorf("expected node %q not in snapshot", name)
				}
			}

			wantBinder := tt.wantNodesInBinder
			if wantBinder == nil {
				wantBinder = map[string]int{}
			}
			if !maps.Equal(snapshot.NodesInBinder, wantBinder) {
				t.Errorf("NodesInBinder = %v, want %v", snapshot.NodesInBinder, wantBinder)
			}

			wantGeneration := int64(0)
			if sc.headNode != nil {
				wantGeneration = sc.headNode.info.Generation
			}
			if got := snapshot.GetGeneration(); got != wantGeneration {
				t.Errorf("snapshot generation = %d, want %d", got, wantGeneration)
			}
		})
	}
}

func addReadyNode(t *testing.T, sc *SchedulerCache, name string) {
	t.Helper()
	node := util.BuildNode(name, schedulingapi.BuildResourceList("4", "8Gi"), nil)
	if err := sc.AddOrUpdateNode(node); err != nil {
		t.Fatalf("AddOrUpdateNode(%s) failed: %v", name, err)
	}
	if !sc.Nodes[name].info.Ready() {
		t.Fatalf("expected node %q to be Ready", name)
	}
}

func addPlaceholderNode(t *testing.T, sc *SchedulerCache, name string) {
	t.Helper()
	pod := util.BuildPod("default", "pod-on-"+name, name,
		v1.PodRunning, schedulingapi.BuildResourceList("100m", "128Mi"), "", nil, nil)

	sc.Mutex.Lock()
	err := sc.addTask(schedulingapi.NewTaskInfo(pod))
	sc.Mutex.Unlock()
	if err != nil {
		t.Fatalf("addTask for placeholder node %q failed: %v", name, err)
	}

	placeholder, ok := sc.Nodes[name]
	if !ok {
		t.Fatalf("expected placeholder node %q in cache", name)
	}
	if placeholder.info.Node != nil {
		t.Fatalf("expected placeholder Node to be nil, got %#v", placeholder.info.Node)
	}
	if placeholder.info.Ready() {
		t.Fatalf("expected placeholder node %q to be not Ready", name)
	}
}
