/*
Copyright 2017 The Kubernetes Authors.
Copyright 2017-2025 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/apis/pkg/apis/scheduling"
)

// ContainsHardPodGroupAntiAffinity returns whether the job has hard cross-PodGroup anti-affinity.
func (ji *JobInfo) ContainsHardPodGroupAntiAffinity() bool {
	if ji.PodGroup == nil || ji.PodGroup.Spec.TopologyAffinity == nil {
		return false
	}
	anti := ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity
	return anti != nil && len(anti.Required) > 0
}

// HasPreferredPodGroupAntiAffinity returns whether the job has soft cross-PodGroup anti-affinity.
func (ji *JobInfo) HasPreferredPodGroupAntiAffinity() bool {
	if ji.PodGroup == nil || ji.PodGroup.Spec.TopologyAffinity == nil {
		return false
	}
	anti := ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity
	return anti != nil && len(anti.Preferred) > 0
}

// RequiresHyperNodeTopology reports whether the job needs a ready topology,
// including preferred-only policies that are evaluated through domain scoring.
func (ji *JobInfo) RequiresHyperNodeTopology() bool {
	return ji.ContainsNetworkTopology() || ji.ContainsHardPodGroupAntiAffinity() || ji.HasPreferredPodGroupAntiAffinity()
}

// RequiredPodGroupAntiAffinityTerms returns hard cross-PodGroup anti-affinity terms.
func (ji *JobInfo) RequiredPodGroupAntiAffinityTerms() []scheduling.PodGroupAffinityTerm {
	if !ji.ContainsHardPodGroupAntiAffinity() {
		return nil
	}
	return ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity.Required
}

// PreferredPodGroupAntiAffinityTerms returns soft cross-PodGroup anti-affinity terms.
func (ji *JobInfo) PreferredPodGroupAntiAffinityTerms() []scheduling.PodGroupAffinityTerm {
	if ji.PodGroup == nil || ji.PodGroup.Spec.TopologyAffinity == nil ||
		ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity == nil {
		return nil
	}
	return ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity.Preferred
}

// ResolvePodGroupTermTier resolves topologyTier or topologyTierName on a PodGroupAffinityTerm.
func ResolvePodGroupTermTier(term scheduling.PodGroupAffinityTerm, tierNameMap HyperNodeTierNameMap) (int, error) {
	if term.TopologyTier != nil && term.TopologyTierName != "" {
		return 0, fmt.Errorf("topologyTier and topologyTierName are mutually exclusive")
	}
	if term.TopologyTier != nil {
		return int(*term.TopologyTier), nil
	}
	if term.TopologyTierName != "" {
		tier, ok := tierNameMap[term.TopologyTierName]
		if !ok {
			return 0, fmt.Errorf("unknown topologyTierName %q", term.TopologyTierName)
		}
		return tier, nil
	}
	return 0, fmt.Errorf("topologyTier or topologyTierName must be set")
}

// HasTopologyDomainTasks reports whether the job has tasks that currently
// occupy a topology domain, including pipelined tasks with an assigned node.
func (ji *JobInfo) HasTopologyDomainTasks() bool {
	return len(collectJobAllocatedTasks(ji)) > 0
}

// HasTopologyDomainTasks reports whether the subJob has tasks that currently
// occupy a topology domain, including pipelined tasks with an assigned node.
func (sji *SubJobInfo) HasTopologyDomainTasks() bool {
	return len(collectSubJobAllocatedTasks(sji)) > 0
}

func collectJobAllocatedTasks(job *JobInfo) []*TaskInfo {
	tasks := make([]*TaskInfo, 0)
	for _, subJob := range job.SubJobs {
		tasks = append(tasks, collectSubJobAllocatedTasks(subJob)...)
	}
	if len(tasks) > 0 {
		return tasks
	}
	for status, taskMap := range job.TaskStatusIndex {
		if !occupiesTopologyDomain(status, nil) {
			continue
		}
		for _, task := range taskMap {
			if !occupiesTopologyDomain(status, task) {
				continue
			}
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func collectSubJobAllocatedTasks(subJob *SubJobInfo) []*TaskInfo {
	if subJob == nil {
		return nil
	}
	tasks := make([]*TaskInfo, 0, subJob.AllocatedTaskNum())
	for status, taskMap := range subJob.TaskStatusIndex {
		if !occupiesTopologyDomain(status, nil) {
			continue
		}
		for _, task := range taskMap {
			if !occupiesTopologyDomain(status, task) {
				continue
			}
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func occupiesTopologyDomain(status TaskStatus, task *TaskInfo) bool {
	if AllocatedStatus(status) {
		return true
	}
	if status != Pipelined {
		return false
	}
	// With a nil task this is the cheap status-level precheck used by callers.
	return task == nil || task.NodeName != ""
}

// OccupiesTopologyDomain reports whether this task contributes a Node to placement.
func (ti *TaskInfo) OccupiesTopologyDomain() bool {
	return ti != nil && ti.NodeName != "" && occupiesTopologyDomain(ti.Status, ti)
}

func getAllocatedHyperNodeFromTasks(
	tasks []*TaskInfo,
	hyperNodeSet sets.Set[string],
	nodesByHyperNode map[string]sets.Set[string],
	hyperNodes HyperNodeInfoMap,
) string {
	if len(tasks) == 0 {
		return ""
	}

	var candidateHyperNodes sets.Set[string]
	for _, task := range tasks {
		if task.NodeName == "" {
			continue
		}

		search := hyperNodeSet
		if candidateHyperNodes != nil {
			search = candidateHyperNodes
		}

		taskHyperNodes := sets.New[string]()
		for hyperNode := range search {
			if nodes, found := nodesByHyperNode[hyperNode]; found && nodes.Has(task.NodeName) {
				taskHyperNodes.Insert(hyperNode)
			}
		}
		if taskHyperNodes.Len() == 0 {
			return ""
		}
		candidateHyperNodes = taskHyperNodes
	}
	return getLowestTierHyperNode(candidateHyperNodes, hyperNodes)
}

func getLowestTierHyperNode(hyperNodeNames sets.Set[string], hyperNodes HyperNodeInfoMap) string {
	if hyperNodeNames == nil || hyperNodeNames.Len() == 0 {
		return ""
	}

	var lowest *HyperNodeInfo
	for name := range hyperNodeNames {
		hyperNode, found := hyperNodes[name]
		if !found {
			continue
		}
		if lowest == nil || hyperNode.Tier() < lowest.Tier() {
			lowest = hyperNode
		}
	}
	if lowest == nil {
		return ""
	}
	return lowest.Name
}
