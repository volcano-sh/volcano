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

package gang

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func gangTier() []conf.Tier {
	trueVal := true
	return []conf.Tier{{
		Plugins: []conf.PluginOption{{
			Name:                   PluginName,
			EnabledJobReady:        &trueVal,
			EnabledJobPipelined:    &trueVal,
			EnabledSubJobReady:     &trueVal,
			EnabledSubJobPipelined: &trueVal,
		}},
	}}
}

func hasCondition(conditions []scheduling.PodGroupCondition, condType scheduling.PodGroupConditionType) bool {
	for _, c := range conditions {
		if c.Type == condType && c.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func TestOnSessionCloseConditions(t *testing.T) {
	resource := api.BuildResourceList("1", "1G")

	tests := []struct {
		name                string
		podGroup            *schedulingv1.PodGroup
		pods                []*v1.Pod
		setupJob            func(*api.JobInfo)
		expectUnschedulable bool
		expectScheduled     bool
	}{
		{
			name:     "inqueue job waiting for gang should not set unschedulable",
			podGroup: util.BuildPodGroup("pg1", "c1", "c1", 2, nil, schedulingv1.PodGroupInqueue),
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, resource, "pg1", nil, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, resource, "pg1", nil, nil),
			},
			expectUnschedulable: false,
			expectScheduled:     false,
		},
		{
			name:     "inqueue job with fit errors should set unschedulable",
			podGroup: util.BuildPodGroup("pg1", "c1", "c1", 2, nil, schedulingv1.PodGroupInqueue),
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, resource, "pg1", nil, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, resource, "pg1", nil, nil),
			},
			setupJob: func(job *api.JobInfo) {
				for _, task := range job.TaskStatusIndex[api.Pending] {
					fe := api.NewFitErrors()
					fe.SetNodeError("n1", api.NewFitError(task, &api.NodeInfo{Name: "n1"}, "resource fit failed"))
					job.NodesFitErrors[task.UID] = fe
					break
				}
			},
			expectUnschedulable: true,
			expectScheduled:     false,
		},
		{
			name:     "pending job should set unschedulable",
			podGroup: util.BuildPodGroup("pg1", "c1", "c1", 2, nil, schedulingv1.PodGroupPending),
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, resource, "pg1", nil, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, resource, "pg1", nil, nil),
			},
			expectUnschedulable: true,
			expectScheduled:     false,
		},
		{
			name:     "ready job should set scheduled",
			podGroup: util.BuildPodGroup("pg1", "c1", "c1", 2, nil, schedulingv1.PodGroupInqueue),
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "n1", v1.PodRunning, resource, "pg1", nil, nil),
				util.BuildPod("c1", "p2", "n1", v1.PodRunning, resource, "pg1", nil, nil),
			},
			expectUnschedulable: false,
			expectScheduled:     true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := &uthelper.TestCommonStruct{
				Name:      tt.name,
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{tt.podGroup},
				Pods:      tt.pods,
				Queues:    []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
			}

			ssn := test.RegisterSession(gangTier(), nil)
			defer test.Close()

			for _, job := range ssn.Jobs {
				if tt.setupJob != nil {
					tt.setupJob(job)
				}
			}

			plugin := New(nil)
			plugin.OnSessionClose(ssn)

			var checked bool
			for _, job := range ssn.Jobs {
				checked = true
				assert.Equal(t, tt.expectUnschedulable, hasCondition(job.PodGroup.Status.Conditions, scheduling.PodGroupUnschedulableType),
					"case %d(%s) unschedulable condition mismatch for job %s", i, tt.name, job.Name)
				assert.Equal(t, tt.expectScheduled, hasCondition(job.PodGroup.Status.Conditions, scheduling.PodGroupScheduled),
					"case %d(%s) scheduled condition mismatch for job %s", i, tt.name, job.Name)
			}
			if !checked {
				t.Fatalf("case %d(%s): no jobs found in session", i, tt.name)
			}
		})
	}
}
