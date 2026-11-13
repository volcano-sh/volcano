/*
Copyright 2026 The Volcano Authors.

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

package utils

import (
	"slices"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// SumInitResreq sums InitResreq across the given tasks.
func SumInitResreq(tasks []*api.TaskInfo) *api.Resource {
	demand := api.EmptyResource()
	for _, task := range tasks {
		demand.Add(task.InitResreq)
	}
	return demand
}

// SumIdleAndReleasing sums Idle + Releasing across the given nodes.
func SumIdleAndReleasing(nodes []*api.NodeInfo) *api.Resource {
	idle := api.EmptyResource()
	for _, node := range nodes {
		nodeFutureIdle := api.EmptyResource()
		if node.Idle != nil {
			nodeFutureIdle.Add(node.Idle)
		}
		if node.Releasing != nil {
			nodeFutureIdle.Add(node.Releasing)
		}
		idle.Add(nodeFutureIdle)
	}
	return idle
}

// SumResreq sums Resreq across the given tasks.
func SumResreq(tasks []*api.TaskInfo) *api.Resource {
	total := api.EmptyResource()
	for _, task := range tasks {
		total.Add(task.Resreq)
	}
	return total
}

// PickDomainsFromGradients flattens HyperNode gradients into an ordered list of
// domain names, capped at maxDomains. If no domains are found, fallback is used.
// HyperNodes rejected by a non-nil keep do not count toward the cap.
func PickDomainsFromGradients(gradients [][]*api.HyperNodeInfo, maxDomains int, fallback string, keep func(*api.HyperNodeInfo) bool) []string {
	seen := map[string]struct{}{}
	domains := make([]string, 0)
	for _, gradient := range gradients {
		for _, hn := range gradient {
			if hn == nil {
				continue
			}
			if _, ok := seen[hn.Name]; ok {
				continue
			}
			seen[hn.Name] = struct{}{}
			if keep != nil && !keep(hn) {
				continue
			}
			domains = append(domains, hn.Name)
			if maxDomains > 0 && len(domains) >= maxDomains {
				return domains
			}
		}
	}
	if len(domains) == 0 && fallback != "" {
		domains = append(domains, fallback)
	}
	return domains
}

// GetCandidateDomains fetches HyperNode gradients for the job with PurposeEvict
// and returns an ordered domain list capped at maxDomains. Domains where no
// pending task passes the preemption predicates are dropped before the cap.
func GetCandidateDomains(ssn *framework.Session, job *api.JobInfo, pending []*api.TaskInfo, maxDomains int) []string {
	root := ssn.HyperNodes[framework.ClusterTopHyperNode]
	gradients, _ := ssn.HyperNodeGradientForJobFn(job, root, api.PurposeEvict)
	fallback := ""
	if root != nil && (job == nil || (!job.ContainsHardTopology() && !job.ContainsHardPodGroupAntiAffinity())) {
		fallback = root.Name
	}
	return PickDomainsFromGradients(gradients, maxDomains, fallback, feasibleDomainFn(ssn, pending))
}

// feasibleDomainFn rejects domains where no node passes the preemption
// predicates for a pending task. Nomination runs the same check, so dropping
// them is safe. It returns nil (no filtering) when pre-predicate fails.
func feasibleDomainFn(ssn *framework.Session, pending []*api.TaskInfo) func(*api.HyperNodeInfo) bool {
	probes := predicateProbes(ssn, pending)
	if len(probes) == 0 {
		return nil
	}
	nodeFits := map[string]bool{}
	fits := func(node *api.NodeInfo) bool {
		if node == nil {
			return false
		}
		if fit, ok := nodeFits[node.Name]; ok {
			return fit
		}
		fit := slices.ContainsFunc(probes, func(task *api.TaskInfo) bool {
			return ssn.PredicateForPreemptAction(task, node) == nil
		})
		nodeFits[node.Name] = fit
		return fit
	}
	return func(hn *api.HyperNodeInfo) bool {
		return slices.ContainsFunc(ssn.RealNodesList[hn.Name], fits)
	}
}

// predicateProbes keeps one pre-predicated task per role, like allocate's
// per-role fit error skip (JobInfo.TaskHasFitErrors).
func predicateProbes(ssn *framework.Session, pending []*api.TaskInfo) []*api.TaskInfo {
	roles := map[string]struct{}{}
	probes := make([]*api.TaskInfo, 0, len(pending))
	for _, task := range pending {
		if task.TaskRole != "" {
			if _, ok := roles[task.TaskRole]; ok {
				continue
			}
			roles[task.TaskRole] = struct{}{}
		}
		if err := ssn.PrePredicateFn(task); err != nil {
			return nil
		}
		probes = append(probes, task)
	}
	return probes
}

// ApplySubJobNominations sets SubJobInfo.NominatedHyperNode on each
// subJob whose UID appears in subJobHyperNodes, after the outer
// gangpreempt/gangreclaim Statement has committed, and marks the job
// dirty so the cache writeback persists the NominatedHyperNode into the next cycle.
func ApplySubJobNominations(ssn *framework.Session, job *api.JobInfo, subJobHyperNodes map[api.SubJobID]string) {
	if job == nil {
		return
	}
	applied := false
	for sjID, hn := range subJobHyperNodes {
		if sj, ok := job.SubJobs[sjID]; ok && hn != "" {
			sj.NominatedHyperNode = hn
			applied = true
		}
	}
	if applied && ssn != nil {
		ssn.MarkJobDirty(job.UID)
	}
}

// IsJobPreemptableForGangEviction returns whether tasks from a victim job can be
// selected by gangpreempt/gangreclaim. Unset PodGroup preemptability defaults to
// allowed, while explicit PodGroup preemptability follows JobInfo.Preemptable.
func IsJobPreemptableForGangEviction(job *api.JobInfo) bool {
	if job == nil || job.PodGroup == nil {
		return true
	}

	if _, found := job.PodGroup.Annotations[v1beta1.PodPreemptable]; found {
		return job.Preemptable
	}
	if _, found := job.PodGroup.Labels[v1beta1.PodPreemptable]; found {
		return job.Preemptable
	}

	return true
}
