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
	"testing"

	v1 "k8s.io/api/core/v1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestUpdateSnapshotSkipsNotReadyPlaceholder ensures a placeholder NodeInfo created by addTask
// for a missing nodeName (NewNodeInfo(nil) → NotReady / UnInitialized) is not published into the
// incremental scheduling snapshot. Without the Ready() filter, AddOrUpdateNodes would panic
// on volcanoNodeInfo.Node.Name (see #5924).
func TestUpdateSnapshotSkipsNotReadyPlaceholder(t *testing.T) {
	sc := NewDefaultMockSchedulerCache("agent-scheduler")

	readyNode := util.BuildNode("ready-node", schedulingapi.BuildResourceList("4", "8Gi"), nil)
	readyItem := newNodeInfoListItem(schedulingapi.NewNodeInfo(readyNode))
	sc.Nodes[readyNode.Name] = readyItem
	sc.NodeList = append(sc.NodeList, readyNode.Name)

	missingNodeName := "missing-node"
	readyPod := util.BuildPod("default", "bound-on-ready-node", readyNode.Name,
		v1.PodRunning, schedulingapi.BuildResourceList("100m", "128Mi"), "", nil, nil)
	missingPod := util.BuildPod("default", "bound-on-missing-node", missingNodeName,
		v1.PodRunning, schedulingapi.BuildResourceList("100m", "128Mi"), "", nil, nil)

	sc.Mutex.Lock()
	if err := sc.addTask(schedulingapi.NewTaskInfo(readyPod)); err != nil {
		sc.Mutex.Unlock()
		t.Fatalf("addTask on ready node failed: %v", err)
	}
	if err := sc.addTask(schedulingapi.NewTaskInfo(missingPod)); err != nil {
		sc.Mutex.Unlock()
		t.Fatalf("addTask on missing node failed: %v", err)
	}
	sc.Mutex.Unlock()

	placeholder, ok := sc.Nodes[missingNodeName]
	if !ok {
		t.Fatalf("expected placeholder node %q in cache", missingNodeName)
	}
	if placeholder.info.Node != nil {
		t.Fatalf("expected placeholder Node to be nil, got %#v", placeholder.info.Node)
	}
	if placeholder.info.Ready() {
		t.Fatalf("expected placeholder node to be not Ready")
	}
	if sc.headNode != placeholder {
		t.Fatalf("expected placeholder node to be at the head of the generation list")
	}

	snapshot := k8sutil.NewEmptySnapshot()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("UpdateSnapshot panicked on placeholder node: %v", r)
		}
	}()
	if err := sc.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}

	fwkNodes := snapshot.GetFwkNodeInfoMap()
	if _, exists := fwkNodes[missingNodeName]; exists {
		t.Fatalf("placeholder node %q should not be published into snapshot", missingNodeName)
	}
	if _, exists := fwkNodes[readyNode.Name]; !exists {
		t.Fatalf("ready node %q should be published into snapshot", readyNode.Name)
	}
}
