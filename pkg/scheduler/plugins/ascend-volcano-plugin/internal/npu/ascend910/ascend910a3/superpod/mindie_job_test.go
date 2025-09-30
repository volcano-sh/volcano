/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package superpod is using for HuaWei Atlas 900 A3 SuperPod affinity schedule.
*/

package superpod

import (
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

const npuNodes = 30

func TestIsMindIEJob(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "01 - Both labels exist (app and jobID) should return true",
			labels:   map[string]string{mindIEJobAppLabelKey: "myapp", mindIEJobIDLabelKey: "123"},
			expected: true,
		},
		{
			name:     "02 - Missing app label should return false",
			labels:   map[string]string{mindIEJobIDLabelKey: "123"},
			expected: false,
		},
		{
			name:     "03 - Missing jobID label should return false",
			labels:   map[string]string{mindIEJobAppLabelKey: "myapp"},
			expected: false,
		},
		{
			name:     "04 - No labels at all should return false",
			labels:   nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &module910SuperPod{}
			tp.Label = tt.labels
			result := tp.isMindIEJob()
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("isMindIEJob() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsIdleResourceFit(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected bool
	}{
		{
			name:     "01 - Annotation is nil should return false",
			input:    nil,
			expected: false,
		},
		{
			name:     "02 - sp-fit not exist in Annotation should return false",
			input:    map[string]string{"other-key": "value"},
			expected: false,
		},
		{
			name:     "03 - value of sp-fit is not idlest should return false",
			input:    map[string]string{"sp-fit": "other-policy"},
			expected: false,
		},
		{
			name:     "04 - value of sp-fit is idlest should return true",
			input:    map[string]string{"sp-fit": string(idlestResourceFitPolicy)},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &module910SuperPod{}
			tp.Annotation = tt.input
			result := tp.isIdleResourceFit()
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("isMindIEJob() = %v, want %v", result, tt.expected)
			}
		})
	}
}

type selectSuperPodForMindIEJobTestCase struct {
	name        string
	nodes       []*api.NodeInfo
	totalNodes  map[string]plugin.NPUNode
	npuTaskNum  int
	annotations map[string]string
	jobs        map[api.JobID]plugin.SchedulerJob
	want        map[int32]struct{}
	wantErr     error
}

func mockPodWithAffinity() *corev1.Pod {
	pod := &corev1.Pod{}
	wpaf := corev1.WeightedPodAffinityTerm{
		Weight: 100,
		PodAffinityTerm: corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"jobID": "default-test"},
			},
		},
	}
	affinity := &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{wpaf},
		},
	}
	pod.Spec.Affinity = affinity
	return pod
}

func mockTask() *api.TaskInfo {
	return &api.TaskInfo{
		UID:  "0",
		Job:  "job1",
		Name: "task0",
		Pod:  mockPodWithAffinity(),
	}
}

func selectSuperPodForMindIEJobTestCases01() []selectSuperPodForMindIEJobTestCase {
	totalNodes1 := newNPUNodes(npuNodes, superPodSize10)
	testCase1 := selectSuperPodForMindIEJobTestCase{
		name:       "01-total nodes is not fit for job require, should return err",
		nodes:      []*api.NodeInfo{node0, node1},
		totalNodes: totalNodes1,
		npuTaskNum: npuTaskNum4,
		want:       nil,
		wantErr:    fmt.Errorf("not enough virtual super-pod to schedule, required 2, total 1"),
	}
	testCase2 := selectSuperPodForMindIEJobTestCase{
		name:       "02-job with out sp-fit should return the super-pod witch fit exclude reserve",
		nodes:      []*api.NodeInfo{node0, node1, node2, node3, node10, node11, node12},
		totalNodes: totalNodes1,
		npuTaskNum: npuTaskNum2,
		want:       map[int32]struct{}{0: {}},
	}
	testCase3 := selectSuperPodForMindIEJobTestCase{
		name:       "02-job with out sp-fit should return smaller super-pod",
		nodes:      []*api.NodeInfo{node0, node1, node2, node3, node10, node11, node12, node13, node14},
		totalNodes: totalNodes1,
		npuTaskNum: npuTaskNum2,
		want:       map[int32]struct{}{0: {}},
	}
	testCase4 := selectSuperPodForMindIEJobTestCase{
		name:        "03-job with sp-fit should return bigger super-pod",
		nodes:       []*api.NodeInfo{node0, node1, node2, node3, node10, node11, node12, node13, node14},
		annotations: map[string]string{"sp-fit": string(idlestResourceFitPolicy)},
		totalNodes:  totalNodes1,
		npuTaskNum:  npuTaskNum2,
		want:        map[int32]struct{}{1: {}},
	}
	return []selectSuperPodForMindIEJobTestCase{
		testCase1,
		testCase2,
		testCase3,
		testCase4,
	}
}

func selectSuperPodForMindIEJobTestCases() selectSuperPodForMindIEJobTestCase {
	totalNodes2 := newNPUNodes(npuNodes, superPodSize10)
	totalNodes2["node4"].Tasks["task4"] = &api.TaskInfo{Job: "job5"}
	totalNodes2["node14"].Tasks["task14"] = &api.TaskInfo{Job: "job14"}
	totalNodes2["node15"].Tasks["task15"] = &api.TaskInfo{Job: "job15"}
	label := map[string]string{"jobID": "default-test"}
	job5 := plugin.SchedulerJob{}
	job5.Label = label
	job14 := plugin.SchedulerJob{}
	job14.Label = label
	job15 := plugin.SchedulerJob{}
	job15.Label = label
	return selectSuperPodForMindIEJobTestCase{
		name:        "04-job with sp-fit should return the super-pod with more affinit pod ",
		nodes:       []*api.NodeInfo{node0, node1, node2, node3, node10, node11, node12, node13},
		annotations: map[string]string{"sp-fit": string(idlestResourceFitPolicy)},
		totalNodes:  totalNodes2,
		npuTaskNum:  npuTaskNum2,
		want:        map[int32]struct{}{1: {}},
		jobs: map[api.JobID]plugin.SchedulerJob{
			"job5": job5, "job14": job14, "job15": job15,
		},
	}
}

func TestSelectSuperPodForMindIEJob(t *testing.T) {
	tests := selectSuperPodForMindIEJobTestCases01()
	tests = append(tests, selectSuperPodForMindIEJobTestCases())
	plg := &module910SuperPod{}
	plg.NPUJob = &util.NPUJob{}
	plg.FrameAttr = plugin.VolcanoFrame{
		ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
			SuperPodSize:   superPodSize10,
			ReservePodSize: reservePodSize2,
		}}}
	plg.spBlock = 2
	task := mockTask()
	for _, cs := range tests {
		t.Run(cs.name, func(t *testing.T) {
			plg.Nodes = cs.totalNodes
			plg.NPUTaskNum = cs.npuTaskNum
			plg.Annotation = cs.annotations
			plg.Jobs = cs.jobs
			selectedNodes, err := plg.selectSuperPodForMindIEJob(task, cs.nodes)
			if !reflect.DeepEqual(err, cs.wantErr) {
				t.Errorf("SelectSuperPodForMindIEJob() error = %v, wantErr %v", err, cs.wantErr)
			}
			if !reflect.DeepEqual(getSelectedNodesSuperPodID(selectedNodes), cs.want) {
				t.Errorf("SelectSuperPodForMindIEJob() selectedNodes = %v, want: %v", selectedNodes, cs.want)
			}
		})

	}
}
