/*
Copyright 2019 The Volcano Authors.

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

package apis

import (
	"fmt"

	v1 "k8s.io/api/core/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// JobInfo struct.
type JobInfo struct {
	Namespace string
	Name      string

	Job  *batch.Job
	Pods map[string]map[string]*v1.Pod
	// Partitions taskName:PartitionInfo
	Partitions map[string]*PartitionInfo
}

type PartitionInfo struct {
	// Partition partitionID:{podName:pod}
	Partition       map[string]map[string]*v1.Pod
	MatchPolicy     []*scheduling.MatchPolicySpec
	NetworkTopology *batch.NetworkTopologySpec
}

// Clone function clones the k8s pod values to the JobInfo struct.
func (ji *JobInfo) Clone() *JobInfo {
	job := &JobInfo{
		Namespace: ji.Namespace,
		Name:      ji.Name,
		Job:       ji.Job,

		Pods:       make(map[string]map[string]*v1.Pod, len(ji.Pods)),
		Partitions: make(map[string]*PartitionInfo, len(ji.Partitions)),
	}

	for key, pods := range ji.Pods {
		job.Pods[key] = make(map[string]*v1.Pod, len(pods))
		for pn, pod := range pods {
			job.Pods[key][pn] = pod
		}
	}

	for taskName, partitionInfo := range ji.Partitions {
		job.Partitions[taskName] = &PartitionInfo{}
		partition := make(map[string]map[string]*v1.Pod, len(partitionInfo.Partition))
		for partitionID, pods := range partitionInfo.Partition {
			group := make(map[string]*v1.Pod, len(pods))
			for podName, pod := range pods {
				group[podName] = pod
			}
			partition[partitionID] = group
		}
		job.Partitions[taskName].Partition = partition
		if partitionInfo.NetworkTopology != nil {
			job.Partitions[taskName].NetworkTopology = partitionInfo.NetworkTopology.DeepCopy()
		}
		matchPolicy := make([]*scheduling.MatchPolicySpec, 0, len(partitionInfo.MatchPolicy))
		for _, value := range partitionInfo.MatchPolicy {
			matchPolicy = append(matchPolicy, value.DeepCopy())
		}
		job.Partitions[taskName].MatchPolicy = matchPolicy
	}

	return job
}

// SetJob sets the volcano jobs values to the JobInfo struct.
func (ji *JobInfo) SetJob(job *batch.Job) {
	ji.Name = job.Name
	ji.Namespace = job.Namespace
	ji.Job = job
	ji.Partitions = make(map[string]*PartitionInfo)
	for _, taskSpec := range job.Spec.Tasks {
		if taskSpec.PartitionPolicy == nil {
			continue
		}
		ji.Partitions[taskSpec.Name] = &PartitionInfo{}
		ji.Partitions[taskSpec.Name].Partition = make(map[string]map[string]*v1.Pod)
		if taskSpec.PartitionPolicy.NetworkTopology != nil {
			nt := &batch.NetworkTopologySpec{
				Mode:               taskSpec.PartitionPolicy.NetworkTopology.Mode,
				HighestTierAllowed: taskSpec.PartitionPolicy.NetworkTopology.HighestTierAllowed,
			}
			ji.Partitions[taskSpec.Name].NetworkTopology = nt
		}
		ji.Partitions[taskSpec.Name].MatchPolicy = make([]*scheduling.MatchPolicySpec, 0)
		labelKey := fmt.Sprintf("volcano.sh/%s-subgroup-id", taskSpec.Name)
		matchPolicySpec := &scheduling.MatchPolicySpec{
			LabelKey: labelKey,
		}
		ji.Partitions[taskSpec.Name].MatchPolicy = append(ji.Partitions[taskSpec.Name].MatchPolicy, matchPolicySpec)
	}
	for taskName, podMap := range ji.Pods {
		for _, pod := range podMap {
			if partitionInfo, found := ji.Partitions[taskName]; found {
				partitionID := getPartitionID(pod)
				if partitionID == "" {
					continue
				}
				if _, found := partitionInfo.Partition[partitionID]; !found {
					partitionInfo.Partition[partitionID] = make(map[string]*v1.Pod)
				}
				partitionInfo.Partition[partitionID][pod.Name] = pod
			}
		}
	}
}

// AddPod adds the k8s pod object values to the Pods field
// of JobStruct if it doesn't exist. Otherwise it throws error.
func (ji *JobInfo) AddPod(pod *v1.Pod) error {
	taskName, found := pod.Annotations[batch.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to find taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	_, found = pod.Annotations[batch.JobVersion]
	if !found {
		return fmt.Errorf("failed to find jobVersion of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if _, found := ji.Pods[taskName]; !found {
		ji.Pods[taskName] = make(map[string]*v1.Pod)
	}
	if _, found := ji.Pods[taskName][pod.Name]; found {
		return fmt.Errorf("duplicated pod")
	}
	ji.Pods[taskName][pod.Name] = pod

	if ji.Partitions != nil {
		if partitionInfo, found := ji.Partitions[taskName]; found {
			partitionID := getPartitionID(pod)
			if partitionID == "" {
				return nil
			}
			if _, found := partitionInfo.Partition[partitionID]; !found {
				partitionInfo.Partition[partitionID] = make(map[string]*v1.Pod)
			}
			partitionInfo.Partition[partitionID][pod.Name] = pod
		}
	}

	return nil
}

// UpdatePod updates the k8s pod object values to the existing pod.
func (ji *JobInfo) UpdatePod(pod *v1.Pod) error {
	taskName, found := pod.Annotations[batch.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to find taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}
	_, found = pod.Annotations[batch.JobVersion]
	if !found {
		return fmt.Errorf("failed to find jobVersion of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if _, found := ji.Pods[taskName]; !found {
		return fmt.Errorf("can not find task %s in cache", taskName)
	}
	if _, found := ji.Pods[taskName][pod.Name]; !found {
		return fmt.Errorf("can not find pod <%s/%s> in cache",
			pod.Namespace, pod.Name)
	}
	ji.Pods[taskName][pod.Name] = pod

	if ji.Partitions != nil {
		if partitionInfo, found := ji.Partitions[taskName]; found {
			partitionID := getPartitionID(pod)
			if partitionID == "" {
				return nil
			}
			if _, found := partitionInfo.Partition[partitionID]; found {
				if _, found := partitionInfo.Partition[partitionID][pod.Name]; found {
					partitionInfo.Partition[partitionID][pod.Name] = pod
				}
			}
		}
	}
	return nil
}

// DeletePod deletes the given k8s pod from the JobInfo struct.
func (ji *JobInfo) DeletePod(pod *v1.Pod) error {
	taskName, found := pod.Annotations[batch.TaskSpecKey]
	if !found {
		return fmt.Errorf("failed to find taskName of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}
	_, found = pod.Annotations[batch.JobVersion]
	if !found {
		return fmt.Errorf("failed to find jobVersion of Pod <%s/%s>",
			pod.Namespace, pod.Name)
	}

	if pods, found := ji.Pods[taskName]; found {
		delete(pods, pod.Name)
		if len(pods) == 0 {
			delete(ji.Pods, taskName)
		}
	}

	if ji.Partitions != nil {
		if partitionInfo, found := ji.Partitions[taskName]; found {
			partitionID := getPartitionID(pod)
			if partitionID == "" {
				return nil
			}
			if _, found := partitionInfo.Partition[partitionID]; found {
				delete(partitionInfo.Partition[partitionID], pod.Name)
				if len(partitionInfo.Partition[partitionID]) == 0 {
					delete(partitionInfo.Partition, partitionID)
				}
			}
		}
	}

	return nil
}

func getPartitionID(pod *v1.Pod) string {
	value, ok := pod.Labels[batch.Partitionkey]
	if ok {
		return value
	}
	return ""
}
