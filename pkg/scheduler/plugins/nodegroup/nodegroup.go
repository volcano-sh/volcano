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
	PluginName = "nodegroup"

	GroupAntiAffinityRequired  = "groupantiaffinity.required"
	GroupAntiAffinityPreferred = "groupantiaffinity.preferred"
	GroupAffinityRequired      = "groupaffinity.required"
	GroupAffinityPreferred     = "groupaffinity.preferred"
	GroupLabelName             = "group.label.name"
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
	groupLabelName                  string
}

func (q queueGroupAffinity) predicate(queue, group string) error {
	if len(queue) == 0 {
		return nil
	}
	if q.queueGroupAffinityRequired != nil {
		if groups, ok := q.queueGroupAffinityRequired[queue]; ok {
			if !contains(groups, group) {
				return errors.New("GroupAffinityRequired")
			}
		}
	}
	if q.queueGroupAntiAffinityRequired != nil {
		if groups, ok := q.queueGroupAntiAffinityRequired[queue]; ok {
			if contains(groups, group) {
				return errors.New("GroupAntiAffinityRequired")
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
//      arguments:
//        groupantiaffinity.required: q1 = g1,g2; q2=g2
//        groupantiaffinity.preferred:
//        groupaffinity.required:
//        groupaffinity.preferred:
//        group.label.name = l1
// 		enablePredicate: true
//      enableNodeOrder: true

func calculateArguments(args framework.Arguments) queueGroupAffinity {

	queueGroupAffinity := queueGroupAffinity{}
	var groupLabelName string
	args.GetString(&groupLabelName, GroupLabelName)
	queueGroupAffinity.groupLabelName = groupLabelName
	var queueGroupAntiAffinityRequired string
	args.GetString(&queueGroupAntiAffinityRequired, GroupAntiAffinityRequired)
	queueGroupAffinity.queueGroupAntiAffinityRequired = CalculateQueueGroupAffinity(queueGroupAntiAffinityRequired)
	var queueGroupAntiAffinityPreferred string
	args.GetString(&queueGroupAntiAffinityPreferred, GroupAntiAffinityPreferred)
	queueGroupAffinity.queueGroupAntiAffinityPreferred = CalculateQueueGroupAffinity(queueGroupAntiAffinityPreferred)
	var queueGroupAffinityRequired string
	args.GetString(&queueGroupAffinityRequired, GroupAffinityRequired)
	queueGroupAffinity.queueGroupAffinityRequired = CalculateQueueGroupAffinity(queueGroupAffinityRequired)
	var queueGroupAffinityPreferred string
	args.GetString(&queueGroupAffinityPreferred, GroupAffinityPreferred)
	queueGroupAffinity.queueGroupAffinityPreferred = CalculateQueueGroupAffinity(queueGroupAffinityPreferred)

	return queueGroupAffinity
}

func CalculateQueueGroupAffinity(queueGroupAffinity string) map[string][]string {
	if len(queueGroupAffinity) == 0 {
		return nil
	}
	queueMap := make(map[string][]string)
	expressions := strings.Split(queueGroupAffinity, ";")
	for _, expression := range expressions {
		expression = strings.TrimSpace(expression)
		equalOperatorIndex := strings.Index(expression, "=")
		queueName := strings.TrimSpace(expression[:equalOperatorIndex])
		if len(queueName) == 0 {
			continue
		}
		groupNames := strings.TrimSpace(expression[equalOperatorIndex+1:])
		if len(groupNames) == 0 {
			continue
		}
		groupSlice := make([]string, 0)
		groups := strings.Split(groupNames, ",")
		for _, group := range groups {
			groupSlice = append(groupSlice, strings.TrimSpace(group))
		}
		queueMap[queueName] = groupSlice
	}
	return queueMap
}

func (pp *nodeGroupPlugin) OnSessionOpen(ssn *framework.Session) {
	queueGroupAffinity := calculateArguments(pp.pluginArguments)
	klog.V(4).Infof("queueGroupAffinity queueGroupAntiAffinityRequired <%v> queueGroupAntiAffinityPreferred <%v> queueGroupAffinityRequired <%v> queueGroupAffinityPreferred <%v> groupLabelName <%v>",
		queueGroupAffinity.queueGroupAntiAffinityRequired, queueGroupAffinity.queueGroupAntiAffinityPreferred,
		queueGroupAffinity.queueGroupAffinityRequired, queueGroupAffinity.queueGroupAffinityPreferred, queueGroupAffinity.groupLabelName)
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		group := node.Node.Labels[queueGroupAffinity.groupLabelName]
		queue := task.Pod.Labels[batch.QueueNameKey]
		score := queueGroupAffinity.score(queue, group)
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %s, score %v", task.Namespace, task.Name, queue, node.Name, group, score)
		return score, nil
	}
	ssn.AddNodeOrderFn(pp.Name(), nodeOrderFn)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		group := node.Node.Labels[queueGroupAffinity.groupLabelName]
		queue := task.Pod.Labels[batch.QueueNameKey]
		klog.V(4).Infof("task <%s>/<%s> queue %s on node %s of nodegroup %v", task.Namespace, task.Name, queue, node.Name, group)
		if err := queueGroupAffinity.predicate(queue, group); err != nil {
			return fmt.Errorf("<%s> predicates Task <%s/%s> on Node <%s> failed <%v>", pp.Name(), task.Namespace, task.Name, node.Name, err)
		}
		return nil
	}

	ssn.AddPredicateFn(pp.Name(), predicateFn)
}

func (pp *nodeGroupPlugin) OnSessionClose(ssn *framework.Session) {
}
