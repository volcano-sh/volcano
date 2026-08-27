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

package unschedulable

import (
	"context"
	"errors"
	"reflect"
	"testing"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type registryHintProvider struct {
	events []EventWithHint
	err    error
}

func (p registryHintProvider) EventsToRegister(context.Context) ([]EventWithHint, error) {
	return p.events, p.err
}

func TestHintRegistryRegister(t *testing.T) {
	nodeEvent := EventWithHint{
		Event: fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add},
	}
	podEvent := EventWithHint{
		Event: fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete},
	}
	indexedNodeEvent := nodeEvent
	indexedNodeEvent.JobKeysFn = func(*api.JobInfo, Rejection) ([]HintKey, error) {
		return []HintKey{"node"}, nil
	}
	indexedNodeEvent.EventKeysFn = func(any, any) ([]HintKey, error) {
		return []HintKey{"node"}, nil
	}
	unpairedNodeEvent := indexedNodeEvent
	unpairedNodeEvent.EventKeysFn = nil
	labeledNodeEvent := nodeEvent
	labeledNodeEvent.Event.CustomLabel = "same-semantic-event"
	tests := []struct {
		name      string
		providers []registryHintProvider
		want      []EventWithHint
	}{
		{
			name:      "registers plugin events",
			providers: []registryHintProvider{{events: []EventWithHint{nodeEvent}}},
			want:      []EventWithHint{nodeEvent},
		},
		{
			name: "replaces previous plugin events",
			providers: []registryHintProvider{
				{events: []EventWithHint{nodeEvent}},
				{events: []EventWithHint{podEvent}},
			},
			want: []EventWithHint{podEvent},
		},
		{
			name: "failed replacement clears previous events",
			providers: []registryHintProvider{
				{events: []EventWithHint{nodeEvent}},
				{err: errors.New("registration failed")},
			},
		},
		{
			name: "duplicate plugin event clears registration",
			providers: []registryHintProvider{{events: []EventWithHint{
				nodeEvent,
				nodeEvent,
			}}},
		},
		{
			name: "custom label does not create a distinct plugin event",
			providers: []registryHintProvider{{events: []EventWithHint{
				nodeEvent,
				labeledNodeEvent,
			}}},
		},
		{
			name: "changing index mode clears registration",
			providers: []registryHintProvider{
				{events: []EventWithHint{nodeEvent}},
				{events: []EventWithHint{indexedNodeEvent}},
			},
		},
		{
			name:      "unpaired HintKey functions clear registration",
			providers: []registryHintProvider{{events: []EventWithHint{unpairedNodeEvent}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := newHintRegistry()
			for _, provider := range test.providers {
				registry.register("plugin", provider)
			}

			if got := registry.eventsForPlugin("plugin"); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("eventsForPlugin() = %#v, want %#v", got, test.want)
			}
		})
	}
}
