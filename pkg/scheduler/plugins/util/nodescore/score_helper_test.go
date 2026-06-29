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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

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
