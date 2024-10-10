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

package eviction

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy/extend"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	utiltesting "volcano.sh/volcano/pkg/agent/utils/testing"
	"volcano.sh/volcano/pkg/config"
)

func makeNode() (*v1.Node, error) {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node",
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
	}, nil
}

func Test_manager_Handle(t *testing.T) {
	fakeTime, err := time.Parse("2006-01-02", "2023-04-01")
	assert.NoError(t, err)
	patch := gomonkey.NewPatches()
	defer patch.Reset()
	patch.ApplyFunc(time.Now, func() time.Time {
		return fakeTime
	})

	pp := utiltesting.NewPodProvider(
		utiltesting.MakePod("offline-pod-1", 30, 30, "BE"),
		utiltesting.MakePod("offline-pod-2", 40, 30, "BE"),
		utiltesting.MakePod("online-pod", 10, 10, ""),
	)

	tests := []struct {
		name         string
		event        interface{}
		Eviction     eviction.Eviction
		policy       func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface
		getNodeFunc  utilnode.ActiveNode
		getPodsFunc  utilpod.ActivePods
		wantErr      assert.ErrorAssertionFunc
		expectedNode func() *v1.Node
	}{
		{
			name: "evict high request pod with extend resource",
			event: framework.NodeMonitorEvent{
				TimeStamp: time.Now(),
				Resource:  v1.ResourceCPU,
			},
			Eviction: pp,
			policy: func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			getPodsFunc: pp.GetPodsFunc,
			getNodeFunc: makeNode,
			wantErr:     assert.NoError,
			expectedNode: func() *v1.Node {
				node := &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: v1.NodeSpec{Taints: []v1.Taint{{Key: apis.PodEvictingKey, Effect: v1.TaintEffectNoSchedule}}},
				}
				return node
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeNode, err := makeNode()
			assert.NoError(t, err)
			fakeClient := fakeclientset.NewSimpleClientset(fakeNode)
			cfg := &config.Configuration{GenericConfiguration: &config.VolcanoAgentConfiguration{
				KubeClient:   fakeClient,
				KubeNodeName: "test-node",
				NodeHasSynced: func() bool {
					return false
				},
			}}
			m := &manager{
				cfg:         cfg,
				Interface:   tt.policy(cfg, tt.getPodsFunc, nil),
				Eviction:    tt.Eviction,
				getNodeFunc: tt.getNodeFunc,
				getPodsFunc: tt.getPodsFunc,
			}
			tt.wantErr(t, m.Handle(tt.event), fmt.Sprintf("Handle(%v)", tt.event))
			evictedPods := pp.GetEvictedPods()
			// Verify that pod should be evicted.
			if len(evictedPods) == 0 || evictedPods[0].Name != "offline-pod-2" {
				t.Errorf("Manager should have evicted offline-pod-2 but not")
			}

			// Node should be schedule disabled.
			node, err := fakeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get node, err: %v", err)
			}
			assert.Equalf(t, tt.expectedNode(), node, "Node should have eviction annotation or taint")
		})
	}
}
