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

package priority

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/preempt"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	options.Default()
}

var (
	trueValue         = true
	falseValue        = false
	priority          = int32(1000)
	pluginEnableEvict = conf.PluginOption{
		Name:               PluginName,
		EnabledTaskOrder:   &trueValue,
		EnabledJobOrder:    &trueValue,
		EnabledPreemptable: &trueValue,
		EnabledReclaimable: &trueValue,
		EnabledJobStarving: &trueValue,
	}
	pluginDisableEvict = conf.PluginOption{
		Name:               PluginName,
		EnabledTaskOrder:   &trueValue,
		EnabledJobOrder:    &trueValue,
		EnabledPreemptable: &falseValue,
		EnabledReclaimable: &falseValue,
		EnabledJobStarving: &trueValue,
	}
)

func TestPreempt(t *testing.T) {
	actions := []framework.Action{allocate.New(), preempt.New()}
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	tests := []struct {
		uthelper.TestCommonStruct
		actions []framework.Action
		tiers   []conf.Tier
	}{
		{
			actions: actions,
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{pluginEnableEvict},
				},
			},
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "enable preempt low priority: enablePreemptable:true",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
				},
				Pods: []*v1.Pod{ // as preemptee victims are searched by node, priority can not be guaranteed cross nodes
					util.BuildPod("ns1", "preemptee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPodWithPriority("ns1", "preemptee2", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string), &priority),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
				},
				ExpectEvicted:   []string{"ns1/preemptee1"},
				ExpectEvictNum:  1,
				ExpectPipeLined: map[string][]string{"ns2/pg2": {"node1"}},
			},
		},
		{
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{pluginDisableEvict},
				},
			},
			actions: actions,
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "disable preempt low priority: enablePreemptable:false",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "preemptee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPod("ns1", "preemptee2", "node2", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "false"}, make(map[string]string)),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
				},
				ExpectEvicted:   []string{},
				ExpectEvictNum:  0,
				ExpectPipeLined: map[string][]string{},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(test.tiers, nil)
			defer test.Close()
			test.Run(test.actions)
			err := test.CheckAll(i)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Helper function to create api.TaskInfo with specific parameters
func createTask(name, namespace, rankValue string, suffixes ...string) *api.TaskInfo {
	podName := name
	if len(suffixes) > 0 {
		name = name + suffixes[0]
		podName = name
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
	}

	if rankValue != "" {
		pod.Spec.Containers = []v1.Container{
			{
				Env: []v1.EnvVar{
					{
						Name:  "RANK",
						Value: rankValue,
					},
				},
			},
		}
	}

	return &api.TaskInfo{
		Name:      name,
		Namespace: namespace,
		Pod:       pod,
	}
}

// TestTaskOrderByRank tests ranking comparison logic
func TestTaskOrderByRank(t *testing.T) {
	// Test cases for rank comparison
	testCases := []struct {
		name     string
		a        *api.TaskInfo
		b        *api.TaskInfo
		expected int
	}{
		{
			name:     "Both have valid numeric ranks - a < b",
			a:        createTask("task1", "ns", "5"),
			b:        createTask("task2", "ns", "10"),
			expected: api.OrderingLess,
		},
		{
			name:     "Valid vs invalid rank",
			a:        createTask("task1", "ns", "5"),
			b:        createTask("task2", "ns", "invalid"),
			expected: api.OrderingGreater,
		},
		{
			name:     "No ranks specified",
			a:        createTask("task1", "ns", ""),
			b:        createTask("task2", "ns", ""),
			expected: api.OrderingEqual,
		},
		{
			name:     "Mixed valid and empty ranks",
			a:        createTask("task1", "ns", "3"),
			b:        createTask("task2", "ns", ""),
			expected: api.OrderingGreater,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compareTaskByRank(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestTaskOrderByNameSuffix tests name suffix comparison logic
func TestTaskOrderByNameSuffix(t *testing.T) {
	testCases := []struct {
		name     string
		a        *api.TaskInfo
		b        *api.TaskInfo
		expected int
	}{
		{
			name:     "A has launcher suffix",
			a:        createTask("task", "ns", "", "-launcher"),
			b:        createTask("task", "ns", "", "-worker-0"),
			expected: api.OrderingLess,
		},
		{
			name:     "B has master-0 suffix",
			a:        createTask("task", "ns", "", "-worker-1"),
			b:        createTask("task", "ns", "", "-master-0"),
			expected: api.OrderingGreater,
		},
		{
			name:     "Both have numeric suffixes",
			a:        createTask("task", "ns", "", "-worker-0"),
			b:        createTask("task", "ns", "", "-worker-1"),
			expected: api.OrderingLess,
		},
		{
			name:     "Complex numeric comparison",
			a:        createTask("task", "ns", "", "-worker-10"),
			b:        createTask("task", "ns", "", "-worker-2"),
			expected: api.OrderingGreater,
		},
		{
			name:     "No numeric suffixes",
			a:        createTask("task", "ns", "", "-worker"),
			b:        createTask("task", "ns", "", "-worker"),
			expected: api.OrderingEqual,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compareTaskByNameSuffix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestCompareNumberStr tests numeric string comparison
func TestCompareNumberStr(t *testing.T) {
	testCases := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{"Valid numbers", "5", "10", api.OrderingLess},
		{"Invalid vs valid", "abc", "5", api.OrderingLess},
		{"Valid vs invalid", "5", "abc", api.OrderingGreater},
		{"Both invalid", "foo", "bar", api.OrderingEqual},
		{"Empty strings", "", "", api.OrderingEqual},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compareNumberStr(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestExtractTrailingNumber tests number extraction from names
func TestExtractTrailingNumber(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"worker-23", "23"},
		{"master-0", "0"},
		{"launcher", ""},
		{"worker-1-2", "2"},
		{"invalid-format", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := extractTrailingNumber(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
