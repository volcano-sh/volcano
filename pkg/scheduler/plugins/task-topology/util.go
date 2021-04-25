/*
Copyright 2021 The Volcano Authors.

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

package tasktopology

import (
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin
	PluginName = "task-topology"
	// PluginWeight is task-topology plugin weight in nodeOrderFn
	PluginWeight = "task-topology.weight"
	// JobAffinityKey is the key to read in task-topology arguments from job annotations
	JobAffinityKey = "volcano.sh/task-topology"
	// OutOfBucket indicates task is outside of any bucket
	OutOfBucket = -1

	// JobAffinityAnnotations is the key to read in task-topology affinity arguments from podgroup annotations
	JobAffinityAnnotations = "volcano.sh/task-topology-affinity"
	// JobAntiAffinityAnnotations is the key to read in task-topology anti-affinity arguments from podgroup annotations
	JobAntiAffinityAnnotations = "volcano.sh/task-topology-anti-affinity"
	// TaskOrderAnnotations is the key to read in task-topology task order arguments from podgroup annotations
	TaskOrderAnnotations = "volcano.sh/task-topology-task-order"
)

// TaskTopology is struct used to save affinity infos of a job read from job plugin or annotations
type TaskTopology struct {
	Affinity     [][]string `json:"affinity,omitempty"`
	AntiAffinity [][]string `json:"antiAffinity,omitempty"`
	TaskOrder    []string   `json:"taskOrder,omitempty"`
}

func calculateWeight(args framework.Arguments) int {
	/*
	   User Should give taskTopologyWeight in this format(task-topology.weight).

	   actions: "enqueue, reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: task-topology
	       arguments:
	         task-topology.weight: 10
	*/
	// Values are initialized to 1.
	weight := 1

	args.GetInt(&weight, PluginWeight)

	return weight
}

func getTaskName(task *api.TaskInfo) string {
	return task.Pod.Annotations[v1alpha1.TaskSpecKey]
}

func addAffinity(m map[string]map[string]struct{}, src, dst string) {
	srcMap, ok := m[src]
	if !ok {
		srcMap = make(map[string]struct{})
		m[src] = srcMap
	}
	srcMap[dst] = struct{}{}
}

func noPendingTasks(job *api.JobInfo) bool {
	return len(job.TaskStatusIndex[api.Pending]) == 0
}

// TaskOrder is struct used to save task order
type TaskOrder struct {
	tasks   []*api.TaskInfo
	manager *JobManager
}

func (p *TaskOrder) Len() int { return len(p.tasks) }

func (p *TaskOrder) Swap(l, r int) {
	p.tasks[l], p.tasks[r] = p.tasks[r], p.tasks[l]
}

func (p *TaskOrder) Less(l, r int) bool {
	L := p.tasks[l]
	R := p.tasks[r]

	LHasNode := L.NodeName != ""
	RHasNode := R.NodeName != ""
	if LHasNode || RHasNode {
		// the task bounded would have high priority
		if LHasNode != RHasNode {
			return !LHasNode
		}
		// all bound, any order is alright
		return L.NodeName > R.NodeName
	}

	result := p.manager.taskAffinityOrder(L, R)
	// they have the same taskAffinity order, any order is alright
	if result == 0 {
		return L.Name > R.Name
	}
	return result < 0
}
