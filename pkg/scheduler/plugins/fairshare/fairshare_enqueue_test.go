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

package fairshare

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildJob(minCPU string, taskCPU ...string) *api.JobInfo {
	tasks := make([]*api.TaskInfo, 0, len(taskCPU))
	for i, cpu := range taskCPU {
		tasks = append(tasks, &api.TaskInfo{
			UID:    api.TaskID(fmt.Sprintf("t%d", i)),
			Job:    "ns/j",
			Resreq: api.NewResource(api.BuildResourceList(cpu, "1G")),
		})
	}

	job := api.NewJobInfo("ns/j", tasks...)
	pg := &api.PodGroup{}
	if minCPU != "" {
		minResources := api.BuildResourceList(minCPU, "1G")
		pg.Spec.MinResources = &minResources
	}
	job.SetPodGroup(pg)

	return job
}

func TestJobTotalResource(t *testing.T) {
	tests := []struct {
		name    string
		minCPU  string
		taskCPU []string
		want    float64
	}{
		{
			name:    "sums tasks and ignores MinResources when tasks exist",
			minCPU:  "10",
			taskCPU: []string{"1", "2"},
			want:    3000,
		},
		{
			name:   "falls back to MinResources when the job has no tasks",
			minCPU: "2",
			want:   2000,
		},
		{
			name: "zero when the job has neither tasks nor MinResources",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := buildJob(tt.minCPU, tt.taskCPU...)
			if got := jobTotalResource(job, v1.ResourceCPU); got != tt.want {
				t.Errorf("jobTotalResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNamespaceDemand(t *testing.T) {
	options.Default()
	oneCPU := api.BuildResourceList("1", "1G")

	tests := []struct {
		name       string
		podGroups  []*schedulingv1.PodGroup
		pods       []*v1.Pod
		wantDemand float64
	}{
		{
			name: "pending job with pods counts its pending tasks",
			podGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithMinResources("pg1", "ns", "q1", 1, nil, oneCPU, schedulingv1.PodGroupPending),
			},
			pods: []*v1.Pod{
				util.BuildPod("ns", "p1", "", v1.PodPending, oneCPU, "pg1", nil, nil),
			},
			wantDemand: 1000,
		},
		{
			name: "pending job without pods counts MinResources",
			podGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithMinResources("pg1", "ns", "q1", 1, nil, oneCPU, schedulingv1.PodGroupPending),
			},
			wantDemand: 1000,
		},
		{
			name: "admitted job without pods is not counted",
			podGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithMinResources("pg1", "ns", "q1", 1, nil, oneCPU, schedulingv1.PodGroupInqueue),
			},
			wantDemand: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer saveAndResetState(t)()

			test := uthelper.TestCommonStruct{
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: tt.podGroups,
				Pods:      tt.pods,
				Nodes:     []*v1.Node{util.BuildNode("n1", api.BuildResourceList("4", "4G"), nil)},
				Queues:    []*schedulingv1.Queue{util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G"))},
			}
			ssn := test.RegisterSession(nil, nil)
			defer test.Close()

			fsp := New(framework.Arguments{
				"fairshare.targetQueues": "q1",
				"fairshare.resourceKey":  "cpu",
			}).(*fairSharePlugin)
			fsp.OnSessionOpen(ssn)

			if got := fsp.queues["q1"].namespaceDemand["ns"]; got != tt.wantDemand {
				t.Errorf("namespaceDemand[ns] = %v, want %v", got, tt.wantDemand)
			}
		})
	}
}

func TestEnqueueGate(t *testing.T) {
	options.Default()

	trueValue := true
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:               PluginName,
		EnabledJobOrder:    &trueValue,
		EnabledJobEnqueued: &trueValue,
		Arguments: map[string]interface{}{
			"fairshare.targetQueues":      "q1",
			"fairshare.resourceKey":       "cpu",
			"fairshare.enableEnqueueGate": "true",
		},
	}}}}

	oneCPU := api.BuildResourceList("1", "1G")
	runningJobs := []string{"a1", "a2", "a3", "a4"}

	basePodGroups := func() []*schedulingv1.PodGroup {
		pgs := make([]*schedulingv1.PodGroup, 0, len(runningJobs)+2)
		for _, n := range runningJobs {
			pgs = append(pgs, util.BuildPodGroupWithMinResources(n, "ns-a", "q1", 1, nil, oneCPU, schedulingv1.PodGroupRunning))
		}
		return append(pgs,
			util.BuildPodGroupWithMinResources("a5", "ns-a", "q1", 1, nil, oneCPU, schedulingv1.PodGroupPending),
			util.BuildPodGroupWithMinResources("b1", "ns-b", "q1", 1, nil, oneCPU, schedulingv1.PodGroupPending),
		)
	}
	runningPods := func() []*v1.Pod {
		pods := make([]*v1.Pod, 0, len(runningJobs)+2)
		for _, n := range runningJobs {
			pods = append(pods, util.BuildPod("ns-a", "p-"+n, "n1", v1.PodRunning, oneCPU, n, nil, nil))
		}
		return pods
	}
	nodes := func() []*v1.Node { return []*v1.Node{util.BuildNode("n1", api.BuildResourceList("4", "4G"), nil)} }
	queues := func() []*schedulingv1.Queue {
		return []*schedulingv1.Queue{util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G"))}
	}
	expect := map[api.JobID]scheduling.PodGroupPhase{
		"ns-a/a5": scheduling.PodGroupPending,
		"ns-b/b1": scheduling.PodGroupInqueue,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name:      "pending jobs already have pods",
			PodGroups: basePodGroups(),
			Pods: append(runningPods(),
				util.BuildPod("ns-a", "p-a5", "", v1.PodPending, oneCPU, "a5", nil, nil),
				util.BuildPod("ns-b", "p-b1", "", v1.PodPending, oneCPU, "b1", nil, nil),
			),
			Nodes:        nodes(),
			Queues:       queues(),
			ExpectStatus: expect,
		},
		{
			Name:         "pending jobs have no pods yet (vcjob)",
			PodGroups:    basePodGroups(),
			Pods:         runningPods(),
			Nodes:        nodes(),
			Queues:       queues(),
			ExpectStatus: expect,
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			defer saveAndResetState(t)()
			test.Plugins = map[string]framework.PluginBuilder{PluginName: New}
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{enqueue.New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
