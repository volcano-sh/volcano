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

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestCapacityHints(t *testing.T) {
	job := &api.JobInfo{Queue: "child"}
	rejection := api.Rejection{Queues: []api.QueueID{"child", "parent", "root"}}
	podGroup := func(queue string, phase scheduling.PodGroupPhase) *api.PodGroup {
		return &api.PodGroup{PodGroup: scheduling.PodGroup{
			Spec:   scheduling.PodGroupSpec{Queue: queue},
			Status: scheduling.PodGroupStatus{Phase: phase},
		}}
	}
	pod := func(queue string) *v1.Pod {
		return &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			batch.QueueNameKey: queue,
		}}}
	}
	queue := func(name string, state scheduling.QueueState, capability string) *scheduling.Queue {
		q := &scheduling.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status:     scheduling.QueueStatus{State: state},
		}
		if capability != "" {
			q.Spec.Capability = v1.ResourceList{v1.ResourceCPU: resource.MustParse(capability)}
		}
		return q
	}
	queueWithCPU := func(name string, state scheduling.QueueState, capability, guarantee, deserved string) *scheduling.Queue {
		q := queue(name, state, capability)
		if guarantee != "" {
			q.Spec.Guarantee.Resource = v1.ResourceList{v1.ResourceCPU: resource.MustParse(guarantee)}
		}
		if deserved != "" {
			q.Spec.Deserved = v1.ResourceList{v1.ResourceCPU: resource.MustParse(deserved)}
		}
		return q
	}

	tests := []struct {
		name   string
		hintFn api.JobHintFn
		oldObj any
		newObj any
		want   api.HintResult
	}{
		{name: "capability increase in ancestor scope wakes Job", hintFn: queueHint, oldObj: queue("parent", scheduling.QueueStateOpen, "1"), newObj: queue("parent", scheduling.QueueStateOpen, "2"), want: api.HintWakeup},
		{name: "capability decrease in scope is skipped", hintFn: queueHint, oldObj: queue("child", scheduling.QueueStateOpen, "2"), newObj: queue("child", scheduling.QueueStateOpen, "1"), want: api.HintSkip},
		{name: "removing capability limit wakes Job", hintFn: queueHint, oldObj: queue("child", scheduling.QueueStateOpen, "1"), newObj: queue("child", scheduling.QueueStateOpen, ""), want: api.HintWakeup},
		{name: "opening Queue wakes Job", hintFn: queueHint, oldObj: queue("child", scheduling.QueueStateClosed, "1"), newObj: queue("child", scheduling.QueueStateOpen, "1"), want: api.HintWakeup},
		{name: "guarantee increase wakes Job", hintFn: queueHint, oldObj: queueWithCPU("child", scheduling.QueueStateOpen, "2", "1", ""), newObj: queueWithCPU("child", scheduling.QueueStateOpen, "2", "2", ""), want: api.HintWakeup},
		{name: "deserved increase wakes Job", hintFn: queueHint, oldObj: queueWithCPU("child", scheduling.QueueStateOpen, "2", "", "1"), newObj: queueWithCPU("child", scheduling.QueueStateOpen, "2", "", "2"), want: api.HintWakeup},
		{name: "parent change wakes Job conservatively", hintFn: queueHint, oldObj: func() *scheduling.Queue {
			q := queue("child", scheduling.QueueStateOpen, "2")
			q.Spec.Parent = "parent"
			return q
		}(), newObj: func() *scheduling.Queue {
			q := queue("child", scheduling.QueueStateOpen, "2")
			q.Spec.Parent = "other-parent"
			return q
		}(), want: api.HintWakeup},
		{name: "weight and priority change is skipped", hintFn: queueHint, oldObj: func() *scheduling.Queue {
			q := queue("child", scheduling.QueueStateOpen, "2")
			q.Spec.Weight = 1
			q.Spec.Priority = 1
			return q
		}(), newObj: func() *scheduling.Queue {
			q := queue("child", scheduling.QueueStateOpen, "2")
			q.Spec.Weight = 2
			q.Spec.Priority = 2
			return q
		}(), want: api.HintSkip},
		{name: "status counters update in scope is skipped", hintFn: queueHint, oldObj: queue("child", scheduling.QueueStateOpen, "1"), newObj: func() *scheduling.Queue {
			q := queue("child", scheduling.QueueStateOpen, "1")
			q.Status.Running = 1
			return q
		}(), want: api.HintSkip},
		{name: "queue update outside scope is skipped", hintFn: queueHint, oldObj: queue("other", scheduling.QueueStateOpen, "1"), newObj: queue("other", scheduling.QueueStateOpen, "2"), want: api.HintSkip},
		{name: "PodGroup leaving consuming phase wakes Job", hintFn: podGroupHint, oldObj: podGroup("child", scheduling.PodGroupRunning), newObj: podGroup("child", scheduling.PodGroupCompleted), want: api.HintWakeup},
		{name: "PodGroup remaining in consuming phase is skipped", hintFn: podGroupHint, oldObj: podGroup("child", scheduling.PodGroupRunning), newObj: podGroup("child", scheduling.PodGroupInqueue), want: api.HintSkip},
		{name: "consuming PodGroup moving out of rejection path wakes Job", hintFn: podGroupHint, oldObj: podGroup("child", scheduling.PodGroupRunning), newObj: podGroup("other", scheduling.PodGroupRunning), want: api.HintWakeup},
		{name: "PodGroup release outside rejection path is skipped", hintFn: podGroupHint, oldObj: podGroup("other", scheduling.PodGroupRunning), newObj: podGroup("other", scheduling.PodGroupCompleted), want: api.HintSkip},
		{name: "consuming PodGroup deletion in rejection path wakes Job", hintFn: podGroupHint, oldObj: podGroup("parent", scheduling.PodGroupRunning), want: api.HintWakeup},
		{name: "non-consuming PodGroup deletion is skipped", hintFn: podGroupHint, oldObj: podGroup("other", scheduling.PodGroupPending), want: api.HintSkip},
		{name: "Pod deletion in rejection path wakes Job", hintFn: podHint, oldObj: pod("child"), want: api.HintWakeup},
		{name: "Pod deletion outside rejection path is skipped", hintFn: podHint, oldObj: pod("other"), want: api.HintSkip},
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

func TestCapacityEventsToRegister(t *testing.T) {
	want := []struct {
		event   api.ClusterEvent
		hasHint bool
	}{
		{event: api.ClusterEvent{Resource: api.QueueEvent, ActionType: fwk.Update}, hasHint: true},
		{event: api.ClusterEvent{Resource: api.PodGroupEvent, ActionType: fwk.Update | fwk.Delete}, hasHint: true},
		{event: api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add | fwk.UpdateNodeAllocatable}, hasHint: true},
		{event: api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete | fwk.UpdatePodScaleDown}, hasHint: true},
	}

	events, err := (&CapacityHintProvider{}).EventsToRegister(t.Context())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	if len(events) != len(want) {
		t.Fatalf("EventsToRegister() returned %d events, want %d", len(events), len(want))
	}
	for i := range want {
		if events[i].Event != want[i].event {
			t.Errorf("event[%d] = %#v, want %#v", i, events[i].Event, want[i].event)
		}
		if got := events[i].HintFn != nil; got != want[i].hasHint {
			t.Errorf("event[%d] has hint = %v, want %v", i, got, want[i].hasHint)
		}
	}
}
