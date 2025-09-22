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

package framework

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func newFitErr(taskName, nodeName string, sts ...*api.Status) *api.FitError {
	return api.NewFitErrWithStatus(&api.TaskInfo{Name: taskName}, &api.NodeInfo{Name: nodeName}, sts...)
}

func TestFilterOutPreemptMayNotHelpNodes(t *testing.T) {
	tests := []struct {
		Name      string
		PodGroups []*schedulingv1.PodGroup
		Pods      []*v1.Pod
		Nodes     []*v1.Node
		Queues    []*schedulingv1.Queue
		status    map[api.TaskID]*api.FitError
		want      map[api.TaskID][]string // task's nodes name list which is helpful for preemption
	}{
		{
			Name: "all are helpful for preemption",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(1).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupInqueue).
					Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod().
					Namespace("c1").
					Name("p1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("2", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "master"}).
					NodeSelector(nil).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("2", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
					NodeSelector(make(map[string]string)).
					Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode().
					Name("n1").
					Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Annotations(map[string]string{}).
					Labels(map[string]string{"nodeRole": "worker"}).
					Obj(),

				util.MakeNode().
					Name("n2").
					Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Annotations(map[string]string{}).
					Labels(map[string]string{"nodeRole": "worker"}).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj(),
			},
			status: map[api.TaskID]*api.FitError{},
			want:   map[api.TaskID][]string{"c1-p2": {"n1", "n2"}, "c1-p1": {"n1", "n2"}},
		},
		{
			Name: "master predicate failed: node selector does not match",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(1).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupInqueue).
					Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod().
					Namespace("c1").
					Name("p1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("2", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "master"}).
					NodeSelector(map[string]string{"nodeRole": "master"}).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("2", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
					NodeSelector(map[string]string{"nodeRole": "worker"}).
					Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode().
					Name("n1").
					Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Annotations(map[string]string{}).
					Labels(map[string]string{"nodeRole": "worker"}).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj(),
			},
			status: map[api.TaskID]*api.FitError{"c1-p1": newFitErr("c1-p1", "n1", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable})},
			want:   map[api.TaskID][]string{"c1-p2": {"n1"}, "c1-p1": {}},
		},
		{
			Name: "p1,p3 has node fit error",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(2).
					MinTaskMember(map[string]int32{"master": 1, "worker": 1}).
					Phase(schedulingv1.PodGroupInqueue).
					Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod().
					Namespace("c1").
					Name("p0").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "master"}).
					NodeSelector(map[string]string{"nodeRole": "master"}).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "master"}).
					NodeSelector(map[string]string{"nodeRole": "master"}).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
					NodeSelector(map[string]string{"nodeRole": "worker"}).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p3").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
					NodeSelector(map[string]string{"nodeRole": "worker"}).
					Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode().
					Name("n1").
					Allocatable(api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Annotations(map[string]string{}).
					Labels(map[string]string{"nodeRole": "master"}).
					Obj(),

				util.MakeNode().
					Name("n2").
					Allocatable(api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Annotations(map[string]string{}).
					Labels(map[string]string{"nodeRole": "worker"}).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj(),
			},
			status: map[api.TaskID]*api.FitError{
				"c1-p1": newFitErr("c1-p1", "n2", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable}),
				"c1-p3": newFitErr("c1-p3", "n1", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable}),
			},
			// notes that are useful for preempting
			want: map[api.TaskID][]string{
				"c1-p0": {"n1", "n2"},
				"c1-p1": {"n1"},
				"c1-p2": {"n1", "n2"},
				"c1-p3": {"n2"},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			scherCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
			for _, node := range test.Nodes {
				scherCache.AddOrUpdateNode(node)
			}
			for _, pod := range test.Pods {
				scherCache.AddPod(pod)
			}
			for _, pg := range test.PodGroups {
				scherCache.AddPodGroupV1beta1(pg)
			}
			for _, queue := range test.Queues {
				scherCache.AddQueueV1beta1(queue)
			}
			ssn := OpenSession(scherCache, nil, nil)
			defer CloseSession(ssn)
			for _, job := range ssn.Jobs {
				for _, task := range job.TaskStatusIndex[api.Pending] {
					if fitErr, exist := test.status[task.UID]; exist {
						fe := api.NewFitErrors()
						fe.SetNodeError(fitErr.NodeName, fitErr)
						job.NodesFitErrors[task.UID] = fe
					}

					// check potential nodes
					potentialNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
					want := test.want[task.UID]
					got := make([]string, 0, len(potentialNodes))
					for _, node := range potentialNodes {
						got = append(got, node.Name)
					}
					assert.Equal(t, want, got, fmt.Sprintf("case %d: task %s", i, task.UID))
				}
			}
		})
	}
}
