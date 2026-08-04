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

package hintprovider

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestProportionHints(t *testing.T) {
	// The rejected Job requests CPU only; releases confined to other dimensions
	// must not wake it, but any Queue redistribution input change must.
	job := &api.JobInfo{Queue: "rejected"}
	job.PodGroup = &api.PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
		MinResources: &v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
	}}}
	rejection := api.Rejection{Source: api.RejectionEnqueue}

	queue := func(state scheduling.QueueState, weight int32, capability string) *scheduling.Queue {
		q := &scheduling.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "any"},
			Status:     scheduling.QueueStatus{State: state},
		}
		q.Spec.Weight = weight
		if capability != "" {
			q.Spec.Capability = v1.ResourceList{v1.ResourceCPU: resource.MustParse(capability)}
		}
		return q
	}
	pod := func(cpu, memory string) *v1.Pod {
		return &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{
			Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse(cpu),
				v1.ResourceMemory: resource.MustParse(memory),
			}},
		}}}}
	}
	podGroup := func(minResources v1.ResourceList, phase scheduling.PodGroupPhase) *api.PodGroup {
		pg := &api.PodGroup{PodGroup: scheduling.PodGroup{
			Status: scheduling.PodGroupStatus{Phase: phase},
		}}
		if minResources != nil {
			pg.Spec.MinResources = &minResources
		}
		return pg
	}
	cpu := v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}
	memory := v1.ResourceList{v1.ResourceMemory: resource.MustParse("1Gi")}

	tests := []struct {
		name   string
		hintFn api.JobHintFn
		oldObj any
		newObj any
		want   api.HintResult
	}{
		{name: "queue weight change wakes any rejected Job", hintFn: queueHint, oldObj: queue(scheduling.QueueStateOpen, 1, ""), newObj: queue(scheduling.QueueStateOpen, 2, ""), want: api.HintWakeup},
		{name: "queue capability change wakes any rejected Job", hintFn: queueHint, oldObj: queue(scheduling.QueueStateOpen, 1, "1"), newObj: queue(scheduling.QueueStateOpen, 1, "2"), want: api.HintWakeup},
		{name: "queue deletion returns share to the pool", hintFn: queueHint, oldObj: queue(scheduling.QueueStateOpen, 1, "1"), want: api.HintWakeup},
		{name: "metadata-only queue update is skipped", hintFn: queueHint, oldObj: queue(scheduling.QueueStateOpen, 1, "1"), newObj: queue(scheduling.QueueStateOpen, 1, "1"), want: api.HintSkip},
		{name: "pod deletion freeing CPU wakes Job", hintFn: podHint, oldObj: pod("1", "1Gi"), want: api.HintWakeup},
		{name: "pod releasing only memory is skipped", hintFn: podHint, oldObj: pod("1", "2Gi"), newObj: pod("1", "1Gi"), want: api.HintSkip},
		{name: "consuming PodGroup releasing CPU wakes Job", hintFn: podGroupHint, oldObj: podGroup(cpu, scheduling.PodGroupRunning), newObj: podGroup(cpu, scheduling.PodGroupCompleted), want: api.HintWakeup},
		{name: "PodGroup releasing only memory is skipped", hintFn: podGroupHint, oldObj: podGroup(memory, scheduling.PodGroupRunning), newObj: podGroup(memory, scheduling.PodGroupCompleted), want: api.HintSkip},
		{name: "non-consuming PodGroup deletion is skipped", hintFn: podGroupHint, oldObj: podGroup(cpu, scheduling.PodGroupPending), want: api.HintSkip},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.hintFn(job, rejection, test.oldObj, test.newObj)
			if err != nil {
				t.Fatalf("hint returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("hint result = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEventsToRegister(t *testing.T) {
	want := []api.ClusterEvent{
		{Resource: api.QueueEvent, ActionType: fwk.Update | fwk.Delete},
		{Resource: api.PodGroupEvent, ActionType: fwk.Update | fwk.Delete},
		{Resource: fwk.Node, ActionType: fwk.Add | fwk.UpdateNodeAllocatable},
		{Resource: fwk.Pod, ActionType: fwk.Delete | fwk.UpdatePodScaleDown},
	}

	events, err := (&Provider{}).EventsToRegister(t.Context())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	if len(events) != len(want) {
		t.Fatalf("EventsToRegister() returned %d events, want %d", len(events), len(want))
	}
	for i := range want {
		t.Run(string(want[i].Resource), func(t *testing.T) {
			if events[i].Event != want[i] {
				t.Fatalf("event = %#v, want %#v", events[i].Event, want[i])
			}
			if events[i].HintFn == nil {
				t.Fatal("proportion event should filter with a hint function")
			}
		})
	}
}
