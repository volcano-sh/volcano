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

package cpumonitor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/config"
)

func Test_monitor_detectCPUThrottling(t *testing.T) {
	tests := []struct {
		name                   string
		cpuThrottlingThreshold int
		cpuJitterLimitPercent  int
		pods                   []*v1.Pod
		expectedEventCount     int
		expectedQuotaMilli     int64
	}{
		{
			name:                   "emit quota with no online pods",
			cpuThrottlingThreshold: 80,
			cpuJitterLimitPercent:  1,
			pods:                   []*v1.Pod{},
			expectedEventCount:     1,
			expectedQuotaMilli:     -1,
		},
		{
			name:                   "subtract online pod requests from quota",
			cpuThrottlingThreshold: 80,
			cpuJitterLimitPercent:  1,
			pods: []*v1.Pod{
				buildPod("online-1", "100m", "LS"),
				buildPod("online-2", "200m", "LS"),
			},
			expectedEventCount: 1,
			expectedQuotaMilli: 500,
		},
		{
			name:                   "quota floored at zero when online requests exceed allowance",
			cpuThrottlingThreshold: 50,
			pods: []*v1.Pod{
				buildPod("online-1", "750m", "LS"),
				buildPod("be-1", "100m", "BE"),
			},
			expectedEventCount: 1,
			expectedQuotaMilli: 50,
		},
		{
			name:                   "skip when throttling disabled",
			cpuThrottlingThreshold: 0,
			pods:                   []*v1.Pod{},
			expectedEventCount:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeNode, err := makeNodeWithAllocatable("1000m")
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

			m := &cpuQuotaMonitor{
				queue:                  queue,
				Configuration:          cfg,
				cpuThrottlingThreshold: tt.cpuThrottlingThreshold,
				getNodeFunc: func() (*v1.Node, error) {
					return fakeNode, nil
				},
				getPodsFunc: func() ([]*v1.Pod, error) {
					return tt.pods, nil
				},
				cpuJitterLimitPercent: tt.cpuJitterLimitPercent,
			}

			m.detectCPUQuota()

			assert.Equal(t, tt.expectedEventCount, queue.Len(), "unexpected event count")

			if tt.expectedEventCount > 0 {
				key, shutdown := queue.Get()
				assert.False(t, shutdown, "queue should not be shutdown")

				event, ok := key.(framework.NodeCPUThrottleEvent)
				assert.True(t, ok, "event should be NodeCPUThrottleEvent")
				assert.Equal(t, v1.ResourceCPU, event.Resource, "unexpected event resource")
				assert.Equal(t, tt.expectedQuotaMilli, event.CPUQuotaMilli, "unexpected event quota")
				assert.True(t, time.Since(event.TimeStamp) < time.Second, "event timestamp should be recent")
			}
		})
	}
}

func makeNodeWithAllocatable(cpu string) (*v1.Node, error) {
	node, err := makeNode()
	if err != nil {
		return nil, err
	}
	node.Status.Allocatable = v1.ResourceList{
		v1.ResourceCPU: resource.MustParse(cpu),
	}
	return node, nil
}

func buildPod(name, cpuRequest, qosLevel string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				apis.PodQosLevelKey: qosLevel,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "contrainer-1",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{},
					},
				},
			},
		},
	}

	if cpuRequest != "" {
		pod.Spec.Containers[0].Resources.Requests[v1.ResourceCPU] = resource.MustParse(cpuRequest)
	}
	return pod
}

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
