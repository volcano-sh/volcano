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

package noderesources

import (
	"fmt"
	"os"
	"path"
	"testing"

	. "github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/oversubscription/policy/extend"
	"volcano.sh/volcano/pkg/agent/oversubscription/queue"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	utiltesting "volcano.sh/volcano/pkg/agent/utils/testing"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	fakecollector "volcano.sh/volcano/pkg/metriccollect/testing"
)

func makeNode() (*v1.Node, error) {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				apis.OverSubscriptionTypesKey: "cpu",
			},
			Labels: map[string]string{
				apis.OverSubscriptionNodeLabelKey: "true",
			},
		},
		Status: v1.NodeStatus{Allocatable: map[v1.ResourceName]resource.Quantity{
			v1.ResourceCPU:    *resource.NewMilliQuantity(3000, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(10000, resource.BinarySI),
		}},
	}, nil
}

func Test_historicalUsageCalculator_utilizationMonitoring(t *testing.T) {
	dir, err := os.MkdirTemp("", "cpuManagerPolicy")
	assert.NoError(t, err)
	defer func() {
		err = os.RemoveAll(dir)
		assert.NoError(t, err)
	}()

	cfg := &config.Configuration{
		GenericConfiguration: &config.VolcanoAgentConfiguration{
			OverSubscriptionRatio: 60,
		},
	}

	mgr, err := metriccollect.NewMetricCollectorManager(cfg, nil)
	assert.NoError(t, err)

	tests := []struct {
		name        string
		usages      workqueue.RateLimitingInterface
		getPodsFunc utilpod.ActivePods
		getNodeFunc utilnode.ActiveNode
		prepare     func()
		expectRes   []apis.Resource
	}{
		{
			name:        "calculate using extend cpu&memory && cpu manager policy none",
			usages:      workqueue.NewNamedRateLimitingQueue(nil, "calculator"),
			getNodeFunc: makeNode,
			prepare: func() {
				err = os.Setenv("KUBELET_ROOT_DIR", dir)
				assert.NoError(t, err)
				b := []byte(`{"policyName":"none","defaultCpuSet":"0-1","checksum":1636926438}`)
				err = os.WriteFile(path.Join(dir, "cpu_manager_state"), b, 0600)
				assert.NoError(t, err)
			},
			getPodsFunc: func() ([]*v1.Pod, error) {
				return []*v1.Pod{
					utiltesting.MakePod("online-1", 3, 2000, ""),
					utiltesting.MakePod("online-2", 2, 2000, ""),
					utiltesting.MakePodWithExtendResources("offline-1", 1000, 1000, "BE"),
				}, nil
			},
			expectRes: []apis.Resource{{v1.ResourceCPU: 1200, v1.ResourceMemory: 4800}},
		},
		{
			name:        "calculate using extend cpu&memory && cpu manager policy static",
			usages:      workqueue.NewNamedRateLimitingQueue(nil, "calculator"),
			getNodeFunc: makeNode,
			prepare: func() {
				err = os.Setenv("KUBELET_ROOT_DIR", dir)
				assert.NoError(t, err)
				b := []byte(`{"policyName":"static","defaultCpuSet":"0-1","checksum":1636926438}`)
				err = os.WriteFile(path.Join(dir, "cpu_manager_state"), b, 0600)
				assert.NoError(t, err)
			},
			getPodsFunc: func() ([]*v1.Pod, error) {
				return []*v1.Pod{
					utiltesting.MakePod("online-1", 1, 2000, ""),
					utiltesting.MakePod("online-2", 1, 2000, ""),
					utiltesting.MakePodWithExtendResources("offline-1", 1000, 1000, "BE"),
				}, nil
			},
			expectRes: []apis.Resource{{v1.ResourceCPU: 0, v1.ResourceMemory: 4800}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqQueue := queue.NewSqQueue()

			ApplyFunc(cfg.GetActivePods, func() ([]*v1.Pod, error) {
				return tt.getPodsFunc()
			})
			ApplyFunc(cfg.GetNode, func() (*v1.Node, error) {
				return tt.getNodeFunc()
			})
			r := &historicalUsageCalculator{
				// fake collector: cpu:1000m, memory:2000byte
				Interface:   extend.NewExtendResource(cfg, mgr, nil, sqQueue, fakecollector.CollectorName),
				usages:      tt.usages,
				queue:       sqQueue,
				getNodeFunc: tt.getNodeFunc,
			}
			if tt.prepare != nil {
				tt.prepare()
			}
			r.CalOverSubscriptionResources()
			res := r.queue.GetAll()
			assert.Equalf(t, tt.expectRes, res, "utilizationMonitoring")
		})
	}
}

func Test_historicalUsageCalculator_preProcess(t *testing.T) {
	queue := queue.NewSqQueue()
	queue.Enqueue(apis.Resource{
		v1.ResourceCPU:    1000,
		v1.ResourceMemory: 1500,
	})

	queue.Enqueue(apis.Resource{
		v1.ResourceCPU:    1000,
		v1.ResourceMemory: 1500,
	})

	tests := []struct {
		name             string
		getNodeFunc      utilnode.ActiveNode
		getPodsFunc      utilpod.ActivePods
		resourceTypes    sets.String
		expectedResource framework.NodeResourceEvent
	}{
		{
			name: "default report cpu and memory",
			getNodeFunc: func() (*v1.Node, error) {
				node, err := makeNode()
				assert.NoError(t, err)
				node.Annotations = nil
				return node, nil
			},
			getPodsFunc: func() ([]*v1.Pod, error) {
				return []*v1.Pod{
					utiltesting.MakePod("online-1", 3, 2000, ""),
					utiltesting.MakePod("online-2", 2, 2000, ""),
					utiltesting.MakePod("offline-1", 2, 2000, "BE"),
				}, nil
			},
			expectedResource: framework.NodeResourceEvent{
				MillCPU:     1000,
				MemoryBytes: 1500,
			},
		},
		{
			name:        "only report cpu with node annotation specified",
			getNodeFunc: makeNode,
			getPodsFunc: func() ([]*v1.Pod, error) {
				return []*v1.Pod{
					utiltesting.MakePod("online-1", 3, 2000, ""),
					utiltesting.MakePod("online-2", 2, 2000, ""),
					utiltesting.MakePod("online-3", 2, 2000, ""),
				}, nil
			},
			expectedResource: framework.NodeResourceEvent{
				MillCPU:     1000,
				MemoryBytes: 0,
			},
		},
	}

	cfg := &config.Configuration{
		GenericConfiguration: &config.VolcanoAgentConfiguration{
			OverSubscriptionRatio: 60,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &historicalUsageCalculator{
				Interface:     extend.NewExtendResource(cfg, nil, nil, queue, ""),
				queue:         queue,
				usages:        workqueue.NewNamedRateLimitingQueue(nil, ""),
				getNodeFunc:   tt.getNodeFunc,
				resourceTypes: sets.NewString("cpu", "memory"),
			}
			r.preProcess()
			usages, shutdown := r.usages.Get()
			if shutdown {
				t.Errorf("queue shutdown")
			}
			event, ok := usages.(framework.NodeResourceEvent)
			if !ok {
				t.Errorf("illegal node resource event")
			}
			assert.Equal(t, tt.expectedResource, event)
		})
	}
}

func Test_historicalUsageCalculator_RefreshCfg(t *testing.T) {
	tests := []struct {
		name                 string
		resourceTypes        sets.String
		cfg                  *api.ColocationConfig
		expectedResourceType sets.String
		wantErr              assert.ErrorAssertionFunc
	}{
		{
			name:                 "return err",
			cfg:                  nil,
			resourceTypes:        sets.NewString(),
			expectedResourceType: sets.NewString(),
			wantErr:              assert.Error,
		},
		{
			name:                 "empty config",
			cfg:                  &api.ColocationConfig{OverSubscriptionConfig: &api.OverSubscription{OverSubscriptionTypes: utilpointer.String("")}},
			resourceTypes:        sets.NewString(),
			expectedResourceType: sets.NewString(),
			wantErr:              assert.NoError,
		},
		{
			name:                 "cpu",
			cfg:                  &api.ColocationConfig{OverSubscriptionConfig: &api.OverSubscription{OverSubscriptionTypes: utilpointer.String("cpu")}},
			resourceTypes:        sets.NewString(),
			expectedResourceType: sets.NewString("cpu"),
			wantErr:              assert.NoError,
		},
		{
			name:                 "cpu and memory",
			cfg:                  &api.ColocationConfig{OverSubscriptionConfig: &api.OverSubscription{OverSubscriptionTypes: utilpointer.String("cpu,memory")}},
			resourceTypes:        sets.NewString(),
			expectedResourceType: sets.NewString("cpu", "memory"),
			wantErr:              assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &historicalUsageCalculator{
				resourceTypes: tt.resourceTypes,
			}
			tt.wantErr(t, r.RefreshCfg(tt.cfg), fmt.Sprintf("RefreshCfg(%v)", tt.cfg))
			assert.Equal(t, tt.expectedResourceType, r.resourceTypes)
		})
	}
}
