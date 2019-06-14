package api

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/kubernetes/pkg/scheduler/algorithm"
)

const (
	NodePodNumberExceeded = "node(s) pod number exceeded"
	NodeResourceFitFailed = "node(s) resource fit failed"

	AllNodeUnavailableMsg = "all nodes are unavailable"
)

type FitErrors struct {
	nodes map[string]*FitError
	err   string
}

func NewFitErrors() *FitErrors {
	f := new(FitErrors)
	f.nodes = make(map[string]*FitError)
	return f
}

func (f *FitErrors) SetError(err string) {
	f.err = err
}

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

type FitError struct {
	taskNamespace string
	taskName      string
	NodeName      string
	Reasons       []string
}

func NewFitError(task *TaskInfo, node *NodeInfo, message ...string) *FitError {
	fe := &FitError{
		taskName:      task.Name,
		taskNamespace: task.Namespace,
		NodeName:      node.Name,
		Reasons:       message,
	}
	return fe
}

func NewFitErrorByReasons(task *TaskInfo, node *NodeInfo, reasons ...algorithm.PredicateFailureReason) *FitError {
	message := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		message = append(message, reason.GetReason())
	}
	return NewFitError(task, node, message...)
}

func (f *FitError) Error() string {
	return fmt.Sprintf("task %s/%s on node %s fit failed: %s", f.taskNamespace, f.taskName, f.NodeName, strings.Join(f.Reasons, ", "))
}
