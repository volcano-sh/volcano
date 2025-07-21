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

package api

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// NodePodNumberExceeded means pods in node exceed the allocatable pod number
	NodePodNumberExceeded = "node(s) pod number exceeded"
	// NodeResourceFitFailed means node could not fit the request of pod
	NodeResourceFitFailed = "node(s) resource fit failed"

	// AllNodeUnavailableMsg is the default error message
	AllNodeUnavailableMsg = "all nodes are unavailable"
)

// These are reasons for a pod's transition to a condition.
const (
	// PodReasonUnschedulable reason in PodScheduled PodCondition means that the scheduler
	// can't schedule the pod right now, for example due to insufficient resources in the cluster.
	// It can also mean that the scheduler skips scheduling the pod which left the pod `Undetermined`,
	// for example due to unschedulable pod already occurred.
	PodReasonUnschedulable = "Unschedulable"
	// PodReasonSchedulable reason in PodScheduled PodCondition means that the scheduler
	// can schedule the pod right now, but not bind yet
	PodReasonSchedulable = "Schedulable"
	// PodReasonSchedulerError reason in PodScheduled PodCondition means that the scheduler
	// tried to schedule the pod, but went error when scheduling
	// for example bind pod return error.
	PodReasonSchedulerError = "SchedulerError"
)

// FitErrors is set of FitError on many nodes
type FitErrors struct {
	nodes map[string]*FitError
	err   string
}

// NewFitErrors returns an FitErrors
func NewFitErrors() *FitErrors {
	f := new(FitErrors)
	f.nodes = make(map[string]*FitError)
	return f
}

// SetError set the common error message in FitErrors
func (f *FitErrors) SetError(err string) {
	f.err = err
}

// SetNodeError set the node error in FitErrors
func (f *FitErrors) SetNodeError(nodeName string, err error) {
	var fe *FitError
	switch obj := err.(type) {
	case *FitError:
		obj.NodeName = nodeName
		fe = obj
	default:
		fe = &FitError{
			NodeName: nodeName,
			Status:   []*Status{{Code: Error, Reason: obj.Error()}},
		}
	}

	f.nodes[nodeName] = fe
}

// GetUnschedulableAndUnresolvableNodes returns the set of nodes that has no help from preempting pods from it
func (f *FitErrors) GetUnschedulableAndUnresolvableNodes() map[string]sets.Empty {
	ret := make(map[string]sets.Empty)
	for _, node := range f.nodes {
		if node.Status.ContainsUnschedulableAndUnresolvable() {
			ret[node.NodeName] = sets.Empty{}
		}
	}
	return ret
}

// Error returns the final error message
func (f *FitErrors) Error() string {
	if f.err == "" {
		f.err = fmt.Sprintf("0/%v", len(f.nodes)) + " nodes are unavailable"
	}
	if len(f.nodes) == 0 {
		return f.err
	}

	reasons := make(map[string]int)
	for _, node := range f.nodes {
		for _, reason := range node.Reasons() {
			reasons[reason]++
		}
	}

	sortReasonsHistogram := func() []string {
		reasonStrings := []string{}
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	reasonMsg := fmt.Sprintf(f.err+": %v.", strings.Join(sortReasonsHistogram(), ", "))
	return reasonMsg
}

// FitError describe the reason why task could not fit that node
type FitError struct {
	taskNamespace string
	taskName      string
	NodeName      string
	Status        StatusSets
}

// NewFitError return FitError by message, setting default code to Error
func NewFitError(task *TaskInfo, node *NodeInfo, message ...string) *FitError {
	fe := &FitError{
		taskName:      task.Name,
		taskNamespace: task.Namespace,
		NodeName:      node.Name,
	}
	sts := make([]*Status, 0, len(message))
	for _, msg := range message {
		sts = append(sts, &Status{Reason: msg, Code: Error})
	}
	fe.Status = StatusSets(sts)
	return fe
}

// NewFitErrWithStatus returns a fit error with code and reason in it
func NewFitErrWithStatus(task *TaskInfo, node *NodeInfo, sts ...*Status) *FitError {
	fe := &FitError{
		taskName:      task.Name,
		taskNamespace: task.Namespace,
		NodeName:      node.Name,
		Status:        sts,
	}
	return fe
}

// Reasons returns the reasons
func (fe *FitError) Reasons() []string {
	if fe == nil {
		return []string{}
	}
	return fe.Status.Reasons()
}

// Error returns the final error message
func (f *FitError) Error() string {
	return fmt.Sprintf("task %s/%s on node %s fit failed: %s", f.taskNamespace, f.taskName, f.NodeName, strings.Join(f.Reasons(), ", "))
}

// WrapInsufficientResourceReason wrap insufficient resource reason.
func WrapInsufficientResourceReason(resources []string) string {
	if len(resources) == 0 {
		return ""
	}
	return "Insufficient " + resources[0]
}
