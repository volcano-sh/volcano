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

package cache

import (
	"context"
	"errors"
	"reflect"
	"testing"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type registryHintProvider struct {
	events []api.ClusterEventWithHint
	err    error
}

func (p registryHintProvider) EventsToRegister(context.Context) ([]api.ClusterEventWithHint, error) {
	return p.events, p.err
}

func TestHintRegistryRegister(t *testing.T) {
	nodeEvent := api.ClusterEventWithHint{
		Event: api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add},
	}
	podEvent := api.ClusterEventWithHint{
		Event: api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete},
	}
	tests := []struct {
		name      string
		providers []registryHintProvider
		want      []api.ClusterEventWithHint
	}{
		{
			name:      "registers plugin events",
			providers: []registryHintProvider{{events: []api.ClusterEventWithHint{nodeEvent}}},
			want:      []api.ClusterEventWithHint{nodeEvent},
		},
		{
			name: "replaces previous plugin events",
			providers: []registryHintProvider{
				{events: []api.ClusterEventWithHint{nodeEvent}},
				{events: []api.ClusterEventWithHint{podEvent}},
			},
			want: []api.ClusterEventWithHint{podEvent},
		},
		{
			name: "failed replacement clears previous events",
			providers: []registryHintProvider{
				{events: []api.ClusterEventWithHint{nodeEvent}},
				{err: errors.New("registration failed")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewHintRegistry()
			for _, provider := range test.providers {
				registry.Register("plugin", provider)
			}

			if got := registry.eventsForPlugin("plugin"); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("eventsForPlugin() = %#v, want %#v", got, test.want)
			}
		})
	}
}
