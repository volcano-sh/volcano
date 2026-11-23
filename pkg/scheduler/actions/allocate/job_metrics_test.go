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

package allocate

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const e2eJobSchedulingLatencyMetric = "volcano_e2e_job_scheduling_latency_milliseconds"

// e2eJobSchedulingLatencyCount returns the number of observations recorded in
// the per-job e2e scheduling latency histogram.
func e2eJobSchedulingLatencyCount(t *testing.T) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == e2eJobSchedulingLatencyMetric {
			require.Len(t, family.GetMetric(), 1)
			return family.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func TestAllocateRecordsJobSchedulingLatencyOnCommit(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName:       gang.New,
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
	}
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledAllocatable: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	tests := []struct {
		name             string
		minMember        int32
		nodeCPU          string
		expectBindMap    map[string]string
		wantObservations uint64
	}{
		{
			name:             "gang job with three tasks is observed once when committed",
			minMember:        3,
			nodeCPU:          "3",
			expectBindMap:    map[string]string{"c1/p1": "n1", "c1/p2": "n1", "c1/p3": "n1"},
			wantObservations: 1,
		},
		{
			name:             "gang job whose allocation is discarded is not observed",
			minMember:        3,
			nodeCPU:          "2",
			expectBindMap:    map[string]string{},
			wantObservations: 0,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := uthelper.TestCommonStruct{
				Name:    tt.name,
				Plugins: plugins,
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", tt.minMember, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", nil, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", nil, nil),
					util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", nil, nil),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList(tt.nodeCPU, "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindMap:  tt.expectBindMap,
				ExpectBindsNum: len(tt.expectBindMap),
			}
			test.RegisterSession(tiers, nil)
			defer test.Close()

			before := e2eJobSchedulingLatencyCount(t)
			test.Run([]framework.Action{New()})

			require.NoError(t, test.CheckBind(i))
			assert.Equal(t, tt.wantObservations, e2eJobSchedulingLatencyCount(t)-before)
		})
	}
}
