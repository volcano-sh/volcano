/*
Copyright 2025 The Volcano Authors.

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

package framework

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestGenerateNodeMapAndSlice_ImageStates(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{},
		},
	}

	nodeInfo := api.NewNodeInfo(node)
	nodeInfo.ImageStates = map[string]*fwk.ImageStateSummary{
		"docker.io/library/nginx:latest": {
			Size:     100 * 1024 * 1024,
			NumNodes: 3,
		},
		"docker.io/library/redis:7": {
			Size:     50 * 1024 * 1024,
			NumNodes: 5,
		},
	}

	nodes := map[string]*api.NodeInfo{
		"node1": nodeInfo,
	}

	nodeMap := GenerateNodeMapAndSlice(nodes)

	k8sNodeInfo, ok := nodeMap["node1"]
	if !ok {
		t.Fatalf("expected node1 in nodeMap")
	}

	if len(k8sNodeInfo.GetImageStates()) != 2 {
		t.Fatalf("expected 2 image states, got %d", len(k8sNodeInfo.GetImageStates()))
	}

	nginxState, ok := k8sNodeInfo.GetImageStates()["docker.io/library/nginx:latest"]
	if !ok {
		t.Fatalf("expected nginx image state")
	}
	if nginxState.Size != 100*1024*1024 {
		t.Errorf("expected nginx size 100MB, got %d", nginxState.Size)
	}
	if nginxState.NumNodes != 3 {
		t.Errorf("expected nginx NumNodes 3, got %d", nginxState.NumNodes)
	}

	redisState, ok := k8sNodeInfo.GetImageStates()["docker.io/library/redis:7"]
	if !ok {
		t.Fatalf("expected redis image state")
	}
	if redisState.Size != 50*1024*1024 {
		t.Errorf("expected redis size 50MB, got %d", redisState.Size)
	}
	if redisState.NumNodes != 5 {
		t.Errorf("expected redis NumNodes 5, got %d", redisState.NumNodes)
	}
}
