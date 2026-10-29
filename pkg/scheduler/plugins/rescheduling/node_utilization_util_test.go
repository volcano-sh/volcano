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

package rescheduling

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestEvictPodsFromSourceNodes(t *testing.T) {
	makePod := func(namespace, name, uid string, priority int32) *v1.Pod {
		pod := util.BuildPodWithPriority(namespace, name, "source", v1.PodRunning,
			api.BuildResourceList("1", "1Gi"), "job", nil, nil, &priority)
		pod.UID = types.UID(uid)
		return pod
	}
	workerA := makePod("team-a", "worker-0", "worker-a", 0)
	workerB := makePod("team-b", "worker-0", "worker-b", 0)
	oldWorker := makePod("team-a", "worker-0", "old-worker", 0)
	highPriority := makePod("team-a", "worker-1", "high-priority", 100)

	tests := []struct {
		name            string
		pods            []*v1.Pod
		candidates      []*v1.Pod
		wantUIDs        []types.UID
		wantUtilization float64
	}{
		{
			name:            "same name in another namespace",
			pods:            []*v1.Pod{workerA},
			candidates:      []*v1.Pod{workerB, workerA},
			wantUIDs:        []types.UID{workerA.UID},
			wantUtilization: 80,
		},
		{
			name:            "same name does not make a pod eligible",
			pods:            []*v1.Pod{workerA},
			candidates:      []*v1.Pod{workerB},
			wantUtilization: 90,
		},
		{
			name:            "recreated pod does not match the old UID",
			pods:            []*v1.Pod{workerA},
			candidates:      []*v1.Pod{oldWorker},
			wantUtilization: 90,
		},
		{
			name:            "skip ineligible pods and stop after relieving the source",
			pods:            []*v1.Pod{workerB, highPriority, workerA},
			candidates:      []*v1.Pod{highPriority, workerA},
			wantUIDs:        []types.UID{workerA.UID},
			wantUtilization: 80,
		},
		{
			name:            "no candidates",
			pods:            []*v1.Pod{workerA},
			wantUtilization: 90,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks := make([]*api.TaskInfo, 0, len(tt.candidates))
			for _, pod := range tt.candidates {
				tasks = append(tasks, api.NewTaskInfo(pod.DeepCopy()))
			}
			source := &NodeUtilization{
				nodeInfo:    util.BuildNode("source", api.BuildResourceList("10", "10Gi"), nil),
				utilization: map[v1.ResourceName]float64{v1.ResourceCPU: 90, v1.ResourceMemory: 90},
				pods:        append([]*v1.Pod(nil), tt.pods...),
			}
			target := &NodeUtilization{
				nodeInfo:    util.BuildNode("target", api.BuildResourceList("10", "10Gi"), nil),
				utilization: map[v1.ResourceName]float64{v1.ResourceCPU: 10, v1.ResourceMemory: 10},
			}
			config := LowNodeUtilizationConf{TargetThresholds: map[string]float64{"cpu": 85, "memory": 85}}
			victims := evictPodsFromSourceNodes([]*NodeUtilization{source}, []*NodeUtilization{target}, tasks, isContinueEvictPods, config)
			require.Len(t, victims, len(tt.wantUIDs))
			for i, victim := range victims {
				assert.Equal(t, tt.wantUIDs[i], victim.Pod.UID)
			}
			assert.InDelta(t, tt.wantUtilization, source.utilization[v1.ResourceCPU], 0.001)
			assert.InDelta(t, tt.wantUtilization, source.utilization[v1.ResourceMemory], 0.001)
			assert.Equal(t, float64(10), target.utilization[v1.ResourceCPU])
			assert.Equal(t, float64(10), target.utilization[v1.ResourceMemory])
		})
	}
}

func BenchmarkEvictPodsFromSourceNodes(b *testing.B) {
	for _, taskCount := range []int{1000, 5000, 10000} {
		b.Run(fmt.Sprintf("tasks=%d", taskCount), func(b *testing.B) {
			tasks := make([]*api.TaskInfo, taskCount)
			for i := range tasks {
				pod := util.BuildPod("default", fmt.Sprintf("candidate-%d", i), "other", v1.PodRunning,
					api.BuildResourceList("1", "1Gi"), "job", nil, nil)
				pod.UID = types.UID(pod.Name)
				tasks[i] = api.NewTaskInfo(pod)
			}
			// Pods outside the candidate set must be skipped without changing utilization.
			pods := make([]*v1.Pod, 1000)
			for i := range pods {
				priority := int32(i)
				pods[i] = util.BuildPodWithPriority("default", fmt.Sprintf("ineligible-%d", i), "source", v1.PodRunning,
					api.BuildResourceList("1", "1Gi"), "job", nil, nil, &priority)
				pods[i].UID = types.UID(pods[i].Name)
			}
			source := &NodeUtilization{
				nodeInfo:    util.BuildNode("source", api.BuildResourceList("10000", "10000Gi"), nil),
				utilization: map[v1.ResourceName]float64{v1.ResourceCPU: 90, v1.ResourceMemory: 90},
				pods:        pods,
			}
			target := &NodeUtilization{
				nodeInfo:    util.BuildNode("target", api.BuildResourceList("10000", "10000Gi"), nil),
				utilization: map[v1.ResourceName]float64{v1.ResourceCPU: 10, v1.ResourceMemory: 10},
			}
			config := LowNodeUtilizationConf{TargetThresholds: map[string]float64{"cpu": 85, "memory": 85}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				evictPodsFromSourceNodes([]*NodeUtilization{source}, []*NodeUtilization{target}, tasks, isContinueEvictPods, config)
			}
		})
	}
}
