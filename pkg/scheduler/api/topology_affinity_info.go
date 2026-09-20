/*
Copyright 2017 The Kubernetes Authors.
Copyright 2017-2025 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
)

// ContainsHardPodGroupAntiAffinity returns whether the job has hard cross-PodGroup anti-affinity.
func (ji *JobInfo) ContainsHardPodGroupAntiAffinity() bool {
	if ji.PodGroup == nil || ji.PodGroup.Spec.TopologyAffinity == nil {
		return false
	}
	anti := ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity
	return anti != nil && len(anti.Required) > 0
}

// HasPreferredPodGroupAntiAffinity returns whether the job has soft cross-PodGroup anti-affinity.
func (ji *JobInfo) HasPreferredPodGroupAntiAffinity() bool {
	if ji.PodGroup == nil || ji.PodGroup.Spec.TopologyAffinity == nil {
		return false
	}
	anti := ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity
	return anti != nil && len(anti.Preferred) > 0
}

// RequiredPodGroupAntiAffinityTerms returns hard cross-PodGroup anti-affinity terms.
func (ji *JobInfo) RequiredPodGroupAntiAffinityTerms() []scheduling.PodGroupAffinityTerm {
	if !ji.ContainsHardPodGroupAntiAffinity() {
		return nil
	}
	return ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity.Required
}

// PreferredPodGroupAntiAffinityTerms returns soft cross-PodGroup anti-affinity terms.
func (ji *JobInfo) PreferredPodGroupAntiAffinityTerms() []scheduling.PodGroupAffinityTerm {
	if ji.PodGroup == nil || ji.PodGroup.Spec.TopologyAffinity == nil ||
		ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity == nil {
		return nil
	}
	return ji.PodGroup.Spec.TopologyAffinity.PodGroupAntiAffinity.Preferred
}

// WithTopologyAffinity returns whether the job declares topologyAffinity.
func (ji *JobInfo) WithTopologyAffinity() bool {
	return ji.PodGroup != nil && ji.PodGroup.Spec.TopologyAffinity != nil
}

// ResolvePodGroupTermTier resolves topologyTier or topologyTierName on a PodGroupAffinityTerm.
func ResolvePodGroupTermTier(term scheduling.PodGroupAffinityTerm, tierNameMap HyperNodeTierNameMap) (int, error) {
	if term.TopologyTier != nil && term.TopologyTierName != "" {
		return 0, fmt.Errorf("topologyTier and topologyTierName are mutually exclusive")
	}
	if term.TopologyTier != nil {
		return int(*term.TopologyTier), nil
	}
	if term.TopologyTierName != "" {
		tier, ok := tierNameMap[term.TopologyTierName]
		if !ok {
			return 0, fmt.Errorf("unknown topologyTierName %q", term.TopologyTierName)
		}
		return tier, nil
	}
	return 0, fmt.Errorf("topologyTier or topologyTierName must be set")
}

// PodGroupMatchesTerm reports whether otherJob is selected by term's podGroupSelector (excluding selfJob).
func PodGroupMatchesTerm(term scheduling.PodGroupAffinityTerm, selfJob, otherJob *JobInfo) bool {
	matched, _ := PodGroupMatchesTermWithNamespaceLister(term, selfJob, otherJob, nil)
	return matched
}

// PodGroupMatchesTermWithNamespaceLister reports whether otherJob is selected by
// the PodGroup and namespace selectors. A nil namespaceSelector means the
// current PodGroup namespace; a non-nil selector is evaluated against Namespace labels.
func PodGroupMatchesTermWithNamespaceLister(
	term scheduling.PodGroupAffinityTerm,
	selfJob, otherJob *JobInfo,
	namespaceLister corelisters.NamespaceLister,
) (bool, error) {
	if otherJob == nil || otherJob.PodGroup == nil || selfJob == nil || selfJob.UID == otherJob.UID {
		return false, nil
	}
	matchedNamespace, err := matchesNamespaceSelector(term.NamespaceSelector, selfJob.Namespace, otherJob.Namespace, namespaceLister)
	if err != nil || !matchedNamespace {
		return false, err
	}
	if term.PodGroupSelector == nil {
		return false, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(term.PodGroupSelector)
	if err != nil {
		return false, err
	}
	return selector.Matches(labels.Set(otherJob.PodGroup.Labels)), nil
}

func matchesNamespaceSelector(
	namespaceSelector *metav1.LabelSelector,
	selfNamespace, otherNamespace string,
	namespaceLister corelisters.NamespaceLister,
) (bool, error) {
	if namespaceSelector == nil {
		return selfNamespace == otherNamespace, nil
	}
	if namespaceLister == nil {
		// Preserve the historical helper behavior for callers that do not have an
		// informer. Scheduler plugins always pass the Namespace lister below.
		return selfNamespace == otherNamespace, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(namespaceSelector)
	if err != nil {
		return false, err
	}
	namespace, err := namespaceLister.Get(otherNamespace)
	if err != nil {
		return false, err
	}
	return selector.Matches(labels.Set(namespace.Labels)), nil
}

// ComputeSubJobAllocatedHyperNode returns the HyperNode that contains all allocated tasks in subJob.
func ComputeSubJobAllocatedHyperNode(
	subJob *SubJobInfo,
	hyperNodes HyperNodeInfoMap,
	nodesByHyperNode map[string]sets.Set[string],
) string {
	if subJob == nil || len(hyperNodes) == 0 || len(nodesByHyperNode) == 0 {
		return ""
	}
	hyperNodeSet := sets.New[string]()
	for name := range hyperNodes {
		hyperNodeSet.Insert(name)
	}
	return getSubJobAllocatedHyperNodeFromTasks(subJob, hyperNodeSet, nodesByHyperNode, hyperNodes)
}

// ComputeJobAllocatedHyperNode returns the HyperNode that contains all allocated tasks in job.
func ComputeJobAllocatedHyperNode(
	job *JobInfo,
	hyperNodes HyperNodeInfoMap,
	nodesByHyperNode map[string]sets.Set[string],
) string {
	if job == nil || len(hyperNodes) == 0 || len(nodesByHyperNode) == 0 {
		return ""
	}
	hyperNodeSet := sets.New[string]()
	for name := range hyperNodes {
		hyperNodeSet.Insert(name)
	}

	var lca string
	for _, subJob := range job.SubJobs {
		subJobHyperNode := ComputeSubJobAllocatedHyperNode(subJob, hyperNodes, nodesByHyperNode)
		if subJobHyperNode == "" {
			continue
		}
		lca = hyperNodes.GetLCAHyperNode(lca, subJobHyperNode)
	}
	if lca != "" {
		return lca
	}
	return getAllocatedHyperNodeFromTasks(collectJobAllocatedTasks(job), hyperNodeSet, nodesByHyperNode, hyperNodes)
}

// SyncJobAllocatedHyperNode refreshes job and subJob AllocatedHyperNode from remaining allocated tasks.
// Call this when task placement changes (for example pod deletion) so placement tracks running pods.
func SyncJobAllocatedHyperNode(
	job *JobInfo,
	hyperNodes HyperNodeInfoMap,
	nodesByHyperNode map[string]sets.Set[string],
) {
	if job == nil {
		return
	}

	for _, subJob := range job.SubJobs {
		if len(collectSubJobAllocatedTasks(subJob)) == 0 {
			subJob.AllocatedHyperNode = ""
			continue
		}
		if len(hyperNodes) == 0 || len(nodesByHyperNode) == 0 {
			continue
		}
		subJob.AllocatedHyperNode = ComputeSubJobAllocatedHyperNode(subJob, hyperNodes, nodesByHyperNode)
	}

	if !jobHasAllocatedTasks(job) {
		job.AllocatedHyperNode = ""
		return
	}
	if len(hyperNodes) == 0 || len(nodesByHyperNode) == 0 {
		return
	}
	job.AllocatedHyperNode = ComputeJobAllocatedHyperNode(job, hyperNodes, nodesByHyperNode)
}

func jobHasAllocatedTasks(job *JobInfo) bool {
	return len(collectJobAllocatedTasks(job)) > 0
}

// HasTopologyDomainTasks reports whether the job has tasks that currently
// occupy a topology domain, including pipelined tasks with an assigned node.
func (ji *JobInfo) HasTopologyDomainTasks() bool {
	return len(collectJobAllocatedTasks(ji)) > 0
}

// HasTopologyDomainTasks reports whether the subJob has tasks that currently
// occupy a topology domain, including pipelined tasks with an assigned node.
func (sji *SubJobInfo) HasTopologyDomainTasks() bool {
	return len(collectSubJobAllocatedTasks(sji)) > 0
}

// getJobAllocatedHyperNode returns job.AllocatedHyperNode when set, otherwise infers it from
// placed tasks (for example matching PodGroups without network topology).
func getJobAllocatedHyperNode(
	job *JobInfo,
	hyperNodes HyperNodeInfoMap,
	nodesByHyperNode map[string]sets.Set[string],
) string {
	if job == nil || len(hyperNodes) == 0 {
		return ""
	}
	if job.AllocatedHyperNode != "" {
		return job.AllocatedHyperNode
	}
	if len(nodesByHyperNode) == 0 {
		return ""
	}

	hyperNodeSet := sets.New[string]()
	for name := range hyperNodes {
		hyperNodeSet.Insert(name)
	}

	var lca string
	for _, subJob := range job.SubJobs {
		subJobHyperNode := subJob.AllocatedHyperNode
		if subJobHyperNode == "" {
			subJobHyperNode = getSubJobAllocatedHyperNodeFromTasks(subJob, hyperNodeSet, nodesByHyperNode, hyperNodes)
		}
		if subJobHyperNode == "" {
			continue
		}
		lca = hyperNodes.GetLCAHyperNode(lca, subJobHyperNode)
	}
	if lca != "" {
		return lca
	}

	return getAllocatedHyperNodeFromTasks(collectJobAllocatedTasks(job), hyperNodeSet, nodesByHyperNode, hyperNodes)
}

func collectJobAllocatedTasks(job *JobInfo) []*TaskInfo {
	tasks := make([]*TaskInfo, 0)
	for _, subJob := range job.SubJobs {
		tasks = append(tasks, collectSubJobAllocatedTasks(subJob)...)
	}
	if len(tasks) > 0 {
		return tasks
	}
	for status, taskMap := range job.TaskStatusIndex {
		if !occupiesTopologyDomain(status, nil) {
			continue
		}
		for _, task := range taskMap {
			if !occupiesTopologyDomain(status, task) {
				continue
			}
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func collectSubJobAllocatedTasks(subJob *SubJobInfo) []*TaskInfo {
	if subJob == nil {
		return nil
	}
	tasks := make([]*TaskInfo, 0, subJob.AllocatedTaskNum())
	for status, taskMap := range subJob.TaskStatusIndex {
		if !occupiesTopologyDomain(status, nil) {
			continue
		}
		for _, task := range taskMap {
			if !occupiesTopologyDomain(status, task) {
				continue
			}
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func occupiesTopologyDomain(status TaskStatus, task *TaskInfo) bool {
	if AllocatedStatus(status) {
		return true
	}
	if status != Pipelined {
		return false
	}
	// With a nil task this is the cheap status-level precheck used by callers.
	return task == nil || task.NodeName != ""
}

func getSubJobAllocatedHyperNodeFromTasks(
	subJob *SubJobInfo,
	hyperNodeSet sets.Set[string],
	nodesByHyperNode map[string]sets.Set[string],
	hyperNodes HyperNodeInfoMap,
) string {
	return getAllocatedHyperNodeFromTasks(
		collectSubJobAllocatedTasks(subJob), hyperNodeSet, nodesByHyperNode, hyperNodes,
	)
}

func getAllocatedHyperNodeFromTasks(
	tasks []*TaskInfo,
	hyperNodeSet sets.Set[string],
	nodesByHyperNode map[string]sets.Set[string],
	hyperNodes HyperNodeInfoMap,
) string {
	if len(tasks) == 0 {
		return ""
	}

	var candidateHyperNodes sets.Set[string]
	for _, task := range tasks {
		if task.NodeName == "" {
			continue
		}

		search := hyperNodeSet
		if candidateHyperNodes != nil {
			search = candidateHyperNodes
		}

		taskHyperNodes := sets.New[string]()
		for hyperNode := range search {
			if nodes, found := nodesByHyperNode[hyperNode]; found && nodes.Has(task.NodeName) {
				taskHyperNodes.Insert(hyperNode)
			}
		}
		if taskHyperNodes.Len() == 0 {
			return ""
		}
		candidateHyperNodes = taskHyperNodes
	}
	return getLowestTierHyperNode(candidateHyperNodes, hyperNodes)
}

func getLowestTierHyperNode(hyperNodeNames sets.Set[string], hyperNodes HyperNodeInfoMap) string {
	if hyperNodeNames == nil || hyperNodeNames.Len() == 0 {
		return ""
	}

	var lowest *HyperNodeInfo
	for name := range hyperNodeNames {
		hyperNode, found := hyperNodes[name]
		if !found {
			continue
		}
		if lowest == nil || hyperNode.Tier() < lowest.Tier() {
			lowest = hyperNode
		}
	}
	if lowest == nil {
		return ""
	}
	return lowest.Name
}

// CollectJobOccupiedHyperNodesAtTier returns HyperNode names at tier where matching
// job tasks are placed. Each allocated task contributes its ancestor at tier; when a job
// spans multiple sibling domains (for example hn-A and hn-B), all occupied domains are
// returned instead of expanding an LCA to every sibling (which would incorrectly block hn-C).
func CollectJobOccupiedHyperNodesAtTier(
	job *JobInfo,
	hyperNodes HyperNodeInfoMap,
	tier int,
	nodesByHyperNode map[string]sets.Set[string],
) sets.Set[string] {
	occupied := sets.New[string]()
	if job == nil {
		return occupied
	}
	for _, task := range collectJobAllocatedTasks(job) {
		if hyperNode := taskOccupiedHyperNodeAtTier(task, hyperNodes, tier, nodesByHyperNode); hyperNode != "" {
			occupied.Insert(hyperNode)
		}
	}
	if occupied.Len() > 0 {
		return occupied
	}

	// Fallback: use job.AllocatedHyperNode when the task path yields nothing.
	//
	// Primary path is per-task placement (above). Steady-state Running jobs with a
	// complete node→HyperNode mapping should always hit that path.
	//
	// Fallback is for cases where placement is recorded on the job but tasks cannot
	// be mapped to HyperNodes at tier, for example:
	//   - unit tests that set AllocatedHyperNode without populating tasks;
	//   - scheduler restart or early session: annotation/cache has AllocatedHyperNode
	//     before allocated tasks are fully synced into JobInfo;
	//   - HyperNode RealNodesSet not ready or incomplete (nodeName present but no
	//     matching entry in nodesByHyperNode).
	//
	// Limitation: when AllocatedHyperNode is an LCA spanning sibling domains (e.g.
	// root while pods sit on hn-A and hn-B), ResolveHyperNodesAtTier expands to all
	// tier siblings and may over-block. That is why the task path is preferred; this
	// fallback is best-effort for single-domain placement or transient cache gaps.
	allocatedHyperNode := getJobAllocatedHyperNode(job, hyperNodes, nodesByHyperNode)
	for _, hyperNode := range hyperNodes.ResolveHyperNodesAtTier(allocatedHyperNode, tier) {
		occupied.Insert(hyperNode)
	}
	return occupied
}

func taskOccupiedHyperNodeAtTier(
	task *TaskInfo,
	hyperNodes HyperNodeInfoMap,
	tier int,
	nodesByHyperNode map[string]sets.Set[string],
) string {
	if task == nil || task.NodeName == "" || len(nodesByHyperNode) == 0 {
		return ""
	}

	finestName := ""
	finestTier := 0
	for name, nodes := range nodesByHyperNode {
		if !nodes.Has(task.NodeName) {
			continue
		}
		hyperNode, ok := hyperNodes[name]
		if !ok {
			continue
		}
		if finestName == "" || hyperNode.Tier() < finestTier {
			finestName = name
			finestTier = hyperNode.Tier()
		}
	}
	if finestName == "" {
		return ""
	}
	return hyperNodes.GetAncestorHyperNode(finestName, tier)
}

// MatchingPodGroupsAllocatedHyperNodesForTerm returns ancestor HyperNodes at the term tier
// where matching PodGroups (other than selfJob) are already allocated.
// nodesByHyperNode is used to infer placement for matching PodGroups without AllocatedHyperNode.
func MatchingPodGroupsAllocatedHyperNodesForTerm(
	jobs map[JobID]*JobInfo,
	hyperNodes HyperNodeInfoMap,
	tierNameMap HyperNodeTierNameMap,
	selfJob *JobInfo,
	term scheduling.PodGroupAffinityTerm,
	nodesByHyperNode map[string]sets.Set[string],
) (sets.Set[string], error) {
	return MatchingPodGroupsAllocatedHyperNodesForTermWithNamespaceLister(
		jobs, hyperNodes, tierNameMap, selfJob, term, nodesByHyperNode, nil,
	)
}

func MatchingPodGroupsAllocatedHyperNodesForTermWithNamespaceLister(
	jobs map[JobID]*JobInfo,
	hyperNodes HyperNodeInfoMap,
	tierNameMap HyperNodeTierNameMap,
	selfJob *JobInfo,
	term scheduling.PodGroupAffinityTerm,
	nodesByHyperNode map[string]sets.Set[string],
	namespaceLister corelisters.NamespaceLister,
) (sets.Set[string], error) {
	tier, err := ResolvePodGroupTermTier(term, tierNameMap)
	if err != nil {
		return nil, err
	}

	matchingHyperNodes := sets.New[string]()
	for _, matchingJob := range jobs {
		matched, err := PodGroupMatchesTermWithNamespaceLister(term, selfJob, matchingJob, namespaceLister)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		occupiedHyperNodes := CollectJobOccupiedHyperNodesAtTier(matchingJob, hyperNodes, tier, nodesByHyperNode)
		if occupiedHyperNodes.Len() == 0 {
			// matching job not yet placed, it occupies no domain, skip it.
			continue
		}
		resolvedHyperNodes := occupiedHyperNodes.UnsortedList()
		sort.Strings(resolvedHyperNodes)
		allocatedHyperNode := getJobAllocatedHyperNode(matchingJob, hyperNodes, nodesByHyperNode)
		klog.V(3).Infof("podGroup anti-affinity: matching job hyperNode, job=%s, matchingJob=%s, termTier=%d, allocatedHyperNode=%s, resolvedHyperNodes=%s",
			klog.KRef(selfJob.Namespace, selfJob.Name),
			klog.KRef(matchingJob.Namespace, matchingJob.Name),
			tier, allocatedHyperNode, strings.Join(resolvedHyperNodes, ","))
		for _, hyperNode := range resolvedHyperNodes {
			matchingHyperNodes.Insert(hyperNode)
		}
	}
	return matchingHyperNodes, nil
}
