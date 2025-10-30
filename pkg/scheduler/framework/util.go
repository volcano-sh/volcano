/*
Copyright 2022 The Volcano Authors.

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

package framework

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// PodFilter is a function to filter a pod. If pod passed return true else return false.
type PodFilter func(*v1.Pod) bool

// PodsLister interface represents anything that can list pods for a scheduler.
type PodsLister interface {
	// Returns the list of pods.
	List(labels.Selector) ([]*v1.Pod, error)
	// This is similar to "List()", but the returned slice does not
	// contain pods that don't pass `podFilter`.
	FilteredList(podFilter PodFilter, selector labels.Selector) ([]*v1.Pod, error)
}

// PodLister is used in predicate and nodeorder plugin
type PodLister struct {
	Session *Session

	CachedPods       map[api.TaskID]*v1.Pod
	Tasks            map[api.TaskID]*api.TaskInfo
	TaskWithAffinity map[api.TaskID]*api.TaskInfo
}

// PodAffinityLister is used to list pod with affinity
type PodAffinityLister struct {
	pl *PodLister
}

// HaveAffinity checks pod have affinity or not
func HaveAffinity(pod *v1.Pod) bool {
	affinity := pod.Spec.Affinity
	return affinity != nil &&
		(affinity.NodeAffinity != nil ||
			affinity.PodAffinity != nil ||
			affinity.PodAntiAffinity != nil)
}

// NewPodLister returns a PodLister generate from ssn
func NewPodLister(ssn *Session) *PodLister {
	pl := &PodLister{
		Session: ssn,

		CachedPods:       make(map[api.TaskID]*v1.Pod),
		Tasks:            make(map[api.TaskID]*api.TaskInfo),
		TaskWithAffinity: make(map[api.TaskID]*api.TaskInfo),
	}

	for _, job := range pl.Session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				pl.Tasks[task.UID] = task

				pod := pl.copyTaskPod(task)
				pl.CachedPods[task.UID] = pod

				if HaveAffinity(task.Pod) {
					pl.TaskWithAffinity[task.UID] = task
				}
			}
		}
	}

	return pl
}

// NewPodListerFromNode returns a PodLister generate from ssn
func NewPodListerFromNode(ssn *Session) *PodLister {
	pl := &PodLister{
		Session:          ssn,
		CachedPods:       make(map[api.TaskID]*v1.Pod),
		Tasks:            make(map[api.TaskID]*api.TaskInfo),
		TaskWithAffinity: make(map[api.TaskID]*api.TaskInfo),
	}

	for _, node := range pl.Session.Nodes {
		for _, task := range node.Tasks {
			if !api.AllocatedStatus(task.Status) && task.Status != api.Releasing {
				continue
			}

			pl.Tasks[task.UID] = task
			pod := pl.copyTaskPod(task)
			pl.CachedPods[task.UID] = pod
			if HaveAffinity(task.Pod) {
				pl.TaskWithAffinity[task.UID] = task
			}
		}
	}

	return pl
}

func (pl *PodLister) copyTaskPod(task *api.TaskInfo) *v1.Pod {
	pod := task.Pod.DeepCopy()
	pod.Spec.NodeName = task.NodeName
	return pod
}

// GetPod will get pod with proper nodeName, from cache or DeepCopy
// keeping this function read only to avoid concurrent panic of map
func (pl *PodLister) GetPod(task *api.TaskInfo) *v1.Pod {
	if task.NodeName == task.Pod.Spec.NodeName {
		return task.Pod
	}

	pod, found := pl.CachedPods[task.UID]
	if !found {
		// we could not write the copied pod back into cache for read only
		pod = pl.copyTaskPod(task)
		klog.Warningf("DeepCopy for pod %s/%s at PodLister.GetPod is unexpected", pod.Namespace, pod.Name)
	}
	return pod
}

// UpdateTask will update the pod nodeName in cache using nodeName
// NOT thread safe, please ensure UpdateTask is the only called function of PodLister at the same time.
func (pl *PodLister) UpdateTask(task *api.TaskInfo, nodeName string) *v1.Pod {
	pod, found := pl.CachedPods[task.UID]
	if !found {
		pod = pl.copyTaskPod(task)
		pl.CachedPods[task.UID] = pod
	}
	pod.Spec.NodeName = nodeName

	if !api.AllocatedStatus(task.Status) {
		delete(pl.Tasks, task.UID)
		if HaveAffinity(task.Pod) {
			delete(pl.TaskWithAffinity, task.UID)
		}
	} else {
		pl.Tasks[task.UID] = task
		if HaveAffinity(task.Pod) {
			pl.TaskWithAffinity[task.UID] = task
		}
	}

	return pod
}

// List method is used to list all the pods
func (pl *PodLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, task := range pl.Tasks {
		pod := pl.GetPod(task)
		if selector.Matches(labels.Set(pod.Labels)) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

// FilteredList is used to list all the pods under filter condition
func (pl *PodLister) filteredListWithTaskSet(taskSet map[api.TaskID]*api.TaskInfo, podFilter PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, task := range taskSet {
		pod := pl.GetPod(task)
		if podFilter(pod) && selector.Matches(labels.Set(pod.Labels)) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

// FilteredList is used to list all the pods under filter condition
func (pl *PodLister) FilteredList(podFilter PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	return pl.filteredListWithTaskSet(pl.Tasks, podFilter, selector)
}

// AffinityFilteredList is used to list all the pods with affinity under filter condition
func (pl *PodLister) AffinityFilteredList(podFilter PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	return pl.filteredListWithTaskSet(pl.TaskWithAffinity, podFilter, selector)
}

// AffinityLister generate a PodAffinityLister following current PodLister
func (pl *PodLister) AffinityLister() *PodAffinityLister {
	pal := &PodAffinityLister{
		pl: pl,
	}
	return pal
}

// List method is used to list all the pods
func (pal *PodAffinityLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	return pal.pl.List(selector)
}

// FilteredList is used to list all the pods with affinity under filter condition
func (pal *PodAffinityLister) FilteredList(podFilter PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	return pal.pl.AffinityFilteredList(podFilter, selector)
}

// GenerateNodeMapAndSlice returns the nodeMap and nodeSlice generated from ssn
func GenerateNodeMapAndSlice(nodes map[string]*api.NodeInfo) map[string]*k8sframework.NodeInfo {
	nodeMap := make(map[string]*k8sframework.NodeInfo)
	for _, node := range nodes {
		nodeInfo := k8sframework.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)
		nodeMap[node.Name] = nodeInfo
		// add imagestate into nodeinfo
		nodeMap[node.Name].ImageStates = node.CloneImageSummary()
	}
	return nodeMap
}

// CachedNodeInfo is used in nodeorder and predicate plugin
type CachedNodeInfo struct {
	Session *Session
}

// GetNodeInfo is used to get info of a particular node
func (c *CachedNodeInfo) GetNodeInfo(name string) (*v1.Node, error) {
	node, found := c.Session.Nodes[name]
	if !found {
		return nil, errors.NewNotFound(v1.Resource("node"), name)
	}

	return node.Node, nil
}

// NodeLister is used in nodeorder plugin
type NodeLister struct {
	Session *Session
}

// List is used to list all the nodes
func (nl *NodeLister) List() ([]*v1.Node, error) {
	var nodes []*v1.Node
	for _, node := range nl.Session.Nodes {
		nodes = append(nodes, node.Node)
	}
	return nodes, nil
}

func copyAndClearSessionFunctions(schedulerPolicy *SchedulingPolicy, ssn *Session) {
	copyFunctionsToPolicy(schedulerPolicy, ssn)
	clearSessionFunctions(ssn)
}

// copyFunctionsToPolicy Copy functions from the session into the scheduling policy.
func copyFunctionsToPolicy(policy *SchedulingPolicy, ssn *Session) {
	policy.jobOrderFns = ssn.jobOrderFns
	policy.queueOrderFns = ssn.queueOrderFns
	policy.victimQueueOrderFns = ssn.victimQueueOrderFns
	policy.taskOrderFns = ssn.taskOrderFns
	policy.clusterOrderFns = ssn.clusterOrderFns
	policy.predicateFns = ssn.predicateFns
	policy.prePredicateFns = ssn.prePredicateFns
	policy.bestNodeFns = ssn.bestNodeFns
	policy.nodeOrderFns = ssn.nodeOrderFns
	policy.batchNodeOrderFns = ssn.batchNodeOrderFns
	policy.nodeMapFns = ssn.nodeMapFns
	policy.nodeReduceFns = ssn.nodeReduceFns
	policy.hyperNodeOrderFns = ssn.hyperNodeOrderFns
	policy.preemptableFns = ssn.preemptableFns
	policy.reclaimableFns = ssn.reclaimableFns
	policy.overusedFns = ssn.overusedFns
	policy.preemptiveFns = ssn.preemptiveFns
	policy.allocatableFns = ssn.allocatableFns
	policy.jobReadyFns = ssn.jobReadyFns
	policy.jobPipelinedFns = ssn.jobPipelinedFns
	policy.jobValidFns = ssn.jobValidFns
	policy.jobEnqueueableFns = ssn.jobEnqueueableFns
	policy.jobEnqueuedFns = ssn.jobEnqueuedFns
	policy.targetJobFns = ssn.targetJobFns
	policy.reservedNodesFns = ssn.reservedNodesFns
	policy.victimTasksFns = ssn.victimTasksFns
	policy.jobStarvingFns = ssn.jobStarvingFns
	policy.simulateRemoveTaskFns = ssn.simulateRemoveTaskFns
	policy.simulateAddTaskFns = ssn.simulateAddTaskFns
	policy.simulatePredicateFns = ssn.simulatePredicateFns
	policy.simulateAllocatableFns = ssn.simulateAllocatableFns
}

// clearSessionFunctions Clear all function mappings from the session.
func clearSessionFunctions(ssn *Session) {
	ssn.jobOrderFns = make(map[string]api.CompareFn)
	ssn.queueOrderFns = make(map[string]api.CompareFn)
	ssn.victimQueueOrderFns = make(map[string]api.VictimCompareFn)
	ssn.taskOrderFns = make(map[string]api.CompareFn)
	ssn.clusterOrderFns = make(map[string]api.CompareFn)
	ssn.predicateFns = make(map[string]api.PredicateFn)
	ssn.prePredicateFns = make(map[string]api.PrePredicateFn)
	ssn.bestNodeFns = make(map[string]api.BestNodeFn)
	ssn.nodeOrderFns = make(map[string]api.NodeOrderFn)
	ssn.batchNodeOrderFns = make(map[string]api.BatchNodeOrderFn)
	ssn.nodeMapFns = make(map[string]api.NodeMapFn)
	ssn.nodeReduceFns = make(map[string]api.NodeReduceFn)
	ssn.hyperNodeOrderFns = make(map[string]api.HyperNodeOrderFn)
	ssn.preemptableFns = make(map[string]api.EvictableFn)
	ssn.reclaimableFns = make(map[string]api.EvictableFn)
	ssn.overusedFns = make(map[string]api.ValidateFn)
	ssn.preemptiveFns = make(map[string]api.ValidateWithCandidateFn)
	ssn.allocatableFns = make(map[string]api.AllocatableFn)
	ssn.jobReadyFns = make(map[string]api.ValidateFn)
	ssn.jobPipelinedFns = make(map[string]api.VoteFn)
	ssn.jobValidFns = make(map[string]api.ValidateExFn)
	ssn.jobEnqueueableFns = make(map[string]api.VoteFn)
	ssn.jobEnqueuedFns = make(map[string]api.JobEnqueuedFn)
	ssn.targetJobFns = make(map[string]api.TargetJobFn)
	ssn.reservedNodesFns = make(map[string]api.ReservedNodesFn)
	ssn.victimTasksFns = make(map[string][]api.VictimTasksFn)
	ssn.jobStarvingFns = make(map[string]api.ValidateFn)
	ssn.simulateRemoveTaskFns = make(map[string]api.SimulateRemoveTaskFn)
	ssn.simulateAddTaskFns = make(map[string]api.SimulateAddTaskFn)
	ssn.simulatePredicateFns = make(map[string]api.SimulatePredicateFn)
	ssn.simulateAllocatableFns = make(map[string]api.SimulateAllocatableFn)
}

// GetSchedulingPolicyFromJob return the SchedulingPolicy from the job.
func GetSchedulingPolicyFromJob(job *api.JobInfo) *SchedulingPolicy {
	schedulingPolicy, _ := job.SchedulingPolicy.(*SchedulingPolicy)
	return schedulingPolicy
}

// GetSchedulingPolicyFromQueue return the SchedulingPolicy from the queue.
func GetSchedulingPolicyFromQueue(queue *api.QueueInfo) *SchedulingPolicy {
	schedulingPolicy, _ := queue.SchedulingPolicy.(*SchedulingPolicy)
	return schedulingPolicy
}

// GetSchedulingPolicyFromTask return the SchedulingPolicy from the task.
func GetSchedulingPolicyFromTask(task *api.TaskInfo) *SchedulingPolicy {
	schedulingPolicy, _ := task.SchedulingPolicy.(*SchedulingPolicy)
	return schedulingPolicy
}
