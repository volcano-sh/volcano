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
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestSnapshot(t *testing.T) {
	var (
		nodeName = "test-node"

		pod1 = util.MakePod().
			Namespace("node_info_cache_test").
			Name("test-1").
			NodeName(nodeName).
			PodPhase(v1.PodRunning).
			ResourceList(api.BuildResourceList("200m", "1Ki")).
			GroupName("pg2").
			Labels(make(map[string]string)).
			NodeSelector(make(map[string]string)).
			Obj()
		pod2 = util.MakePod().
			Namespace("node_info_cache_test").
			Name("test-2").
			NodeName(nodeName).
			PodPhase(v1.PodRunning).
			ResourceList(api.BuildResourceList("200m", "1Ki")).
			GroupName("pg2").
			Labels(map[string]string{"test": "test"}).
			NodeSelector(make(map[string]string)).
			Obj()
	)

	tests := []struct {
		name              string
		nodeInfoMap       map[string]*framework.NodeInfo
		expectedNodeInfos []*framework.NodeInfo
		expectedPods      []*v1.Pod
		expectErr         error
	}{
		{
			name: "test snapshot operation",
			nodeInfoMap: map[string]*framework.NodeInfo{nodeName: {
				Requested:        &framework.Resource{},
				NonZeroRequested: &framework.Resource{},
				Allocatable:      &framework.Resource{},
				Generation:       2,
				ImageStates:      map[string]*framework.ImageStateSummary{},
				PVCRefCounts:     map[string]int{},
				Pods: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithRequiredAntiAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
			}},
			expectedNodeInfos: []*framework.NodeInfo{{
				Requested:        &framework.Resource{},
				NonZeroRequested: &framework.Resource{},
				Allocatable:      &framework.Resource{},
				Generation:       2,
				ImageStates:      map[string]*framework.ImageStateSummary{},
				PVCRefCounts:     map[string]int{},
				Pods: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithRequiredAntiAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
			}},
			expectedPods: []*v1.Pod{pod2},
			expectErr:    fmt.Errorf("nodeinfo not found for node name %q", nodeName),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := NewSnapshot(tc.nodeInfoMap)
			nodeInfoList, err := snapshot.List()
			if !reflect.DeepEqual(nodeInfoList, tc.expectedNodeInfos) || err != nil {
				t.Fatalf("unexpected list nodeInfos value (+got: %s/-want: %s), err: %s", tc.expectedNodeInfos, nodeInfoList, err)
			}

			_, err = snapshot.Get(nodeName)
			if !reflect.DeepEqual(tc.expectErr, err) {
				t.Fatalf("unexpected get nodeInfos by nodeName value (+got: %T/-want: %T)", err, tc.expectErr)
			}

			nodeInfoList, err = snapshot.HavePodsWithAffinityList()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfoList) || err != nil {
				t.Fatalf("unexpected list HavePodsWithAffinity nodeInfos value (+got: %s/-want: %s), err: %s", nodeInfoList, tc.expectedNodeInfos, err)
			}

			nodeInfoList, err = snapshot.HavePodsWithRequiredAntiAffinityList()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfoList) || err != nil {
				t.Fatalf("unexpected list PodsWithRequiredAntiAffinity nodeInfos value (+got: %s/-want: %s), err: %s", nodeInfoList, tc.expectedNodeInfos, err)
			}

			sel, _ := labels.Parse("test==test")
			pods, err := snapshot.Pods().List(sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Fatalf("unexpected list pods value (+got: %s/-want: %s), err: %s", pods, tc.expectedNodeInfos, err)
			}

			pods, err = snapshot.Pods().FilteredList(func(pod *v1.Pod) bool {
				return true
			}, sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Fatalf("unexpected list filtered pods value (+got: %s/-want: %s), err: %s", pods, tc.expectedPods, err)
			}

			nodeInfos, err := snapshot.NodeInfos().List()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfos) || err != nil {
				t.Fatalf("unexpected list nodeInfos value (+got: %s/-want: %s), err: %s", nodeInfos, tc.expectedNodeInfos, err)
			}
		})
	}
}
