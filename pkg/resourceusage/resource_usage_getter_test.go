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

package resourceusage

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	fakecollector "volcano.sh/volcano/pkg/metriccollect/testing"
)

func Test_getter_UsagesByValue(t *testing.T) {
	cfg := &config.Configuration{GenericConfiguration: &config.VolcanoAgentConfiguration{IncludeSystemUsage: false}}
	collector, err := metriccollect.NewMetricCollectorManager(cfg, &cgroup.CgroupManagerImpl{})
	assert.NoError(t, err)
	tests := []struct {
		name                  string
		collector             *metriccollect.MetricCollectorManager
		includeGuaranteedPods bool
		want                  Resource
	}{
		{
			name:                  "include guaranteed pods",
			includeGuaranteedPods: true,
			collector:             collector,
			want: map[v1.ResourceName]int64{
				v1.ResourceCPU:    int64(1000),
				v1.ResourceMemory: int64(2000),
			},
		},
		{
			name:                  "not include guaranteed pods",
			includeGuaranteedPods: false,
			collector:             collector,
			want: map[v1.ResourceName]int64{
				v1.ResourceCPU:    int64(1000),
				v1.ResourceMemory: int64(2000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &getter{
				collectorName: fakecollector.CollectorName,
				collector:     tt.collector,
			}
			if got := g.UsagesByValue(tt.includeGuaranteedPods); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UsagesByValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getter_UsagesByPercentage(t *testing.T) {
	cfg := &config.Configuration{GenericConfiguration: &config.VolcanoAgentConfiguration{IncludeSystemUsage: false}}
	collector, err := metriccollect.NewMetricCollectorManager(cfg, &cgroup.CgroupManagerImpl{})
	assert.NoError(t, err)
	tests := []struct {
		name      string
		node      *v1.Node
		collector *metriccollect.MetricCollectorManager
		want      Resource
	}{
		{
			name:      "get usage percent correctly",
			collector: collector,
			node:      makeNode(),
			want: map[v1.ResourceName]int64{
				v1.ResourceCPU:    20,
				v1.ResourceMemory: 40,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &getter{
				collectorName: fakecollector.CollectorName,
				collector:     tt.collector,
			}
			assert.Equalf(t, tt.want, g.UsagesByPercentage(tt.node), "UsagesByPercentage(%v)", tt.node)
		})
	}
}

func makeNode() *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: v1.NodeStatus{Allocatable: v1.ResourceList{
			v1.ResourceCPU:    *resource.NewMilliQuantity(5000, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(5000, resource.DecimalSI),
		}},
	}
}
