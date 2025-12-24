/*
Copyright 2024 The Volcano Authors.

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

package k8s

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestSnapshot(t *testing.T) {
	var (
		nodeName = "test-node"
		pod1     = util.BuildPod("node_info_cache_test", "test-1", nodeName,
			v1.PodRunning, api.BuildResourceList("200m", "1Ki"), "pg2",
			make(map[string]string), make(map[string]string))
		pod2 = util.BuildPod("node_info_cache_test", "test-2", nodeName,
			v1.PodRunning, api.BuildResourceList("200m", "1Ki"), "pg2",
			map[string]string{"test": "test"}, make(map[string]string))
	)

	nodeInfo := framework.NewNodeInfo(pod1, pod2)

	tests := []struct {
		name                                          string
		nodeInfoMap                                   map[string]*framework.NodeInfo
		expectedNodeInfos                             []fwk.NodeInfo
		expectedPodsWithAffinityNodeInfos             []fwk.NodeInfo
		expectedPodsWithRequiredAntiAffinityNodeInfos []fwk.NodeInfo
		expectedPods                                  []*v1.Pod
		expectErr                                     error
	}{
		{
			name:                              "test snapshot operation",
			nodeInfoMap:                       map[string]*framework.NodeInfo{nodeName: nodeInfo},
			expectedNodeInfos:                 []fwk.NodeInfo{nodeInfo.Snapshot()},
			expectedPodsWithAffinityNodeInfos: []fwk.NodeInfo{}, // pods don't have affinity
			expectedPodsWithRequiredAntiAffinityNodeInfos: []fwk.NodeInfo{}, // pods don't have anti-affinity
			expectedPods: []*v1.Pod{pod2},
			expectErr:    fmt.Errorf("nodeinfo not found for node name %q", nodeName),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := NewSnapshot(util.ConvertNodeInfoSliceToInterface(tc.nodeInfoMap))
			nodeInfoList, err := snapshot.List()
			if !reflect.DeepEqual(nodeInfoList, tc.expectedNodeInfos) || err != nil {
				t.Fatalf("unexpected list nodeInfos value\ngot: %+v\nwant: %+v\nerr: %s", nodeInfoList, tc.expectedNodeInfos, err)
			}

			_, err = snapshot.Get(nodeName)
			if !reflect.DeepEqual(tc.expectErr, err) {
				t.Fatalf("unexpected get nodeInfos by nodeName value (+got: %T/-want: %T)", err, tc.expectErr)
			}

			nodeInfoList, err = snapshot.HavePodsWithAffinityList()
			if !reflect.DeepEqual(tc.expectedPodsWithAffinityNodeInfos, nodeInfoList) || err != nil {
				t.Fatalf("unexpected havePodsWithAffinityNodeInfoList\ngot:  %+v\nwant: %+v\nerr: %v", nodeInfoList, tc.expectedPodsWithAffinityNodeInfos, err)
			}

			nodeInfoList, err = snapshot.HavePodsWithRequiredAntiAffinityList()
			if !reflect.DeepEqual(tc.expectedPodsWithRequiredAntiAffinityNodeInfos, nodeInfoList) || err != nil {
				t.Fatalf("unexpected havePodsWithRequiredAntiAffinityNodeInfoList\ngot:  %+v\nwant: %+v\nerr: %v", nodeInfoList, tc.expectedPodsWithRequiredAntiAffinityNodeInfos, err)
			}

			sel, _ := labels.Parse("test==test")
			pods, err := snapshot.Pods().List(sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Fatalf("unexpected list pods value\ngot:  %+v\nwant: %+v\nerr: %v", pods, tc.expectedPods, err)
			}

			pods, err = snapshot.Pods().FilteredList(func(pod *v1.Pod) bool {
				return true
			}, sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Fatalf("unexpected list filtered pods value\ngot:  %+v\nwant: %+v\nerr: %v", pods, tc.expectedPods, err)
			}

			nodeInfos, err := snapshot.NodeInfos().List()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfos) || err != nil {
				t.Fatalf("unexpected NodeInfos list value\ngot:  %+v\nwant: %+v\nerr: %v", nodeInfos, tc.expectedNodeInfos, err)
			}
		})
	}
}
