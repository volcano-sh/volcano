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

package api

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
)

type SubJobID types.UID

type SubJobGID types.UID // All subgroups within a SubGroupPolicy have the same SubJobGID.

type SubJobInfo struct {
	GID SubJobGID
	UID SubJobID
	Job JobID

	MinAvailable int32
	Priority     int32 // determined by the highest priority task in the subJob
	MatchIndex   int   // the first label value match to the pods in the subJob

	Tasks           map[TaskID]*TaskInfo
	TaskStatusIndex map[TaskStatus]TasksMap
	taskPriorities  map[int32]sets.Set[TaskID]

	AllocatedHyperNode string

	networkTopology *scheduling.NetworkTopologySpec

	// For nested SubJob structure
	Children       map[SubJobID]*SubJobInfo // Child SubJobs (empty if this is a leaf SubJob)
	IsLeaf         bool                     // Whether this is a leaf node (has tasks directly instead of children)
	MinLeafSubJobs int32                    // Minimum number of leaf SubJobs that need to be ready
}

func NewSubJobInfo(gid SubJobGID, uid SubJobID, job JobID, policy *scheduling.SubGroupPolicySpec, matchValues []string) *SubJobInfo {
	sji := &SubJobInfo{
		GID:             gid,
		UID:             uid,
		Job:             job,
		MinAvailable:    1,
		Tasks:           make(map[TaskID]*TaskInfo),
		TaskStatusIndex: make(map[TaskStatus]TasksMap),
		taskPriorities:  make(map[int32]sets.Set[TaskID]),
		Children:        make(map[SubJobID]*SubJobInfo),
		IsLeaf:          true,
	}
	if policy != nil {
		if policy.SubGroupSize != nil {
			sji.MinAvailable = *policy.SubGroupSize
		}
		if policy.NetworkTopology != nil {
			sji.networkTopology = policy.NetworkTopology.DeepCopy()
		}
	}
	if len(matchValues) > 0 {
		if v, err := strconv.Atoi(matchValues[0]); err == nil {
			sji.MatchIndex = v
		}
	}
	return sji
}

// IsHardTopologyMode return whether the subJob's network topology mode is hard and also return the highest allowed tier
func (sji *SubJobInfo) IsHardTopologyMode() (bool, int) {
	if sji.networkTopology == nil || sji.networkTopology.HighestTierAllowed == nil {
		return false, 0
	}

	return sji.networkTopology.Mode == scheduling.HardNetworkTopologyMode, *sji.networkTopology.HighestTierAllowed
}

// IsSoftTopologyMode returns whether the subJob has configured network topologies with soft mode.
func (sji *SubJobInfo) IsSoftTopologyMode() bool {
	if sji.networkTopology == nil {
		return false
	}
	return sji.networkTopology.Mode == scheduling.SoftNetworkTopologyMode
}

// WithNetworkTopology returns whether the subJob has configured network topologies
func (sji *SubJobInfo) WithNetworkTopology() bool {
	return sji.networkTopology != nil
}

func (sji *SubJobInfo) addTask(ti *TaskInfo) {
	sji.Tasks[ti.UID] = ti

	if _, found := sji.TaskStatusIndex[ti.Status]; !found {
		sji.TaskStatusIndex[ti.Status] = TasksMap{}
	}
	sji.TaskStatusIndex[ti.Status][ti.UID] = ti

	if _, found := sji.taskPriorities[ti.Priority]; !found {
		sji.taskPriorities[ti.Priority] = sets.New[TaskID]()
	}
	sji.taskPriorities[ti.Priority].Insert(ti.UID)
	if ti.Priority > sji.Priority {
		sji.Priority = ti.Priority
	}
}

func (sji *SubJobInfo) deleteTask(ti *TaskInfo) {
	delete(sji.Tasks, ti.UID)

	if tasks, found := sji.TaskStatusIndex[ti.Status]; found {
		delete(tasks, ti.UID)
		if len(tasks) == 0 {
			delete(sji.TaskStatusIndex, ti.Status)
		}
	}

	if tasks, found := sji.taskPriorities[ti.Priority]; found {
		delete(tasks, ti.UID)
		if len(tasks) == 0 {
			delete(sji.taskPriorities, ti.Priority)
			if ti.Priority > sji.Priority {
				sji.Priority = sji.getTaskHighestPriority()
			}
		}
	}
}

func (sji *SubJobInfo) getTaskHighestPriority() int32 {
	var highestPriority int32
	for priority := range sji.taskPriorities {
		if priority > highestPriority {
			highestPriority = priority
		}
	}
	return highestPriority
}

// getSubJobMatchValues determines the match values for a subjob based on the pod's labels and the sub-group policy.
// It first checks if the pod matches the policy's label selector. If not, returns nil.
// Then, it collects values from the pod's labels corresponding to the policy's MatchLabelKeys.
// If any key is missing or has an empty value, returns nil.
// If no matching rules are configured, returns an empty slice indicating the default group.
func getSubJobMatchValues(policy scheduling.SubGroupPolicySpec, pod *v1.Pod) []string {
	// Check if the pod matches the label selector specified in the policy
	if policy.LabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.LabelSelector)
		if err != nil {
			klog.Errorf("Failed to convert label selector %v: %v", policy.LabelSelector, err)
			return nil
		}

		podLabels := labels.Set(pod.Labels)
		if !selector.Matches(podLabels) {
			klog.V(4).Infof("Pod %s/%s labels %v do not match selector %q", pod.Namespace, pod.Name, podLabels, selector.String())
			return nil
		}
		klog.V(4).Infof("Pod %s/%s labels %v match selector %q", pod.Namespace, pod.Name, podLabels, selector.String())
	}

	matchValues := make([]string, 0, len(policy.MatchLabelKeys))
	// Collect match values from pod labels based on policy's MatchLabelKeys
	if len(policy.MatchLabelKeys) > 0 {
		if pod.Labels == nil {
			klog.V(4).Infof("Pod %s/%s has no labels, cannot match MatchLabelKeys %v", pod.Namespace, pod.Name, policy.MatchLabelKeys)
			return nil
		}

		for _, key := range policy.MatchLabelKeys {
			value, ok := pod.Labels[key]
			if !ok || value == "" {
				klog.V(4).Infof("Pod %s/%s missing or empty label value for key %q, cannot match", pod.Namespace, pod.Name, key)
				return nil
			}
			matchValues = append(matchValues, value)
		}

		klog.V(4).Infof("Pod %s/%s matched MatchLabelKeys %v with values %v", pod.Namespace, pod.Name, policy.MatchLabelKeys, matchValues)
		return matchValues
	}

	// Log when no matching rules are configured, using default group
	klog.V(4).Infof("No MatchLabelKeys configured for policy, pod %s/%s uses default subjob group", pod.Namespace, pod.Name)
	return matchValues
}

func getSubJobGID(job JobID, policy string) SubJobGID {
	return SubJobGID(fmt.Sprintf("%s/%s", job, policy))
}

func getSubJobID(job JobID, policy string, matchValues []string) SubJobID {
	id := strings.Join(matchValues, "-")
	if len(id) > 128 {
		hasher := fnv.New32a()
		_, _ = hasher.Write([]byte(id))
		id = rand.SafeEncodeString(fmt.Sprint(hasher.Sum32())) // todo handle collision
	}
	return SubJobID(fmt.Sprintf("%s/%s-%s", job, policy, id))
}

func (sji *SubJobInfo) IsReady() bool {
	return sji.ReadyTaskNum()+sji.PendingBestEffortTaskNum() >= sji.MinAvailable
}

func (sji *SubJobInfo) IsPipelined() bool {
	return sji.WaitingTaskNum()+sji.ReadyTaskNum()+sji.PendingBestEffortTaskNum() >= sji.MinAvailable
}

// ReadyTaskNum returns the number of tasks that are ready or that is best-effort.
func (sji *SubJobInfo) ReadyTaskNum() int32 {
	// For leaf SubJobs, count tasks directly
	if sji.IsLeaf {
		occupied := 0
		occupied += len(sji.TaskStatusIndex[Bound])
		occupied += len(sji.TaskStatusIndex[Binding])
		occupied += len(sji.TaskStatusIndex[Running])
		occupied += len(sji.TaskStatusIndex[Allocated])
		occupied += len(sji.TaskStatusIndex[Succeeded])
		return int32(occupied)
	}

	// For non-leaf SubJobs, recursively sum all children's ready tasks
	totalReady := int32(0)
	for _, child := range sji.Children {
		totalReady += child.ReadyTaskNum()
	}
	return totalReady
}

func (sji *SubJobInfo) PendingBestEffortTaskNum() int32 {
	// For leaf SubJobs, count best-effort pending tasks directly
	if sji.IsLeaf {
		count := 0
		for _, task := range sji.TaskStatusIndex[Pending] {
			if task.BestEffort {
				count++
			}
		}
		return int32(count)
	}

	// For non-leaf SubJobs, recursively sum all children's best-effort pending tasks
	totalBestEffort := int32(0)
	for _, child := range sji.Children {
		totalBestEffort += child.PendingBestEffortTaskNum()
	}
	return totalBestEffort
}

// WaitingTaskNum returns the number of tasks that are pipelined.
func (sji *SubJobInfo) WaitingTaskNum() int32 {
	// For leaf SubJobs, count pipelined tasks directly
	if sji.IsLeaf {
		return int32(len(sji.TaskStatusIndex[Pipelined]))
	}

	// For non-leaf SubJobs, recursively sum all children's waiting tasks
	totalWaiting := int32(0)
	for _, child := range sji.Children {
		totalWaiting += child.WaitingTaskNum()
	}
	return totalWaiting
}

func (sji *SubJobInfo) AllocatedTaskNum() int32 {
	count := 0
	for status, tasks := range sji.TaskStatusIndex {
		if AllocatedStatus(status) {
			count += len(tasks)
		}
	}
	return int32(count)
}

func (sji *SubJobInfo) CloneStatusFrom(source *SubJobInfo) {
	sji.AllocatedHyperNode = source.AllocatedHyperNode
}

func (sji *SubJobInfo) LeafSubJobCount() int32 {
	count := int32(0)
	if sji.IsLeaf {
		return 1
	} else {
		for _, child := range sji.Children {
			count += child.LeafSubJobCount()
		}
	}
	return count
}

// GetMinResources The current sub job is constrained to gang scheduling,
// where the MinAvailable of a sub job equals the size of itself.
// so the minimum required resource to run a sub job can be considered as
// the total initResReq of each pending task in the sub job.
func (sji *SubJobInfo) GetMinResources() *Resource {
	totalResource := EmptyResource()
	for _, task := range sji.TaskStatusIndex[Pending] {
		totalResource.Add(task.InitResreq)
	}
	return totalResource
}
