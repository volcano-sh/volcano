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

package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"volcano.sh/volcano/pkg/agent/apis"
)

func TestNodeUpdate(t *testing.T) {
	const nodeName = "test-node"
	newNode := func(labels map[string]string, annotations map[string]string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Labels:      labels,
			Annotations: annotations,
		}}
	}

	tests := []struct {
		name       string
		oldObj     interface{}
		newObj     interface{}
		wantQueued bool
	}{
		{
			name:   "labels unchanged",
			oldObj: newNode(map[string]string{"pool": "cpu"}, nil),
			newObj: newNode(map[string]string{"pool": "cpu"}, map[string]string{"updated": "true"}),
		},
		{
			name:   "no labels on either node",
			oldObj: newNode(nil, nil),
			newObj: newNode(map[string]string{}, nil),
		},
		{
			name:       "colocation label changed",
			oldObj:     newNode(map[string]string{apis.ColocationEnableNodeLabelKey: "false"}, nil),
			newObj:     newNode(map[string]string{apis.ColocationEnableNodeLabelKey: "true"}, nil),
			wantQueued: true,
		},
		{
			name:       "selector label value changed",
			oldObj:     newNode(map[string]string{"pool": "cpu"}, nil),
			newObj:     newNode(map[string]string{"pool": "gpu"}, nil),
			wantQueued: true,
		},
		{
			name:       "selector label added",
			oldObj:     newNode(nil, nil),
			newObj:     newNode(map[string]string{"pool": "gpu"}, nil),
			wantQueued: true,
		},
		{
			name:       "selector label removed",
			oldObj:     newNode(map[string]string{"pool": "gpu"}, nil),
			newObj:     newNode(nil, nil),
			wantQueued: true,
		},
		{
			name:       "old object is not a node",
			oldObj:     "not-a-node",
			newObj:     newNode(nil, nil),
			wantQueued: true,
		},
		{
			name:       "new object is not a node",
			oldObj:     newNode(nil, nil),
			newObj:     "not-a-node",
			wantQueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, ok := NewConfigMapSource(fake.NewSimpleClientset(), nodeName, "test-ns").(*configMapSource)
			require.True(t, ok)
			defer cs.queue.ShutDown()

			cs.nodeUpdate(tt.oldObj, tt.newObj)

			if !tt.wantQueued {
				assert.Equal(t, 0, cs.queue.Len())
				return
			}
			require.Equal(t, 1, cs.queue.Len())
			item, _ := cs.queue.Get()
			assert.Equal(t, "node", item)
		})
	}
}
