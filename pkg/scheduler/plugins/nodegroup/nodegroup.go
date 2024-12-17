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
	"errors"

	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	sch "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName       = "nodegroup"
	NodeGroupNameKey = "volcano.sh/nodegroup-name"

	BaseScore = 100
)

type nodeGroupPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	return &nodeGroupPlugin{pluginArguments: arguments}
}

func (np *nodeGroupPlugin) Name() string {
	return PluginName
}

type queueGroupAffinity struct {
	queueGroupAntiAffinityRequired  map[string]sets.Set[string]
	queueGroupAntiAffinityPreferred map[string]sets.Set[string]
	queueGroupAffinityRequired      map[string]sets.Set[string]
	queueGroupAffinityPreferred     map[string]sets.Set[string]
}

func NewQueueGroupAffinity() queueGroupAffinity {
	return queueGroupAffinity{
		queueGroupAntiAffinityRequired:  make(map[string]sets.Set[string], 0),
		queueGroupAntiAffinityPreferred: make(map[string]sets.Set[string], 0),
		queueGroupAffinityRequired:      make(map[string]sets.Set[string], 0),
		queueGroupAffinityPreferred:     make(map[string]sets.Set[string], 0),
	}
}

func (q queueGroupAffinity) predicate(queue, group string) error {
	if len(queue) == 0 {
		return nil
	}
	flag := false
	if q.queueGroupAffinityRequired != nil {
		if groups, ok := q.queueGroupAffinityRequired[queue]; ok {
			if groups.Has(group) {
				flag = true
			}
		}
	}
	if q.queueGroupAffinityPreferred != nil {
		if groups, ok := q.queueGroupAffinityPreferred[queue]; ok {
			if groups.Has(group) {
				flag = true
			}
		}
	}
	// AntiAffinity: hard constraints should be checked first
	// to make sure soft constraints satisfy
	// and antiAffinity's priority is higher than affinity
	if q.queueGroupAntiAffinityRequired != nil {
		if groups, ok := q.queueGroupAntiAffinityRequired[queue]; ok {
			if groups.Has(group) {
				flag = false
			}
		}
	}
	if q.queueGroupAntiAffinityPreferred != nil {
		if groups, ok := q.queueGroupAntiAffinityPreferred[queue]; ok {
			if groups.Has(group) {
				flag = true
			}
		}
	}
	if !flag {
		return errors.New("not satisfy")
	}
	return nil
}

func (q queueGroupAffinity) score(queue string, group string) float64 {
	nodeScore := 0.0
	if len(queue) == 0 {
		return nodeScore
	}
	// Affinity: hard constraints should be checked first
	// to make sure soft constraints can cover score.
	// And same to predict, antiAffinity's priority is higher than affinity
	if q.queueGroupAffinityRequired != nil {
		if groups, ok := q.queueGroupAffinityRequired[queue]; ok {
			if groups.Has(group) {
				nodeScore += BaseScore
			}
		}
	}
	if q.queueGroupAffinityPreferred != nil {
		if groups, ok := q.queueGroupAffinityPreferred[queue]; ok {
			if groups.Has(group) {
				nodeScore += 0.5 * BaseScore
			}
		}
	}
	if q.queueGroupAntiAffinityPreferred != nil {
		if groups, ok := q.queueGroupAntiAffinityPreferred[queue]; ok {
			if groups.Has(group) {
				nodeScore = -1
			}
		}
	}

	return nodeScore
}

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

func calculateArguments(ssn *framework.Session, args framework.Arguments) queueGroupAffinity {
	queueGroupAffinity := NewQueueGroupAffinity()
	for _, queue := range ssn.Queues {
		affinity := queue.Queue.Spec.Affinity
		if affinity == nil {
			continue
		}
		nodeGroupAffinity := affinity.NodeGroupAffinity
		if nodeGroupAffinity != nil {
			preferreds := sets.Set[string]{}
			preferreds = preferreds.Insert(nodeGroupAffinity.PreferredDuringSchedulingIgnoredDuringExecution...)
			if len(preferreds) > 0 {
				queueGroupAffinity.queueGroupAffinityPreferred[queue.Name] = preferreds
			}
			requireds := sets.Set[string]{}
			requireds = requireds.Insert(nodeGroupAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
			if len(requireds) > 0 {
				queueGroupAffinity.queueGroupAffinityRequired[queue.Name] = requireds
			}
		}
		nodeGroupAntiAffinity := affinity.NodeGroupAntiAffinity
		if nodeGroupAntiAffinity != nil {
			preferreds := sets.Set[string]{}
			preferreds = preferreds.Insert(nodeGroupAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution...)
			if len(preferreds) > 0 {
				queueGroupAffinity.queueGroupAntiAffinityPreferred[queue.Name] = preferreds
			}
			requireds := sets.Set[string]{}
			requireds = requireds.Insert(nodeGroupAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
			if len(requireds) > 0 {
				queueGroupAffinity.queueGroupAntiAffinityRequired[queue.Name] = requireds
			}
		}
	}
	return queueGroupAffinity
}

// There are 3 ways to assign pod to queue for now:
// scheduling.volcano.sh/queue-name support only annotation
// volcano.sh/queue-name support both labels & annotation
// the key should be unified, maybe volcano.sh/queue-name is better
func GetPodQueue(task *api.TaskInfo) string {
	if _, ok := task.Pod.Labels[batch.QueueNameKey]; ok {
		return task.Pod.Labels[batch.QueueNameKey]
	}
	if _, ok := task.Pod.Annotations[batch.QueueNameKey]; ok {
		return task.Pod.Annotations[batch.QueueNameKey]
	}
	if _, ok := task.Pod.Annotations[sch.QueueNameAnnotationKey]; ok {
		return task.Pod.Annotations[sch.QueueNameAnnotationKey]
	}
	return ""
}

func (np *nodeGroupPlugin) OnSessionOpen(ssn *framework.Session) {
	queueGroupAffinity := calculateArguments(ssn, np.pluginArguments)
	klog.V(4).Infof("queueGroupAffinity queueGroupAntiAffinityRequired <%v> queueGroupAntiAffinityPreferred <%v> queueGroupAffinityRequired <%v> queueGroupAffinityPreferred <%v> groupLabelName <%v>",
		queueGroupAffinity.queueGroupAntiAffinityRequired, queueGroupAffinity.queueGroupAntiAffinityPreferred,
		queueGroupAffinity.queueGroupAffinityRequired, queueGroupAffinity.queueGroupAffinityPreferred, NodeGroupNameKey)
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		group := node.Node.Labels[NodeGroupNameKey]
		queue := GetPodQueue(task)
		score := queueGroupAffinity.score(queue, group)
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %s, score %v", task.Namespace, task.Name, queue, node.Name, group, score)
		return score, nil
	}
	ssn.AddNodeOrderFn(np.Name(), nodeOrderFn)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		predicateStatus := make([]*api.Status, 0)

		group := node.Node.Labels[NodeGroupNameKey]
		queue := GetPodQueue(task)
		if err := queueGroupAffinity.predicate(queue, group); err != nil {
			nodeStatus := &api.Status{
				Code:   api.UnschedulableAndUnresolvable,
				Reason: "node not satisfy",
			}
			predicateStatus = append(predicateStatus, nodeStatus)
			return api.NewFitErrWithStatus(task, node, predicateStatus...)
		}
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %v", task.Namespace, task.Name, queue, node.Name, group)
		return nil
	}

	ssn.AddPredicateFn(np.Name(), predicateFn)
}

func (np *nodeGroupPlugin) OnSessionClose(ssn *framework.Session) {
}
