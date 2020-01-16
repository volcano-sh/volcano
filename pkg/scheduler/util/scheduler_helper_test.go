/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"reflect"
	"testing"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestHasEnoughResourceForJob(t *testing.T) {
	cases := []struct {
		job *api.JobInfo
		nodes []*api.NodeInfo
		expectedResult bool
	}{
		{
			job: &api.JobInfo{
				Name: "job1",
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task1": buildTaskInfoObj("task1", 2000, 4000),
					"task2": buildTaskInfoObj("task2", 2000, 4000),
				},
			},
			nodes: []*api.NodeInfo{
				buildNodeInfoObj("node1", 2000, 4000),
				buildNodeInfoObj("node2", 2000, 4000),
			},
			expectedResult: true,
		},
		{
			job: &api.JobInfo{
				Name: "job1",
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task1": buildTaskInfoObj("task1", 4000, 4000),
					"task2": buildTaskInfoObj("task2", 4000, 4000),
				},
			},
			nodes: []*api.NodeInfo{
				buildNodeInfoObj("node1", 2000, 4000),
				buildNodeInfoObj("node2", 2000, 4000),
				buildNodeInfoObj("node3", 2000, 4000),
				buildNodeInfoObj("node4", 2000, 4000),
				buildNodeInfoObj("node5", 2000, 4000),
			},
			expectedResult: false,
		},
		{
			job: &api.JobInfo{
				Name: "job1",
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task1": buildTaskInfoObj("task1", 1500, 2000),
					"task2": buildTaskInfoObj("task1", 1000, 2000),
					"task3": buildTaskInfoObj("task1", 1000, 2000),
				},
			},
			nodes: []*api.NodeInfo{
				buildNodeInfoObj("node1", 2000, 4000),
				buildNodeInfoObj("node2", 1800, 4000),
			},
			expectedResult: true,
		},
	}

	for i, test := range cases {
		result := HasEnoughResourceForJob(test.job, test.nodes)
		if result != test.expectedResult {
			t.Errorf("Failed test case #%d, expected: %#v, got %#v", i, test.expectedResult, result)
		}
	}
}

// buildApiResource builds task resource object
func buildTaskInfoObj(name string, cpu, memory float64) *api.TaskInfo {
	resource := buildApiResource(cpu, memory)
	return &api.TaskInfo{
		Name:        name,
		Resreq:      resource,
		InitResreq:  resource,
	}
}

func buildNodeInfoObj(name string, cpu, memory float64) *api.NodeInfo {
	resource := buildApiResource(cpu, memory)
	return &api.NodeInfo{
		Name:        name,
		Idle:      resource,
		Used:  api.EmptyResource(),
	}
}

func buildApiResource(cpu, memory float64) *api.Resource {
	return &api.Resource{
		MilliCPU:     cpu,
		Memory:   memory,
		MaxTaskNum: 1,
	}
}

func TestSelectBestNode(t *testing.T) {
	cases := []struct {
		NodeScores map[float64][]*api.NodeInfo
		// Expected node is one of ExpectedNodes
		ExpectedNodes []*api.NodeInfo
	}{
		{
			NodeScores: map[float64][]*api.NodeInfo{
				1.0: {&api.NodeInfo{Name: "node1"}, &api.NodeInfo{Name: "node2"}},
				2.0: {&api.NodeInfo{Name: "node3"}, &api.NodeInfo{Name: "node4"}},
			},
			ExpectedNodes: []*api.NodeInfo{{Name: "node3"}, {Name: "node4"}},
		},
		{
			NodeScores: map[float64][]*api.NodeInfo{
				1.0: {&api.NodeInfo{Name: "node1"}, &api.NodeInfo{Name: "node2"}},
				3.0: {&api.NodeInfo{Name: "node3"}},
				2.0: {&api.NodeInfo{Name: "node4"}, &api.NodeInfo{Name: "node5"}},
			},
			ExpectedNodes: []*api.NodeInfo{{Name: "node3"}},
		},
	}

	oneOf := func(node *api.NodeInfo, nodes []*api.NodeInfo) bool {
		for _, v := range nodes {
			if reflect.DeepEqual(node, v) {
				return true
			}
		}
		return false
	}
	for i, test := range cases {
		result := SelectBestNode(test.NodeScores)
		if !oneOf(result, test.ExpectedNodes) {
			t.Errorf("Failed test case #%d, expected: %#v, got %#v", i, test.ExpectedNodes, result)
		}
	}
}
