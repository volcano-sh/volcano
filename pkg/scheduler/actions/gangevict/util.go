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

package gangevict

import (
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
func PickDomainsFromGradients(gradients [][]*api.HyperNodeInfo, maxDomains int, fallback string) []string {
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
// and returns an ordered domain list capped at maxDomains.
func GetCandidateDomains(ssn *framework.Session, job *api.JobInfo, maxDomains int) []string {
	root := ssn.HyperNodes[framework.ClusterTopHyperNode]
	gradients := ssn.HyperNodeGradientForJobFn(job, root, api.PurposeEvict)
	fallback := ""
	if root != nil && (job == nil || !job.ContainsHardTopology()) {
		fallback = root.Name
	}
	return PickDomainsFromGradients(gradients, maxDomains, fallback)
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
