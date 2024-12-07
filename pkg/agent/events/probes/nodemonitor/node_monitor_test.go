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

package nodemonitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy/extend"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/resourceusage"
)

func makeNode() (*v1.Node, error) {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				apis.OverSubscriptionNodeLabelKey: "true",
			},
		},
		Spec: v1.NodeSpec{Taints: []v1.Taint{{
			Key:    apis.PodEvictingKey,
			Effect: v1.TaintEffectNoSchedule,
		}}},
	}, nil
}

func Test_monitor_detect(t *testing.T) {
	tests := []struct {
		name                    string
		Configuration           *config.Configuration
		policy                  func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface
		queue                   workqueue.RateLimitingInterface
		lowWatermark            apis.Watermark
		highWatermark           apis.Watermark
		highUsageCountByResName map[v1.ResourceName]int
		getNodeFunc             utilnode.ActiveNode
		getPodsFunc             utilpod.ActivePods
		usageGetter             resourceusage.Getter
		expectedNode            func() *v1.Node
		expectedRes             v1.ResourceName
		expectedLen             int
	}{
		{
			name:                    "cpu in high usage with extend resource",
			highUsageCountByResName: map[v1.ResourceName]int{v1.ResourceCPU: 6},
			getNodeFunc:             makeNode,
			getPodsFunc: func() ([]*v1.Pod, error) {
				return []*v1.Pod{}, nil
			},
			policy: func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			usageGetter: resourceusage.NewFakeResourceGetter(0, 0, 60, 60),
			expectedRes: v1.ResourceCPU,
			expectedLen: 1,
		},
		{
			name:                    "remove taint when use extend resource",
			highUsageCountByResName: map[v1.ResourceName]int{v1.ResourceCPU: 5},
			getNodeFunc:             makeNode,
			getPodsFunc: func() ([]*v1.Pod, error) {
				return []*v1.Pod{}, nil
			},
			policy: func(cfg *config.Configuration, pods utilpod.ActivePods, evictor eviction.Eviction) policy.Interface {
				return extend.NewExtendResource(cfg, nil, evictor, nil, "")
			},
			usageGetter: resourceusage.NewFakeResourceGetter(0, 0, 20, 20),
			expectedRes: v1.ResourceCPU,
			expectedNode: func() *v1.Node {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Spec.Taints = nil
				return node
			},
			expectedLen: 0,
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
			queue := workqueue.NewNamedRateLimitingQueue(nil, "test")
			m := &monitor{
				queue:                   queue,
				Configuration:           cfg,
				Interface:               tt.policy(cfg, nil, nil),
				highUsageCountByResName: tt.highUsageCountByResName,
				lowWatermark:            map[v1.ResourceName]int{v1.ResourceCPU: 30, v1.ResourceMemory: 30},
				getNodeFunc:             tt.getNodeFunc,
				getPodsFunc:             tt.getPodsFunc,
				usageGetter:             tt.usageGetter,
			}
			m.detect()
			assert.Equalf(t, tt.expectedLen, queue.Len(), "detect()")
			if queue.Len() != 0 {
				key, shutdown := queue.Get()
				if shutdown {
					t.Errorf("Unexpected: queue is shutdown")
				}
				event, ok := key.(framework.NodeMonitorEvent)
				if !ok {
					t.Errorf("Invalid event: %v", key)
				}
				assert.Equalf(t, tt.expectedRes, event.Resource, "detect()")
			}
			if tt.expectedNode != nil {
				node, err := fakeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get node, err: %v", err)
				}
				assert.Equalf(t, tt.expectedNode(), node, "detect()")
			}
		})
	}
}
