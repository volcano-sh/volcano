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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/schedulinggates"
)

func TestNewDefaultQueuePlugins(t *testing.T) {
	ctx := context.Background()
	plugins, err := newDefaultQueuePlugins(ctx)
	require.NoError(t, err)
	require.Len(t, plugins, 1)
	plugin := plugins[0]
	assert.Equal(t, schedulinggates.Name, plugin.Name())

	tests := []struct {
		name  string
		gates []v1.PodSchedulingGate
		code  fwk.Code
	}{
		{
			name: "pod without scheduling gates",
			code: fwk.Success,
		},
		{
			name:  "pod with scheduling gates",
			gates: []v1.PodSchedulingGate{{Name: "example.com/ready"}},
			code:  fwk.UnschedulableAndUnresolvable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{Spec: v1.PodSpec{SchedulingGates: tt.gates}}
			assert.Equal(t, tt.code, plugin.PreEnqueue(ctx, pod).Code())
		})
	}

	events, err := plugin.EventsToRegister(ctx)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, fwk.ClusterEvent{
		Resource:   fwk.TargetPod,
		ActionType: fwk.UpdatePodSchedulingGatesEliminated,
	}, events[0].Event)
	require.NotNil(t, events[0].QueueingHintFn)
	oldPod := &v1.Pod{Spec: v1.PodSpec{
		SchedulingGates: []v1.PodSchedulingGate{{Name: "example.com/ready"}},
	}}
	newPod := &v1.Pod{}
	hint, err := events[0].QueueingHintFn(klog.Background(), newPod, oldPod, newPod)
	require.NoError(t, err)
	assert.Equal(t, fwk.Queue, hint)
}
