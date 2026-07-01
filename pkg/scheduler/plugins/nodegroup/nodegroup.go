/*
Copyright 2023 The Volcano Authors.

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

package nodegroup

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName  = "nodegroup"
	rootQueueID = "root"

	BaseScore = 100
)

//
// User should specify arguments in the config in this format:
//
//  actions: "reclaim, allocate, backfill, preempt"
//  tiers:
//  - plugins:
//    - name: priority
//    - name: gang
//    - name: conformance
//  - plugins:
//    - name: drf
//    - name: predicates
//    - name: proportion
//    - name: nodegroup
//      #enableHierarchy: true # If user wants to enable hierarchy, set this to true. Queue without affinity will inherit affinity from its nearest ancestor.
//      arguments:
//        enablePreferredOrder: true # If user wants preferred nodegroups to be scored by their list order (earlier = higher priority), set this to true.

type nodeGroupPlugin struct {
	// Arguments given for the plugin
	pluginArguments      framework.Arguments
	strict               bool
	enablePreferredOrder bool
	queueAttrs           map[api.QueueID]*queueAttr
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	nodeGroupPlugin := &nodeGroupPlugin{pluginArguments: arguments, strict: true}
	arguments.GetBool(&nodeGroupPlugin.strict, "strict")
	arguments.GetBool(&nodeGroupPlugin.enablePreferredOrder, "enablePreferredOrder")
	return nodeGroupPlugin
}

func (np *nodeGroupPlugin) Name() string {
	return PluginName
}

type queueAttr struct {
	queueID                 api.QueueID
	ancestors               *list.List
	affinity                *queueGroupAffinity
	nodeGroupResourceLimits map[string]*nodeGroupResourceLimit
	nodeGroupAllocated      map[string]*api.Resource
	resourceLimitErr        error
}

type nodeGroupResourceLimit struct {
	resource *api.Resource
	names    sets.Set[v1.ResourceName]
}

type queueGroupAffinity struct {
	queueGroupAntiAffinityRequired     sets.Set[string]
	queueGroupAntiAffinityPreferred    sets.Set[string]
	queueGroupAffinityRequired         sets.Set[string]
	queueGroupAffinityPreferred        sets.Set[string]
	queueGroupAffinityPreferredIndexes map[string]int
}

func newQueueAttr(queue *api.QueueInfo) *queueAttr {
	limits, err := parseNodeGroupResourceLimits(queue)
	return &queueAttr{
		queueID:                 queue.UID,
		ancestors:               list.New(),
		affinity:                newQueueGroupAffinity(queue),
		nodeGroupResourceLimits: limits,
		nodeGroupAllocated:      make(map[string]*api.Resource),
		resourceLimitErr:        err,
	}
}

func parseNodeGroupResourceLimits(queue *api.QueueInfo) (map[string]*nodeGroupResourceLimit, error) {
	if queue == nil || queue.Queue == nil || len(queue.Queue.Annotations) == 0 {
		return nil, nil
	}
	raw := queue.Queue.Annotations[schedulingv1.NodeGroupResourceLimitsAnnotationKey]
	if raw == "" {
		return nil, nil
	}

	resourceLists := map[string]v1.ResourceList{}
	if err := json.Unmarshal([]byte(raw), &resourceLists); err != nil {
		return nil, fmt.Errorf("parse annotation %s: %w", schedulingv1.NodeGroupResourceLimitsAnnotationKey, err)
	}

	limits := make(map[string]*nodeGroupResourceLimit, len(resourceLists))
	for group, resourceList := range resourceLists {
		limit := &nodeGroupResourceLimit{
			resource: api.NewResource(resourceList),
			names:    sets.New[v1.ResourceName](),
		}
		for name := range resourceList {
			limit.names.Insert(name)
		}
		limits[group] = limit
	}
	return limits, nil
}

func newQueueGroupAffinity(queue *api.QueueInfo) *queueGroupAffinity {
	if queue.Queue.Spec.Affinity == nil {
		return nil
	}

	affinity := &queueGroupAffinity{
		queueGroupAntiAffinityRequired:  sets.New[string](),
		queueGroupAntiAffinityPreferred: sets.New[string](),
		queueGroupAffinityRequired:      sets.New[string](),
		queueGroupAffinityPreferred:     sets.New[string](),
	}

	nodeGroupAffinity := queue.Queue.Spec.Affinity.NodeGroupAffinity
	if nodeGroupAffinity != nil {
		affinity.queueGroupAffinityPreferred.Insert(nodeGroupAffinity.PreferredDuringSchedulingIgnoredDuringExecution...)
		affinity.queueGroupAffinityRequired.Insert(nodeGroupAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
		affinity.queueGroupAffinityPreferredIndexes = make(map[string]int, len(nodeGroupAffinity.PreferredDuringSchedulingIgnoredDuringExecution))
		for i, group := range nodeGroupAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
			affinity.queueGroupAffinityPreferredIndexes[group] = i
		}
	}
	nodeGroupAntiAffinity := queue.Queue.Spec.Affinity.NodeGroupAntiAffinity
	if nodeGroupAntiAffinity != nil {
		affinity.queueGroupAntiAffinityPreferred.Insert(nodeGroupAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution...)
		affinity.queueGroupAntiAffinityRequired.Insert(nodeGroupAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
	}

	return affinity
}

func (q queueGroupAffinity) predicate(group string) error {
	flag := false
	if q.queueGroupAffinityRequired.Has(group) {
		flag = true
	}
	if q.queueGroupAffinityPreferred.Has(group) {
		flag = true
	}
	// AntiAffinity: hard constraints should be checked first
	// to make sure soft constraints satisfy
	// and antiAffinity's priority is higher than affinity
	if q.queueGroupAntiAffinityRequired.Has(group) {
		flag = false
	}
	if q.queueGroupAntiAffinityPreferred.Has(group) {
		flag = true
	}
	if !flag {
		return errors.New("not satisfy")
	}
	return nil
}

func (q queueGroupAffinity) score(group string, enablePreferredOrder bool) float64 {
	nodeScore := 0.0
	// Affinity: hard constraints should be checked first
	// to make sure soft constraints can cover score.
	// And same to predict, antiAffinity's priority is higher than affinity
	if q.queueGroupAffinityRequired.Has(group) {
		nodeScore += BaseScore
	}
	if enablePreferredOrder {
		if i, ok := q.queueGroupAffinityPreferredIndexes[group]; ok {
			n := len(q.queueGroupAffinityPreferredIndexes)
			nodeScore += 0.5 * BaseScore * float64(n-i) / float64(n)
		}
	} else {
		if q.queueGroupAffinityPreferred.Has(group) {
			nodeScore += 0.5 * BaseScore
		}
	}
	if q.queueGroupAntiAffinityPreferred.Has(group) {
		nodeScore = -1
	}

	return nodeScore
}

func (np *nodeGroupPlugin) initQueueAttrs(ssn *framework.Session) {
	np.queueAttrs = make(map[api.QueueID]*queueAttr)

	// 1. Init queue attributes for every queue.
	for queueID, queue := range ssn.Queues {
		np.queueAttrs[queueID] = newQueueAttr(queue)
	}

	if !ssn.HierarchyEnabled(np.Name()) {
		return
	}

	// 2. Build ancestor lists for each queue.
	visited := make(map[api.QueueID]struct{})
	for _, attr := range np.queueAttrs {
		np.buildAncestors(ssn, attr, visited)
	}

	// 3. Inherit affinity from ancestors.
	for _, attr := range np.queueAttrs {
		np.updateAffinityFromAncestor(attr)
	}
}

func (np *nodeGroupPlugin) buildAncestors(ssn *framework.Session, attr *queueAttr, visited map[api.QueueID]struct{}) {
	if _, exist := visited[attr.queueID]; exist {
		klog.Warningf("Cycle detected in queue hierarchy for queue %s, skipping ancestor building.", attr.queueID)
		return
	}
	visited[attr.queueID] = struct{}{}
	defer delete(visited, attr.queueID)

	if attr.queueID == rootQueueID {
		return
	}

	queueInfo := ssn.Queues[attr.queueID]
	parentID := api.QueueID(rootQueueID)
	if queueInfo.Queue.Spec.Parent != "" {
		parentID = api.QueueID(queueInfo.Queue.Spec.Parent)
	}

	parentInfo := ssn.Queues[parentID]
	if parentInfo == nil {
		klog.Warningf("Parent queue %s not found for queue %s, unable to build hierarchy.", queueInfo.Queue.Spec.Parent, queueInfo.Name)
		return
	}

	parentAttr, found := np.queueAttrs[parentInfo.UID]
	if !found {
		klog.Warningf("Parent queue attribute not found for %s, which should not happen.", parentInfo.Name)
		return
	}

	// Recursively build ancestors for the parent queue.
	np.buildAncestors(ssn, parentAttr, visited)

	// A queue's ancestors are its parent plus all the parent's ancestors.
	attr.ancestors.PushBack(parentAttr)
	attr.ancestors.PushBackList(parentAttr.ancestors)
}

func (np *nodeGroupPlugin) updateAffinityFromAncestor(attr *queueAttr) {
	// If queue has its own affinity, directly return
	if attr.affinity != nil {
		return
	}
	// Inherits from the affinity of the nearest ancestor
	for e := attr.ancestors.Front(); e != nil; e = e.Next() {
		ancestorAttr, ok := e.Value.(*queueAttr)
		if !ok {
			klog.Errorf("Ancestor <%s> of Queue <%s> is not a queueAttr", e.Value, attr.queueID)
			continue
		}
		if ancestorAttr.affinity != nil {
			// Ref to the same affinity object
			attr.affinity = ancestorAttr.affinity
			return
		}
	}
}

func (np *nodeGroupPlugin) initNodeGroupAllocated(ssn *framework.Session) {
	for _, job := range ssn.Jobs {
		attr := np.queueAttrs[job.Queue]
		if attr == nil {
			continue
		}
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}
			for _, task := range tasks {
				np.addNodeGroupAllocated(ssn, attr, task)
			}
		}
	}
}

func (np *nodeGroupPlugin) addNodeGroupAllocated(ssn *framework.Session, attr *queueAttr, task *api.TaskInfo) {
	group, ok := np.taskNodeGroup(ssn, task)
	if !ok {
		return
	}
	allocated := attr.nodeGroupAllocated[group]
	if allocated == nil {
		allocated = api.EmptyResource()
		attr.nodeGroupAllocated[group] = allocated
	}
	allocated.Add(task.Resreq)
}

func (np *nodeGroupPlugin) subNodeGroupAllocated(ssn *framework.Session, attr *queueAttr, task *api.TaskInfo) {
	group, ok := np.taskNodeGroup(ssn, task)
	if !ok {
		return
	}
	allocated := attr.nodeGroupAllocated[group]
	if allocated == nil {
		return
	}
	allocated.Sub(task.Resreq)
}

func (np *nodeGroupPlugin) taskNodeGroup(ssn *framework.Session, task *api.TaskInfo) (string, bool) {
	if task == nil || task.NodeName == "" {
		return "", false
	}
	node := ssn.Nodes[task.NodeName]
	if node == nil || node.Node == nil || node.Node.Labels == nil {
		return "", false
	}
	group, ok := node.Node.Labels[schedulingv1.NodeGroupNameKey]
	return group, ok
}

func (np *nodeGroupPlugin) checkNodeGroupResourceLimit(task *api.TaskInfo, group string, attr *queueAttr) error {
	if attr.resourceLimitErr != nil {
		return attr.resourceLimitErr
	}
	if len(attr.nodeGroupResourceLimits) == 0 {
		return nil
	}
	limit := attr.nodeGroupResourceLimits[group]
	if limit == nil {
		return nil
	}

	allocated := attr.nodeGroupAllocated[group]
	if allocated == nil {
		allocated = api.EmptyResource()
	}
	futureUsed := allocated.Clone().Add(task.Resreq)
	if resourceNames := limitedResourceExceededNames(futureUsed, limit); len(resourceNames) > 0 {
		return fmt.Errorf("nodegroup %s resource limit exceeded, insufficient resources: %v", group, resourceNames)
	}
	return nil
}

func limitedResourceExceededNames(used *api.Resource, limit *nodeGroupResourceLimit) []string {
	if used == nil || limit == nil || limit.resource == nil || limit.names.Len() == 0 {
		return nil
	}

	var resourceNames []string
	if limit.names.Has(v1.ResourceCPU) && greaterThanResourceLimit(used.MilliCPU, limit.resource.MilliCPU) {
		resourceNames = append(resourceNames, string(v1.ResourceCPU))
	}
	if limit.names.Has(v1.ResourceMemory) && greaterThanResourceLimit(used.Memory, limit.resource.Memory) {
		resourceNames = append(resourceNames, string(v1.ResourceMemory))
	}

	for name := range limit.names {
		switch name {
		case v1.ResourceCPU, v1.ResourceMemory:
			continue
		}
		if greaterThanResourceLimit(used.Get(name), limit.resource.Get(name)) {
			resourceNames = append(resourceNames, string(name))
		}
	}
	return resourceNames
}

func greaterThanResourceLimit(used, limit float64) bool {
	return used > limit && used-limit >= api.GetMinResource()
}

func (np *nodeGroupPlugin) OnSessionOpen(ssn *framework.Session) {
	np.initQueueAttrs(ssn)
	np.initNodeGroupAllocated(ssn)
	for id, attr := range np.queueAttrs {
		if attr.affinity != nil {
			klog.V(4).Infof("queue <%v> affinity: anti-required %v, anti-preferred %v, required %v, preferred %v",
				id, attr.affinity.queueGroupAntiAffinityRequired.UnsortedList(), attr.affinity.queueGroupAntiAffinityPreferred.UnsortedList(),
				attr.affinity.queueGroupAffinityRequired.UnsortedList(), attr.affinity.queueGroupAffinityPreferred.UnsortedList())
		}
		if attr.resourceLimitErr != nil {
			klog.Errorf("queue <%v> has invalid %s annotation: %v", id, schedulingv1.NodeGroupResourceLimitsAnnotationKey, attr.resourceLimitErr)
		}
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		var score float64
		if node.Node.Labels == nil {
			return score, nil
		}
		group, exist := node.Node.Labels[schedulingv1.NodeGroupNameKey]
		if !exist {
			return score, nil
		}
		job := ssn.Jobs[task.Job]
		if job == nil {
			klog.Warningf("[nodegroup] Skip node scoring for task <%s/%s>: job <%s> not found in session (orphaned task from deleted PodGroup)",
				task.Namespace, task.Name, task.Job)
			return score, nil
		}
		attr := np.queueAttrs[job.Queue]
		if attr != nil && attr.affinity != nil {
			score = attr.affinity.score(group, np.enablePreferredOrder)
		}

		klog.V(4).Infof("[nodegroup] task <%s>/<%s> queue %s on node %s of nodegroup %s, score %v", task.Namespace, task.Name, job.Queue, node.Name, group, score)
		return score, nil
	}
	ssn.AddNodeOrderFn(np.Name(), nodeOrderFn)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		job := ssn.Jobs[task.Job]
		if job == nil {
			klog.Warningf("[nodegroup] Skip predicate for task <%s/%s>: job <%s> not found in session (orphaned task from deleted PodGroup)",
				task.Namespace, task.Name, task.Job)
			return fmt.Errorf("job %s not found in session", task.Job)
		}
		attr := np.queueAttrs[job.Queue]
		if attr != nil && attr.resourceLimitErr != nil {
			return newFitErr(task, node, errNodeGroupResourceLimitInvalid)
		}

		// Check if the queue has any node group affinity rules
		unsetAffinity := attr == nil || attr.affinity == nil ||
			(attr.affinity.queueGroupAffinityRequired.Len() == 0 &&
				attr.affinity.queueGroupAffinityPreferred.Len() == 0 &&
				attr.affinity.queueGroupAntiAffinityRequired.Len() == 0 &&
				attr.affinity.queueGroupAntiAffinityPreferred.Len() == 0)

		group, exist := "", false
		if node.Node.Labels != nil {
			group, exist = node.Node.Labels[schedulingv1.NodeGroupNameKey]
		}

		if !exist {
			// In non-strict mode, if the queue also has no affinity requirements,
			// the task is allowed to be scheduled on this node.
			if !np.strict && unsetAffinity {
				return nil
			}
			// Otherwise (in strict mode, or if the queue has affinity rules), the node is not a fit.
			return newFitErr(task, node, errNodeGroupLabelNotFound)
		}

		// The node has a group label, but the queue has no affinity rules, we don't allow schedule onto this node.
		if unsetAffinity {
			return newFitErr(task, node, errNodeGroupAffinityNotFound)
		}

		if err := attr.affinity.predicate(group); err != nil {
			return newFitErr(task, node, errNodeUnsatisfied)
		}
		if err := np.checkNodeGroupResourceLimit(task, group, attr); err != nil {
			klog.V(3).Infof("task <%s>/<%s> queue %s on nodegroup %s rejected by nodegroup resource limit: %v", task.Namespace, task.Name, job.Queue, group, err)
			return newFitErr(task, node, errNodeGroupResourceLimitExceeded)
		}
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %v", task.Namespace, task.Name, job.Queue, node.Name, group)
		return nil
	}

	ssn.AddPredicateFn(np.Name(), predicateFn)

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			if job == nil {
				return
			}
			attr := np.queueAttrs[job.Queue]
			if attr == nil {
				return
			}
			np.addNodeGroupAllocated(ssn, attr, event.Task)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			if job == nil {
				return
			}
			attr := np.queueAttrs[job.Queue]
			if attr == nil {
				return
			}
			np.subNodeGroupAllocated(ssn, attr, event.Task)
		},
	})
}

func (np *nodeGroupPlugin) OnSessionClose(ssn *framework.Session) {
	np.queueAttrs = nil
}
