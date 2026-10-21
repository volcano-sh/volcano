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
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestPredicateForPreemptAction covers Session.PredicateForPreemptAction in its
// entirety against a realistic tier layout. The tier mirrors production: the
// predicate plugins register predicate fns (resourcefit, predicates), while a
// plugin like nodeorder registers none and is skipped. A node stays a preemption
// candidate unless some plugin reports an UnschedulableAndUnresolvable (or
// Error/Skip/Wait) failure; a resolvable Unschedulable failure must not stop the
// tier walk and mask a later unresolvable one.
func TestPredicateForPreemptAction(t *testing.T) {
	task := &api.TaskInfo{Name: "preemptor"}
	node := &api.NodeInfo{Name: "n1"}

	resolvable := func(*api.TaskInfo, *api.NodeInfo) error {
		return api.NewFitErrWithStatus(task, node,
			&api.Status{Code: api.Unschedulable, Reason: "insufficient resources"})
	}
	unresolvable := func(*api.TaskInfo, *api.NodeInfo) error {
		return api.NewFitErrWithStatus(task, node,
			&api.Status{Code: api.UnschedulableAndUnresolvable, Reason: "node taint is not tolerated"})
	}

	newSession := func(predicateFns map[string]api.PredicateFn) *Session {
		enabled := true
		ssn := &Session{
			Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
				{Name: "resourcefit", EnabledPredicate: &enabled},
				{Name: "predicates", EnabledPredicate: &enabled},
				// Non-predicate plugins (nodeorder, proportion, ...) appear in a
				// real tier without a registered predicate fn and must be skipped.
				{Name: "nodeorder", EnabledPredicate: &enabled},
			}}},
			predicateFns: map[string]api.PredicateFn{},
		}
		for name, fn := range predicateFns {
			if fn != nil {
				ssn.AddPredicateFn(name, fn)
			}
		}
		return ssn
	}

	tests := []struct {
		name          string
		resourcefitFn api.PredicateFn
		predicatesFn  api.PredicateFn
		wantErr       bool
	}{
		{
			name:          "all plugins pass, node stays a candidate",
			resourcefitFn: nil,
			predicatesFn:  nil,
		},
		{
			name:          "only resolvable failures, node stays a candidate",
			resourcefitFn: resolvable,
		},
		{
			// The core fix: a later unresolvable failure must not be masked by an
			// earlier resolvable one.
			name:          "later unresolvable is not masked by an earlier resolvable failure",
			resourcefitFn: resolvable,
			predicatesFn:  unresolvable,
			wantErr:       true,
		},
		{
			name:          "unresolvable first excludes the node identically",
			resourcefitFn: unresolvable,
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ssn := newSession(map[string]api.PredicateFn{
				"resourcefit": tc.resourcefitFn,
				"predicates":  tc.predicatesFn,
			})

			err := ssn.PredicateForPreemptAction(task, node)
			if !tc.wantErr {
				assert.NoError(t, err, "node should stay a preemption candidate")
				return
			}
			fe, ok := err.(*api.FitError)
			if !assert.True(t, ok, "want FitError, got: %v", err) {
				return
			}
			assert.True(t, fe.Status.ContainsUnschedulableAndUnresolvable(),
				"an unresolvable failure must exclude the node from preemption candidates, got statuses: %v", fe.Status)
		})
	}
}

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
			Name:      "all are helpful for preemption",
			PodGroups: []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue)},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
			status: map[api.TaskID]*api.FitError{},
			want:   map[api.TaskID][]string{"c1-p2": {"n1", "n2"}, "c1-p1": {"n1", "n2"}},
		},
		{
			Name:      "master predicate failed: node selector does not match",
			PodGroups: []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue)},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes:  []*v1.Node{util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"})},
			Queues: []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
			status: map[api.TaskID]*api.FitError{"c1-p1": newFitErr("c1-p1", "n1", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable})},
			want:   map[api.TaskID][]string{"c1-p2": {"n1"}, "c1-p1": {}},
		},
		{
			Name:      "p1,p3 has node fit error",
			PodGroups: []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"master": 1, "worker": 1}, schedulingv1.PodGroupInqueue)},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p0", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
				util.BuildNode("n2", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
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
