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

package oversubscription

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
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
			Name: "test-node",
			Annotations: map[string]string{
				apis.OverSubscriptionTypesKey: "cpu",
			},
			Labels: map[string]string{
				apis.OverSubscriptionNodeLabelKey: "true",
			},
		},
		Status: v1.NodeStatus{Allocatable: map[v1.ResourceName]resource.Quantity{
			v1.ResourceCPU:    *resource.NewQuantity(10000, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(10000, resource.BinarySI),
		}},
	}, nil
}

func Test_reporter_RefreshCfg(t *testing.T) {
	tests := []struct {
		name              string
		getNodeFunc       utilnode.ActiveNode
		getPodsFunc       utilpod.ActivePods
		podProvider       *utiltesting.PodProvider
		policy            func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface
		cfg               *api.ColocationConfig
		enable            bool
		expectNode        func() *v1.Node
		expectTypes       sets.String
		expectEvictedPods []*v1.Pod
	}{
		{
			name: "enable overSubscription",
			getNodeFunc: func() (*v1.Node, error) {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Labels[apis.OverSubscriptionNodeLabelKey] = "false"
				return node, nil
			},
			podProvider: utiltesting.NewPodProvider(),
			policy: func(cfg *config.Configuration, activePods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(false),
					NodeOverSubscriptionEnable: utilpointer.Bool(true),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable:                utilpointer.Bool(true),
					OverSubscriptionTypes: utilpointer.String("cpu,memory"),
				}},
			enable:      true,
			expectTypes: sets.NewString("cpu", "memory"),
			expectNode: func() *v1.Node {
				node, err := makeNode()
				assert.NoError(t, err)
				return node
			},
			expectEvictedPods: nil,
		},
		{
			name: "enable overSubscription after disable config",
			getNodeFunc: func() (*v1.Node, error) {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Labels[apis.OverSubscriptionNodeLabelKey] = "false"
				return node, nil
			},
			podProvider: utiltesting.NewPodProvider(),
			policy: func(cfg *config.Configuration, activePods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(false),
					NodeOverSubscriptionEnable: utilpointer.Bool(false),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable:                utilpointer.Bool(true),
					OverSubscriptionTypes: utilpointer.String("cpu,memory"),
				}},
			expectTypes: sets.NewString("cpu", "memory"),
			expectNode: func() *v1.Node {
				node, err := makeNode()
				assert.NoError(t, err)
				return node
			},
			expectEvictedPods: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeNode, _ := tt.getNodeFunc()
			fakeClient := fakeclientset.NewSimpleClientset(fakeNode)
			cfg := &config.Configuration{GenericConfiguration: &config.VolcanoAgentConfiguration{
				KubeClient:   fakeClient,
				KubeNodeName: "test-node",
				NodeHasSynced: func() bool {
					return false
				},
				SupportedFeatures: []string{string(features.OverSubscriptionFeature)},
			}}
			r := &reporter{
				BaseHandle: &base.BaseHandle{
					Name:   string(features.OverSubscriptionFeature),
					Config: cfg,
				},
				Interface:   tt.policy(cfg, tt.podProvider.GetPodsFunc, tt.podProvider),
				enabled:     tt.enable,
				getNodeFunc: tt.getNodeFunc,
				getPodsFunc: tt.podProvider.GetPodsFunc,
			}
			assert.NoError(t, r.RefreshCfg(tt.cfg))
			node, err := fakeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get node, err: %v", err)
			}
			assert.Equal(t, tt.expectNode(), node)
			assert.Equal(t, tt.expectEvictedPods, tt.podProvider.GetEvictedPods())
		})
	}
}

func Test_reporter_Handle(t *testing.T) {
	tests := []struct {
		name         string
		policy       func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface
		getNodeFunc  utilnode.ActiveNode
		getPodsFunc  utilpod.ActivePods
		killPodFunc  utilpod.KillPod
		event        interface{}
		wantErr      assert.ErrorAssertionFunc
		expectedNode func() *v1.Node
	}{
		{
			name: "not over subscription node, skip patch",
			policy: func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			getNodeFunc: func() (*v1.Node, error) {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Labels[apis.OverSubscriptionNodeLabelKey] = "false"
				return node, nil
			},
			event: framework.NodeResourceEvent{
				MillCPU:     1000,
				MemoryBytes: 2000,
			},
			wantErr: assert.NoError,
			expectedNode: func() *v1.Node {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Labels[apis.OverSubscriptionNodeLabelKey] = "false"
				return node
			},
		},
		{
			name: "patch over subscription node to node status",
			policy: func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			getNodeFunc: makeNode,
			event: framework.NodeResourceEvent{
				MillCPU:     1000,
				MemoryBytes: 2000,
			},
			wantErr: assert.NoError,
			expectedNode: func() *v1.Node {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Status.Capacity = map[v1.ResourceName]resource.Quantity{}
				node.Status.Capacity[apis.ExtendResourceCPU] = *resource.NewQuantity(1000, resource.DecimalSI)
				node.Status.Capacity[apis.ExtendResourceMemory] = *resource.NewQuantity(2000, resource.BinarySI)
				node.Status.Allocatable[apis.ExtendResourceCPU] = *resource.NewQuantity(1000, resource.DecimalSI)
				node.Status.Allocatable[apis.ExtendResourceMemory] = *resource.NewQuantity(2000, resource.BinarySI)
				return node
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			makeNode, _ := tt.getNodeFunc()
			fakeClient := fakeclientset.NewSimpleClientset(makeNode)
			cfg := &config.Configuration{GenericConfiguration: &config.VolcanoAgentConfiguration{
				KubeClient:   fakeClient,
				KubeNodeName: "test-node",
				NodeHasSynced: func() bool {
					return false
				},
			}}
			r := &reporter{
				BaseHandle: &base.BaseHandle{
					Config: cfg},
				Interface:   tt.policy(cfg, tt.getPodsFunc, nil),
				getNodeFunc: tt.getNodeFunc,
				getPodsFunc: tt.getPodsFunc,
				killPodFunc: tt.killPodFunc,
			}
			tt.wantErr(t, r.Handle(tt.event), fmt.Sprintf("Handle(%v)", tt.event))
			node, err := fakeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get node, err: %v", err)
			}
			assert.Equal(t, tt.expectedNode(), node)
		})
	}
}
