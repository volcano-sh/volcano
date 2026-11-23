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
	"fmt"
	"maps"
	"sync"
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

func TestUpdateSnapshotSharesNodeRecords(t *testing.T) {
	sc, _ := mockForTest()
	defer sc.cancel()
	addReadyNode(t, sc, "node-a")
	addReadyNode(t, sc, "node-b")

	worker1, worker2 := k8sutil.NewEmptySnapshot(), k8sutil.NewEmptySnapshot()
	for _, s := range []*k8sutil.Snapshot{worker1, worker2} {
		if err := sc.UpdateSnapshot(s); err != nil {
			t.Fatalf("UpdateSnapshot failed: %v", err)
		}
	}
	for _, name := range []string{"node-a", "node-b"} {
		n1, _ := worker1.GetVolcanoNodeInfo(name)
		n2, _ := worker2.GetVolcanoNodeInfo(name)
		if n1 == nil || n1 != n2 {
			t.Errorf("node %q is not shared between workers", name)
		}
	}

	// Only node-b changes: node-a's record must be reused, node-b rebuilt, and the
	// snapshot an earlier cycle is still reading must not change.
	oldNodeB, _ := worker1.GetVolcanoNodeInfo("node-b")
	updated := util.BuildNode("node-b", schedulingapi.BuildResourceList("8", "16Gi"), nil)
	if err := sc.AddOrUpdateNode(updated); err != nil {
		t.Fatalf("AddOrUpdateNode(node-b) failed: %v", err)
	}
	worker3 := k8sutil.NewEmptySnapshot()
	if err := sc.UpdateSnapshot(worker3); err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}

	oldNodeA, _ := worker1.GetVolcanoNodeInfo("node-a")
	newNodeA, _ := worker3.GetVolcanoNodeInfo("node-a")
	if oldNodeA != newNodeA {
		t.Error("unchanged node-a was rebuilt")
	}
	newNodeB, _ := worker3.GetVolcanoNodeInfo("node-b")
	if newNodeB == oldNodeB {
		t.Error("changed node-b was not rebuilt")
	}
	if current, _ := worker1.GetVolcanoNodeInfo("node-b"); current != oldNodeB {
		t.Error("snapshot of an earlier cycle was modified")
	}
}

func TestUpdateSnapshotRemovedNode(t *testing.T) {
	sc, queue := mockForTest()
	defer sc.cancel()
	sc.ConflictAwareBinder = NewConflictAwareBinder(sc, queue)
	addReadyNode(t, sc, "node-a")
	addReadyNode(t, sc, "node-b")

	snapshot := k8sutil.NewEmptySnapshot()
	if err := sc.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}

	if err := sc.RemoveNode("node-b"); err != nil {
		t.Fatalf("RemoveNode failed: %v", err)
	}
	if err := sc.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if _, ok := snapshot.GetFwkNodeInfoMap()["node-b"]; ok {
		t.Error("removed node-b is still in the snapshot")
	}
	if _, ok := snapshot.GetFwkNodeInfoMap()["node-a"]; !ok {
		t.Error("node-a is missing from the snapshot")
	}

	addReadyNode(t, sc, "node-b")
	if err := sc.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if _, ok := snapshot.GetFwkNodeInfoMap()["node-b"]; !ok {
		t.Error("re-added node-b is missing from the snapshot")
	}
}

func TestUpdateSnapshotConcurrentWorkers(t *testing.T) {
	sc, _ := mockForTest()
	defer sc.cancel()
	for i := range 20 {
		addReadyNode(t, sc, fmt.Sprintf("node-%d", i))
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot := k8sutil.NewEmptySnapshot()
			for range 200 {
				if err := sc.UpdateSnapshot(snapshot); err != nil {
					t.Errorf("UpdateSnapshot failed: %v", err)
					return
				}
				for _, node := range snapshot.GetFwkNodeInfoList() {
					_ = node.Node().Name
				}
			}
		}()
	}
	for i := range 200 {
		node := util.BuildNode(fmt.Sprintf("node-%d", i%20), schedulingapi.BuildResourceList("4", "8Gi"), nil)
		if err := sc.AddOrUpdateNode(node); err != nil {
			t.Fatalf("AddOrUpdateNode failed: %v", err)
		}
	}
	wg.Wait()
}
