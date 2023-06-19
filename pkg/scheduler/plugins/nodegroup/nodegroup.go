/*
Copyright 2019 The Kubernetes Authors.

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
	"fmt"
	"k8s.io/klog"
	"strings"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName       = "nodegroup"
	NodeGroupNameKey = "volcano.sh/nodegroup-name"
)

type nodeGroupPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	return &nodeGroupPlugin{pluginArguments: arguments}
}

func (pp *nodeGroupPlugin) Name() string {
	return PluginName
}

type queueGroupAffinity struct {
	queueGroupAntiAffinityRequired  map[string][]string
	queueGroupAntiAffinityPreferred map[string][]string
	queueGroupAffinityRequired      map[string][]string
	queueGroupAffinityPreferred     map[string][]string
}

func NewQueueGroupAffinity() queueGroupAffinity {
	return queueGroupAffinity{
		queueGroupAntiAffinityRequired:  make(map[string][]string, 0),
		queueGroupAntiAffinityPreferred: make(map[string][]string, 0),
		queueGroupAffinityRequired:      make(map[string][]string, 0),
		queueGroupAffinityPreferred:     make(map[string][]string, 0),
	}
}
func (q queueGroupAffinity) predicate(queue, group string) error {
	if len(queue) == 0 {
		return nil
	}
	if q.queueGroupAffinityRequired != nil {
		if groups, ok := q.queueGroupAffinityRequired[queue]; ok {
			if !contains(groups, group) {
				return errors.New("NodeGroupAffinityRequired")
			}
		}
	}
	if q.queueGroupAntiAffinityRequired != nil {
		if groups, ok := q.queueGroupAntiAffinityRequired[queue]; ok {
			if contains(groups, group) {
				return errors.New("NodeGroupAntiAffinityRequired")
			}
		}
	}
	return nil
}

func (q queueGroupAffinity) score(queue string, group string) float64 {
	nodeScore := 0.0
	if len(queue) == 0 {
		return nodeScore
	}
	if q.queueGroupAffinityPreferred != nil {
		if groups, ok := q.queueGroupAffinityPreferred[queue]; ok {
			if !contains(groups, group) {
				return 1.0
			}
		}
	}
	if q.queueGroupAntiAffinityPreferred != nil {
		if groups, ok := q.queueGroupAntiAffinityPreferred[queue]; ok {
			if contains(groups, group) {
				return -1.0
			}
		}
	}
	return nodeScore
}

func contains(slice []string, item string) bool {
	if len(slice) == 0 {
		return false
	}
	for _, s := range slice {
		if strings.Compare(s, item) == 0 {
			return true
		}
	}
	return false
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
// 		enablePredicate: true
//      enableNodeOrder: true

func calculateArguments(ssn *framework.Session, args framework.Arguments) queueGroupAffinity {
	queueGroupAffinity := NewQueueGroupAffinity()
	for _, queue := range ssn.Queues {
		affinity := queue.Queue.Spec.Affinity
		if nil == affinity {
			continue
		}
		nodeGroupAffinity := affinity.NodeGroupAffinity
		if nil != nodeGroupAffinity {
			preferreds := nodeGroupAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			if len(preferreds) > 0 {
				queueGroupAffinity.queueGroupAffinityPreferred[queue.Name] = preferreds
			}
			requireds := nodeGroupAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			if len(requireds) > 0 {
				queueGroupAffinity.queueGroupAffinityRequired[queue.Name] = requireds
			}
		}
		nodeGroupAntiAffinity := affinity.NodeGroupAntiAffinity
		if nil != nodeGroupAntiAffinity {
			preferreds := nodeGroupAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			if len(preferreds) > 0 {
				queueGroupAffinity.queueGroupAntiAffinityPreferred[queue.Name] = preferreds
			}
			requireds := nodeGroupAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			if len(requireds) > 0 {
				queueGroupAffinity.queueGroupAntiAffinityRequired[queue.Name] = requireds
			}
		}
	}
	return queueGroupAffinity
}

func (np *nodeGroupPlugin) OnSessionOpen(ssn *framework.Session) {
	queueGroupAffinity := calculateArguments(ssn, np.pluginArguments)
	klog.V(4).Infof("queueGroupAffinity queueGroupAntiAffinityRequired <%v> queueGroupAntiAffinityPreferred <%v> queueGroupAffinityRequired <%v> queueGroupAffinityPreferred <%v> groupLabelName <%v>",
		queueGroupAffinity.queueGroupAntiAffinityRequired, queueGroupAffinity.queueGroupAntiAffinityPreferred,
		queueGroupAffinity.queueGroupAffinityRequired, queueGroupAffinity.queueGroupAffinityPreferred, NodeGroupNameKey)
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		group := node.Node.Labels[NodeGroupNameKey]
		queue := task.Pod.Labels[batch.QueueNameKey]
		score := queueGroupAffinity.score(queue, group)
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %s, score %v", task.Namespace, task.Name, queue, node.Name, group, score)
		return score, nil
	}
	ssn.AddNodeOrderFn(np.Name(), nodeOrderFn)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		group := node.Node.Labels[NodeGroupNameKey]
		queue := task.Pod.Labels[batch.QueueNameKey]
		if err := queueGroupAffinity.predicate(queue, group); err != nil {
			return fmt.Errorf("<%s> predicates Task <%s/%s> on Node <%s> of nodegroup <%v> failed <%v>", np.Name(), task.Namespace, task.Name, node.Name, group, err)
		}
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %v", task.Namespace, task.Name, queue, node.Name, group)
		return nil
	}

	ssn.AddPredicateFn(np.Name(), predicateFn)
}

func (np *nodeGroupPlugin) OnSessionClose(ssn *framework.Session) {
}
