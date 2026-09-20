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

	// HyperNodeFitSummaryPrefix labels HyperNode-tier scheduling diagnostics.
	HyperNodeFitSummaryPrefix = "HyperNode"
	// NodeFitSummaryPrefix labels node-level predicate diagnostics.
	NodeFitSummaryPrefix = "Node"

	// maxSchedulingDimensions is the upper bound of HyperNode + Node fit summaries joined by FormatSchedulingDimensions.
	maxSchedulingDimensions = 2
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

// FormatSchedulingDimensions joins HyperNode-tier and node-level diagnostics.
func FormatSchedulingDimensions(hyperNodeSummary, nodeSummary string) string {
	parts := make([]string, 0, maxSchedulingDimensions)
	if hyperNodeSummary != "" {
		parts = append(parts, fmt.Sprintf("%s: %s", HyperNodeFitSummaryPrefix, hyperNodeSummary))
	}
	if nodeSummary != "" {
		parts = append(parts, fmt.Sprintf("%s: %s", NodeFitSummaryPrefix, nodeSummary))
	}
	return strings.Join(parts, "; ")
}

// AppendSchedulingDimensions appends HyperNode-tier and node-level diagnostics to base.
func AppendSchedulingDimensions(base, hyperNodeSummary, nodeSummary string) string {
	dimensions := FormatSchedulingDimensions(hyperNodeSummary, nodeSummary)
	if base == "" {
		return dimensions
	}
	if dimensions == "" {
		return base
	}
	return base + "; " + dimensions
}

// HyperNodeGradientStats carries per-plugin and intersected tier counts from gradient planning.
type HyperNodeGradientStats struct {
	PluginEligibleByTier map[string]map[int]int
	IntersectedByTier    map[int]int
	// ExcludedByReason maps excluded HyperNode name to plugin exclusion labels (comma-separated).
	ExcludedByReason map[string]string
}

// HyperNodePluginGradient carries one plugin's HyperNode gradient result for intersection.
type HyperNodePluginGradient struct {
	PluginName string
	Gradients  [][]*HyperNodeInfo
}

// HyperNodeMinResourceFilterStats captures minResource filtering on intersected HyperNodes.
type HyperNodeMinResourceFilterStats struct {
	FinalByTier      map[int]int
	ExcludedByTier   map[int]int
	ExcludedByReason map[string]string
}

var hyperNodeExclusionLabels = map[string]string{
	"group-topology-affinity": "podGroupAntiAffinity",
	"network-topology-aware":  "networkTopology",
}

const hyperNodeMinResourceExclusionLabel = "minResource"

// HyperNodeNamesInGradients flattens gradient layers into a set of HyperNode names.
func HyperNodeNamesInGradients(gradients [][]*HyperNodeInfo) sets.Set[string] {
	names := sets.New[string]()
	for _, layer := range gradients {
		for _, hn := range layer {
			names.Insert(hn.Name)
		}
	}
	return names
}

// ComputePluginExcludedHyperNodes returns HyperNodes present in any plugin gradient but removed by intersection.
func ComputePluginExcludedHyperNodes(
	gradientByPlugin []HyperNodePluginGradient,
	eligible sets.Set[string],
) map[string]string {
	if len(gradientByPlugin) <= 1 {
		return nil
	}

	pluginSets := make(map[string]sets.Set[string], len(gradientByPlugin))
	union := sets.New[string]()
	for _, pluginGradient := range gradientByPlugin {
		names := HyperNodeNamesInGradients(pluginGradient.Gradients)
		pluginSets[pluginGradient.PluginName] = names
		union = union.Union(names)
	}

	excluded := make(map[string]string)
	for name := range union {
		if eligible.Has(name) {
			continue
		}
		reasons := make([]string, 0, len(gradientByPlugin))
		for _, pluginGradient := range gradientByPlugin {
			if !pluginSets[pluginGradient.PluginName].Has(name) {
				label := hyperNodeExclusionLabels[pluginGradient.PluginName]
				if label == "" {
					label = pluginGradient.PluginName
				}
				reasons = append(reasons, label)
			}
		}
		sort.Strings(reasons)
		excluded[name] = strings.Join(reasons, ", ")
	}
	return excluded
}

// FormatHyperNodeFitSummary builds the HyperNode-tier portion of JobFitErrors.
func FormatHyperNodeFitSummary(
	stats *HyperNodeGradientStats,
	resourceStats *HyperNodeMinResourceFilterStats,
	minResource *Resource,
	hyperNodesSetByTier map[int]sets.Set[string],
	tierNameMap HyperNodeTierNameMap,
	hyperNodes HyperNodeInfoMap,
) string {
	if stats == nil {
		return ""
	}

	finalByTier := stats.IntersectedByTier
	if resourceStats != nil {
		finalByTier = resourceStats.FinalByTier
	}

	totalByTier := make(map[int]int, len(hyperNodesSetByTier))
	for tier, names := range hyperNodesSetByTier {
		if count := names.Len(); count > 0 {
			totalByTier[tier] = count
		}
	}
	if len(totalByTier) == 0 {
		for tier, count := range stats.IntersectedByTier {
			if count > totalByTier[tier] {
				totalByTier[tier] = count
			}
		}
	}

	tiers := sortedHyperNodeTiersWithTotal(totalByTier)
	if len(tiers) == 0 {
		return ""
	}

	totalAll, finalAll := 0, 0
	tierParts := make([]string, 0, len(tiers))
	hasResourceExclusion := false

	for _, tier := range tiers {
		total := totalByTier[tier]
		final := finalByTier[tier]
		totalAll += total
		finalAll += final

		exclusions := hyperNodeTierExclusions(stats.PluginEligibleByTier, resourceStats, tier, total)
		if resourceStats != nil && resourceStats.ExcludedByTier[tier] > 0 {
			hasResourceExclusion = true
		}

		tierName := tierNameMap.NameForTier(tier, hyperNodes)
		part := fmt.Sprintf("%s %d/%d", tierName, final, total)
		if len(exclusions) > 0 {
			part += fmt.Sprintf(" (%s)", strings.Join(exclusions, ", "))
		}
		tierParts = append(tierParts, part)
	}

	message := fmt.Sprintf("%d/%d hyperNodes available", finalAll, totalAll)
	if minResource != nil && hasResourceExclusion {
		message += fmt.Sprintf(" (minResource: %s)", minResource.String())
	}
	if len(tierParts) > 0 {
		message += ": " + strings.Join(tierParts, "; ")
	}
	return message
}

// MergeHyperNodeFitSummary layers a subJob-specific summary on the job-level baseline
// instead of replacing it.
func MergeHyperNodeFitSummary(baseline, subJobScope, subSummary string) string {
	if subSummary == "" {
		return baseline
	}
	scoped := subSummary
	if subJobScope != "" {
		scoped = fmt.Sprintf("subJob %s: %s", subJobScope, subSummary)
	}
	if baseline == "" {
		return scoped
	}
	return baseline + "; " + scoped
}

func hyperNodeTierExclusions(
	pluginEligibleByTier map[string]map[int]int,
	resourceStats *HyperNodeMinResourceFilterStats,
	tier int,
	total int,
) []string {
	pluginNames := make([]string, 0, len(pluginEligibleByTier))
	for name := range pluginEligibleByTier {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)

	exclusions := make([]string, 0, len(pluginNames)+1)
	for _, pluginName := range pluginNames {
		eligible := 0
		if pluginEligibleByTier[pluginName] != nil {
			eligible = pluginEligibleByTier[pluginName][tier]
		}
		if excluded := total - eligible; excluded > 0 {
			label := hyperNodeExclusionLabels[pluginName]
			if label == "" {
				label = pluginName
			}
			exclusions = append(exclusions, fmt.Sprintf("%d %s", excluded, label))
		}
	}
	if resourceStats != nil {
		if excluded := resourceStats.ExcludedByTier[tier]; excluded > 0 {
			exclusions = append(exclusions, fmt.Sprintf("%d %s", excluded, hyperNodeMinResourceExclusionLabel))
		}
	}
	return exclusions
}

func sortedHyperNodeTiersWithTotal(totalByTier map[int]int) []int {
	tiers := make([]int, 0, len(totalByTier))
	for tier, total := range totalByTier {
		if total > 0 {
			tiers = append(tiers, tier)
		}
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i] > tiers[j] })
	return tiers
}

// FitErrors is set of FitError on many nodes
type FitErrors struct {
	nodes     map[string]*FitError
	hyperNode string
	err       string
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

// SetHyperNode set the hyperNode name in FitErrors
func (f *FitErrors) SetHyperNode(hyperNode string) {
	f.hyperNode = hyperNode
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

// UnschedulablePlugins returns the set of plugin names that rejected the task
// with an Unschedulable or UnschedulableAndUnresolvable status on any node.
func (f *FitErrors) UnschedulablePlugins() sets.Set[string] {
	plugins := sets.New[string]()
	for _, node := range f.nodes {
		for _, status := range node.Status {
			if status == nil || status.Plugin == "" {
				continue
			}
			if status.Code == Unschedulable || status.Code == UnschedulableAndUnresolvable {
				plugins.Insert(status.Plugin)
			}
		}
	}
	return plugins
}

// NodePluginInsufficientResources returns, for every node carrying an
// Unschedulable or UnschedulableAndUnresolvable status for the given plugin,
// the union of that status's structured InsufficientResources dimensions.
//
// A rejected node whose matching status(es) carry no structured dimensions is
// still present in the returned map with a nil slice, so callers can
// distinguish "rejected by this plugin with no structured detail recorded"
// (which must fail open rather than silently drop a real blocker) from "not
// rejected by this plugin" (absent from the map).
func (f *FitErrors) NodePluginInsufficientResources(plugin string) map[string][]string {
	result := make(map[string][]string)
	for _, node := range f.nodes {
		for _, status := range node.Status {
			if status == nil || status.Plugin != plugin {
				continue
			}
			if status.Code != Unschedulable && status.Code != UnschedulableAndUnresolvable {
				continue
			}
			if _, ok := result[node.NodeName]; !ok {
				result[node.NodeName] = nil
			}
			result[node.NodeName] = append(result[node.NodeName], status.InsufficientResources...)
		}
	}
	return result
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
	if f.hyperNode != "" {
		reasonMsg = fmt.Sprintf("In hyperNode %s: %s", f.hyperNode, reasonMsg)
	}
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
