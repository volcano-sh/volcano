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
	"context"
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type fakeEnqueueExtensions struct {
	events []fwk.ClusterEventWithHint
	err    error
}

func (f fakeEnqueueExtensions) Name() string {
	return "fake"
}

func (f fakeEnqueueExtensions) EventsToRegister(context.Context) ([]fwk.ClusterEventWithHint, error) {
	return f.events, f.err
}

func TestKubeHintProviderEventsToRegister(t *testing.T) {
	providerErr := errors.New("registration failed")
	tests := []struct {
		name    string
		ext     fakeEnqueueExtensions
		want    []api.ClusterEvent
		wantErr error
	}{
		{
			name: "adapts kube events",
			ext: fakeEnqueueExtensions{events: []fwk.ClusterEventWithHint{
				{Event: fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}},
				{Event: fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}},
			}},
			want: []api.ClusterEvent{
				{Resource: fwk.Node, ActionType: fwk.Add},
				{Resource: fwk.Pod, ActionType: fwk.Delete},
			},
		},
		{
			name:    "returns kube registration error",
			ext:     fakeEnqueueExtensions{err: providerErr},
			wantErr: providerErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := (&KubeHintProvider{Ext: test.ext}).EventsToRegister(t.Context())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("EventsToRegister() error = %v, want %v", err, test.wantErr)
			}
			if len(events) != len(test.want) {
				t.Fatalf("EventsToRegister() returned %d events, want %d", len(events), len(test.want))
			}
			for i := range test.want {
				if events[i].Event != test.want[i] {
					t.Errorf("event[%d] = %#v, want %#v", i, events[i].Event, test.want[i])
				}
			}
		})
	}
}

func TestWrapPodHint(t *testing.T) {
	hintErr := errors.New("hint failed")
	tests := []struct {
		name    string
		kubeFn  fwk.QueueingHintFn
		want    api.HintResult
		wantErr error
	}{
		{
			name: "skips when every rejected task is skipped",
			kubeFn: func(klog.Logger, *v1.Pod, any, any) (fwk.QueueingHint, error) {
				return fwk.QueueSkip, nil
			},
			want: api.HintSkip,
		},
		{
			name: "wakes when any rejected task queues",
			kubeFn: func(_ klog.Logger, pod *v1.Pod, _, _ any) (fwk.QueueingHint, error) {
				if pod.Name == "task-2" {
					return fwk.Queue, nil
				}
				return fwk.QueueSkip, nil
			},
			want: api.HintWakeup,
		},
		{
			name: "wakes and returns hint error",
			kubeFn: func(klog.Logger, *v1.Pod, any, any) (fwk.QueueingHint, error) {
				return fwk.QueueSkip, hintErr
			},
			want:    api.HintWakeup,
			wantErr: hintErr,
		},
	}

	pod1 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "task-1", UID: "task-1"}}
	pod2 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "task-2", UID: "task-2"}}
	task1 := api.NewTaskInfo(pod1)
	task2 := api.NewTaskInfo(pod2)
	job := api.NewJobInfo("job", task1, task2)
	rejection := api.Rejection{Tasks: []api.TaskID{task1.UID, task2.UID}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := wrapPodHint(test.kubeFn)(job, rejection, nil, nil)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("wrapped hint error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("wrapped hint result = %v, want %v", got, test.want)
			}
		})
	}
}
