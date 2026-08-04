/*
Copyright 2026 The Volcano Authors.

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

package hintutil

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestNodeHint(t *testing.T) {
	node := func(cpu, memory string) *v1.Node {
		return &v1.Node{Status: v1.NodeStatus{Allocatable: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse(cpu),
			v1.ResourceMemory: resource.MustParse(memory),
		}}}
	}
	taskRequest := api.NewResource(v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")})
	task := &api.TaskInfo{UID: "task", Resreq: taskRequest.Clone(), InitResreq: taskRequest}
	taskJob := api.NewJobInfo("job", task)
	taskRejection := api.Rejection{Source: api.RejectionAllocatable, Tasks: []api.TaskID{task.UID}}
	enqueueJob := api.NewJobInfo("enqueue-job")
	enqueueJob.PodGroup = &api.PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
		MinResources: &v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
	}}}

	tests := []struct {
		name      string
		job       *api.JobInfo
		rejection api.Rejection
		oldNode   *v1.Node
		newNode   *v1.Node
		want      api.HintResult
	}{
		{name: "Node Add with requested CPU wakes task rejection", job: taskJob, rejection: taskRejection, newNode: node("1", "1Gi"), want: api.HintWakeup},
		{name: "requested CPU increase wakes task rejection", job: taskJob, rejection: taskRejection, oldNode: node("1", "1Gi"), newNode: node("2", "1Gi"), want: api.HintWakeup},
		{name: "requested CPU decrease is skipped", job: taskJob, rejection: taskRejection, oldNode: node("2", "1Gi"), newNode: node("1", "1Gi"), want: api.HintSkip},
		{name: "unrequested memory increase is skipped", job: taskJob, rejection: taskRejection, oldNode: node("1", "1Gi"), newNode: node("1", "2Gi"), want: api.HintSkip},
		{name: "MinResources CPU increase wakes enqueue rejection", job: enqueueJob, rejection: api.Rejection{Source: api.RejectionEnqueue}, oldNode: node("1", "1Gi"), newNode: node("2", "1Gi"), want: api.HintWakeup},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NodeHint(test.job, test.rejection, test.oldNode, test.newNode)
			if err != nil {
				t.Fatalf("NodeHint() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("NodeHint() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUsageReleasedForRequest(t *testing.T) {
	request := api.NewResource(v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")})
	usage := func(cpu, memory string) *api.Resource {
		return api.NewResource(v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse(cpu),
			v1.ResourceMemory: resource.MustParse(memory),
		})
	}

	tests := []struct {
		name     string
		oldUsage *api.Resource
		newUsage *api.Resource
		want     bool
	}{
		{name: "requested CPU released", oldUsage: usage("2", "1Gi"), newUsage: usage("1", "1Gi"), want: true},
		{name: "full release on deletion", oldUsage: usage("2", "1Gi"), newUsage: api.EmptyResource(), want: true},
		{name: "only unrequested memory released", oldUsage: usage("1", "2Gi"), newUsage: usage("1", "1Gi"), want: false},
		{name: "no release", oldUsage: usage("1", "1Gi"), newUsage: usage("1", "1Gi"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := UsageReleasedForRequest(request, test.oldUsage, test.newUsage); got != test.want {
				t.Fatalf("UsageReleasedForRequest() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPodGroupReleasedQuota(t *testing.T) {
	pg := func(queue string, phase scheduling.PodGroupPhase) *api.PodGroup {
		return &api.PodGroup{PodGroup: scheduling.PodGroup{
			Spec:   scheduling.PodGroupSpec{Queue: queue},
			Status: scheduling.PodGroupStatus{Phase: phase},
		}}
	}

	tests := []struct {
		name  string
		oldPg *api.PodGroup
		newPg *api.PodGroup
		want  bool
	}{
		{name: "leaves consuming phase", oldPg: pg("q", scheduling.PodGroupRunning), newPg: pg("q", scheduling.PodGroupCompleted), want: true},
		{name: "moves to another queue", oldPg: pg("q", scheduling.PodGroupRunning), newPg: pg("other", scheduling.PodGroupRunning), want: true},
		{name: "stays consuming in same queue", oldPg: pg("q", scheduling.PodGroupRunning), newPg: pg("q", scheduling.PodGroupInqueue), want: false},
		{name: "was not consuming", oldPg: pg("q", scheduling.PodGroupPending), newPg: pg("q", scheduling.PodGroupRunning), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PodGroupReleasedQuota(test.oldPg, test.newPg); got != test.want {
				t.Fatalf("PodGroupReleasedQuota() = %v, want %v", got, test.want)
			}
		})
	}
}
