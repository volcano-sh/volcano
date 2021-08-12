package api

import (
	"fmt"
	"sort"
	"strings"
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
	PodReasonUnschedulable = "Unschedulable"
	// PodReasonSchedulable reason in PodScheduled PodCondition means that the scheduler
	// can schedule the pod right now, but not bind yet
	PodReasonSchedulable = "Schedulable"
	// PodReasonUndetermined reason in PodScheduled PodCondition means that the scheduler
	// skips scheduling the pod which left the pod `Undetermined`, for example due to unschedulable pod already occurred.
	PodReasonUndetermined = "Undetermined"
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
			Reasons:  []string{obj.Error()},
		}
	}

	f.nodes[nodeName] = fe
}

// Error returns the final error message
func (f *FitErrors) Error() string {
	reasons := make(map[string]int)

	for _, node := range f.nodes {
		for _, reason := range node.Reasons {
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
	if f.err == "" {
		f.err = AllNodeUnavailableMsg
	}
	reasonMsg := fmt.Sprintf(f.err+": %v.", strings.Join(sortReasonsHistogram(), ", "))
	return reasonMsg
}

// FitError describe the reason why task could not fit that node
type FitError struct {
	taskNamespace string
	taskName      string
	NodeName      string
	Reasons       []string
}

// NewFitError return FitError by message
func NewFitError(task *TaskInfo, node *NodeInfo, message ...string) *FitError {
	fe := &FitError{
		taskName:      task.Name,
		taskNamespace: task.Namespace,
		NodeName:      node.Name,
		Reasons:       message,
	}
	return fe
}

// Error returns the final error message
func (f *FitError) Error() string {
	return fmt.Sprintf("task %s/%s on node %s fit failed: %s", f.taskNamespace, f.taskName, f.NodeName, strings.Join(f.Reasons, ", "))
}
