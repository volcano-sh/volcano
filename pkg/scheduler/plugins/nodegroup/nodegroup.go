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
	"errors"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName       = "nodegroup"
	NodeGroupNameKey = "volcano.sh/nodegroup-name"
	rootQueueID      = "root"

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

type nodeGroupPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments

	queueAttrs map[api.QueueID]*queueAttr
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	return &nodeGroupPlugin{pluginArguments: arguments}
}

func (np *nodeGroupPlugin) Name() string {
	return PluginName
}

type queueAttr struct {
	queueID   api.QueueID
	ancestors *list.List
	affinity  *queueGroupAffinity
}

type queueGroupAffinity struct {
	queueGroupAntiAffinityRequired  sets.Set[string]
	queueGroupAntiAffinityPreferred sets.Set[string]
	queueGroupAffinityRequired      sets.Set[string]
	queueGroupAffinityPreferred     sets.Set[string]
}

func newQueueAttr(queue *api.QueueInfo) *queueAttr {
	return &queueAttr{
		queueID:   queue.UID,
		ancestors: list.New(),
		affinity:  newQueueGroupAffinity(queue),
	}
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

func (q queueGroupAffinity) score(group string) float64 {
	nodeScore := 0.0
	// Affinity: hard constraints should be checked first
	// to make sure soft constraints can cover score.
	// And same to predict, antiAffinity's priority is higher than affinity
	if q.queueGroupAffinityRequired.Has(group) {
		nodeScore += BaseScore
	}
	if q.queueGroupAffinityPreferred.Has(group) {
		nodeScore += 0.5 * BaseScore
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

func (np *nodeGroupPlugin) OnSessionOpen(ssn *framework.Session) {
	np.initQueueAttrs(ssn)
	for id, attr := range np.queueAttrs {
		if attr.affinity != nil {
			klog.V(4).Infof("queue <%v> affinity: anti-required %v, anti-preferred %v, required %v, preferred %v",
				id, attr.affinity.queueGroupAntiAffinityRequired.UnsortedList(), attr.affinity.queueGroupAntiAffinityPreferred.UnsortedList(),
				attr.affinity.queueGroupAffinityRequired.UnsortedList(), attr.affinity.queueGroupAffinityPreferred.UnsortedList())
		}
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		var score float64
		if node.Node.Labels == nil {
			return score, nil
		}
		group, exist := node.Node.Labels[NodeGroupNameKey]
		if !exist {
			return score, nil
		}
		job := ssn.Jobs[task.Job]
		attr := np.queueAttrs[job.Queue]
		if attr != nil && attr.affinity != nil {
			score = attr.affinity.score(group)
		}

		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %s, score %v", task.Namespace, task.Name, job.Queue, node.Name, group, score)
		return score, nil
	}
	ssn.AddNodeOrderFn(np.Name(), nodeOrderFn)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		if node.Node.Labels == nil {
			return newFitErr(task, node, errNodeGroupLabelNotFound)
		}

		group, exist := node.Node.Labels[NodeGroupNameKey]
		if !exist {
			return newFitErr(task, node, errNodeGroupLabelNotFound)
		}

		job := ssn.Jobs[task.Job]
		attr := np.queueAttrs[job.Queue]
		if attr.affinity == nil {
			return newFitErr(task, node, errNodeGroupAffinityNotFound)
		}

		if err := attr.affinity.predicate(group); err != nil {
			return newFitErr(task, node, errNodeUnsatisfied)
		}
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %v", task.Namespace, task.Name, job.Queue, node.Name, group)
		return nil
	}

	ssn.AddPredicateFn(np.Name(), predicateFn)
}

func (np *nodeGroupPlugin) OnSessionClose(ssn *framework.Session) {
}
