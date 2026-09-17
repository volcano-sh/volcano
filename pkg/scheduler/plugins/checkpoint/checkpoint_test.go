/*
Copyright 2025 The Volcano Authors.

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

package checkpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// buildPod builds a running pod in podgroup "pg" with the given priority and,
// if non-empty, the checkpoint-time annotation set to ckpt.
func buildPod(name string, prio int32, ckpt string) *v1.Pod {
	p := util.BuildPodWithPriority("ns1", name, "n1", v1.PodRunning,
		api.BuildResourceList("1", "1Gi"), "pg", nil, nil, &prio)
	if ckpt != "" {
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		p.Annotations[DefaultCheckpointTimeKey] = ckpt
	}
	return p
}

// popOrder returns the victim eviction order (first popped = evicted first) for
// the given pods, using priority + checkpoint victim ordering.
func popOrder(t *testing.T, pods ...*v1.Pod) []string {
	t.Helper()
	trueValue := true
	queue := util.BuildQueue("q1", 1, nil)
	node := util.BuildNode("n1", api.BuildResourceList("100", "100Gi",
		[]api.ScalarResource{{Name: "pods", Value: "100"}}...), nil)
	pg := util.BuildPodGroup("pg", "ns1", "q1", 1, nil, schedulingv1.PodGroupRunning)

	podCopies := make([]*v1.Pod, 0, len(pods))
	for _, p := range pods {
		podCopies = append(podCopies, p.DeepCopy())
	}

	tc := uthelper.TestCommonStruct{
		Plugins: map[string]framework.PluginBuilder{
			priority.PluginName: priority.New,
			PluginName:          New,
		},
		Queues:    []*schedulingv1.Queue{queue.DeepCopy()},
		Nodes:     []*v1.Node{node.DeepCopy()},
		PodGroups: []*schedulingv1.PodGroup{pg.DeepCopy()},
		Pods:      podCopies,
	}
	tiers := []conf.Tier{{
		Plugins: []conf.PluginOption{
			{Name: priority.PluginName, EnabledTaskOrder: &trueValue, EnabledVictimOrder: &trueValue},
			{Name: PluginName, EnabledVictimOrder: &trueValue},
		},
	}}
	ssn := tc.RegisterSession(tiers, nil)
	defer tc.Close()

	var victims []*api.TaskInfo
	byName := map[string]*api.TaskInfo{}
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			byName[task.Name] = task
		}
	}
	for _, p := range pods {
		victims = append(victims, byName[p.Name])
	}

	preemptor := &api.TaskInfo{Job: api.JobID("missing")}
	pq := ssn.BuildVictimsPriorityQueue(victims, preemptor)
	order := make([]string, 0, len(pods))
	for !pq.Empty() {
		order = append(order, pq.Pop().(*api.TaskInfo).Name)
	}
	return order
}

func rfc3339(sec int64) string {
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// Equal priority: the most recently checkpointed task is evicted first.
func TestCheckpointRecencyEvictsMostRecentFirst(t *testing.T) {
	old := buildPod("p-old", 1, rfc3339(100))
	recent := buildPod("p-recent", 1, rfc3339(500))
	order := popOrder(t, old, recent)
	assert.Equal(t, "p-recent", order[0], "most-recently-checkpointed must be evicted first")
	assert.Equal(t, "p-old", order[1])
}

// A task that never checkpointed is evicted last (it has the most to lose).
func TestUncheckpointedEvictedLast(t *testing.T) {
	fresh := buildPod("p-fresh", 1, "")
	checkpointed := buildPod("p-ckpt", 1, rfc3339(200))
	order := popOrder(t, fresh, checkpointed)
	assert.Equal(t, "p-ckpt", order[0], "checkpointed task evicted before never-checkpointed one")
	assert.Equal(t, "p-fresh", order[1], "never-checkpointed task must be evicted last")
}

// Priority dominates checkpoint recency: the lower-priority task is evicted
// first even though it has an older checkpoint than the higher-priority task.
func TestPriorityDominatesCheckpoint(t *testing.T) {
	lowOldCkpt := buildPod("p-low", 1, rfc3339(100))
	highRecentCkpt := buildPod("p-high", 100, rfc3339(900))
	order := popOrder(t, lowOldCkpt, highRecentCkpt)
	assert.Equal(t, "p-low", order[0], "lower-priority task must be evicted first regardless of checkpoint")
	assert.Equal(t, "p-high", order[1])
}

// An unparseable checkpoint value is treated as never checkpointed (evicted last).
func TestInvalidCheckpointTreatedAsNever(t *testing.T) {
	bad := buildPod("p-bad", 1, "not-a-timestamp")
	good := buildPod("p-good", 1, rfc3339(300))
	order := popOrder(t, bad, good)
	assert.Equal(t, "p-good", order[0])
	assert.Equal(t, "p-bad", order[1], "invalid checkpoint annotation must be treated as never checkpointed")
}
