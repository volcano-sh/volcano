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
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// buildPlanFn simulates eviction + placement. It is a variable so tests can count simulations.
var buildPlanFn = BuildNominationPlanInDomain

// SelectMinimalVictimsAndPlan walks pre-sorted victim bundles and commits a nomination
// plan that evicts as few victims as it can while still placing job's pending tasks. It is
// the shared victim-selection path for the gangpreempt and gangreclaim actions.
//
// Bundles are consumed in order. Every bundle before the one that first satisfies the
// resource demand is resource-necessary — even the full sum up to it was still short — so
// it is taken whole. Only that boundary bundle is trimmed to the shortest safe-first prefix
// that places the job. Evicting more victims only frees resources, so placement is treated
// as monotone in the prefix length: the whole bundle is simulated first and, if it places
// the job, a binary search finds the shortest placing prefix in O(log n) simulations. If
// the whole bundle does not place the job, it is kept and selection widens to the next
// bundle, so the result is never worse than selecting whole bundles.
//
// The resource gate is domain-wide, so the selected prefix can include victims on nodes
// the job is not placed on. After a successful simulation, victims on nodes that receive
// none of the job's tasks are dropped (a WHOLE bundle is kept or dropped as a unit, since a
// partial eviction still breaks its gang) and the smaller set is simulated again; it is
// committed only if it still places the job, otherwise the original set is committed. The
// re-simulation is needed because a victim on another node can still matter, for example by
// freeing quota of the job's own queue.
//
// The result is minimal within the bundle order, not a global minimum over all victim
// combinations.
//
// On success the plan is applied to stmt via RecoverOperations and (subJobHyperNodes, true)
// is returned; on failure the session/statement are left unchanged and (nil, false) is
// returned.
func SelectMinimalVictimsAndPlan(
	ssn *framework.Session,
	stmt *framework.Statement,
	queue *api.QueueInfo,
	job *api.JobInfo,
	jobDomainHyperNode *api.HyperNodeInfo,
	bundles []*Bundle,
	domainIdle *api.Resource,
	jobNeed *api.Resource,
	reason string,
	enablePredCache bool,
) (map[api.SubJobID]string, bool) {
	available := domainIdle.Clone()
	victims := make([]*api.TaskInfo, 0)
	wholeBundleOf := map[api.TaskID]*Bundle{}

	addVictim := func(bundle *Bundle, task *api.TaskInfo) {
		available.Add(task.Resreq)
		victims = append(victims, task)
		if bundle.Type == BundleWhole {
			wholeBundleOf[task.UID] = bundle
		}
	}

	for _, bundle := range bundles {
		// Would this whole bundle still leave us short? Then it is resource-necessary;
		// take it whole and move on without simulating.
		probe := available.Clone()
		probe.Add(bundle.LocalRes)
		if !jobNeed.LessEqual(probe, api.Zero) {
			for _, task := range bundle.Tasks {
				addVictim(bundle, task)
			}
			continue
		}

		// Boundary bundle: find the shortest safe-first prefix that both covers jobNeed and
		// actually places the job in a simulation.
		//
		// SAFE tasks are surplus above the gang minimum, so any prefix of them is a
		// valid victim set. A WHOLE bundle instead represents tearing down the victim
		// job's minimum and is all-or-nothing, so it is only ever attempted whole.
		//
		// Evicting more victims only frees resources, so placement gets easier as the prefix
		// grows. The whole bundle is simulated first: if even it does not place the job, the
		// selection widens to the next bundle right away. Otherwise a binary search finds the
		// shortest placing prefix with O(log n) simulations instead of one per task.
		shortest := len(bundle.Tasks)
		if bundle.Type != BundleWhole {
			covered := available.Clone()
			for i, task := range bundle.Tasks {
				covered.Add(task.Resreq)
				if jobNeed.LessEqual(covered, api.Zero) {
					shortest = i + 1
					break
				}
			}
		}
		simulatePrefix := func(n int) ([]*api.TaskInfo, *framework.Statement, map[api.SubJobID]string, bool) {
			attempt := append(append([]*api.TaskInfo(nil), victims...), bundle.Tasks[:n]...)
			plan, subJobHyperNodes, ok := buildPlanFn(ssn, queue, job, jobDomainHyperNode, attempt, reason, enablePredCache)
			return attempt, plan, subJobHyperNodes, ok
		}
		attempt, plan, subJobHyperNodes, ok := simulatePrefix(len(bundle.Tasks))
		if ok {
			lo, hi := shortest, len(bundle.Tasks)
			for lo < hi {
				mid := lo + (hi-lo)/2
				if a, p, h, placed := simulatePrefix(mid); placed {
					hi, attempt, plan, subJobHyperNodes = mid, a, p, h
				} else {
					lo = mid + 1
				}
			}
			if bundle.Type == BundleWhole {
				for _, task := range bundle.Tasks {
					wholeBundleOf[task.UID] = bundle
				}
			}
			plan, subJobHyperNodes = pruneVictimsOffPlacedNodes(ssn, queue, job, jobDomainHyperNode, attempt, wholeBundleOf, plan, subJobHyperNodes, reason, enablePredCache)
			if err := stmt.RecoverOperations(plan); err == nil {
				return subJobHyperNodes, true
			}
		}
		// No prefix of this bundle places the job (predicate, topology or queue quota);
		// keep it whole and widen to the next bundle.
		for _, task := range bundle.Tasks {
			addVictim(bundle, task)
		}
	}
	return nil, false
}

// pruneVictimsOffPlacedNodes drops the victims on nodes that receive none of the job's tasks
// in plan, keeping a WHOLE bundle only as a unit, and re-simulates. It returns the smaller
// plan if the job still places, otherwise the original plan.
func pruneVictimsOffPlacedNodes(
	ssn *framework.Session,
	queue *api.QueueInfo,
	job *api.JobInfo,
	jobDomainHyperNode *api.HyperNodeInfo,
	victims []*api.TaskInfo,
	wholeBundleOf map[api.TaskID]*Bundle,
	plan *framework.Statement,
	subJobHyperNodes map[api.SubJobID]string,
	reason string,
	enablePredCache bool,
) (*framework.Statement, map[api.SubJobID]string) {
	placed := plan.PlacedNodes()
	keepWhole := map[*Bundle]bool{}
	for _, v := range victims {
		if b, ok := wholeBundleOf[v.UID]; ok {
			if _, on := placed[v.NodeName]; on {
				keepWhole[b] = true
			}
		}
	}

	pruned := make([]*api.TaskInfo, 0, len(victims))
	for _, v := range victims {
		if b, ok := wholeBundleOf[v.UID]; ok {
			if keepWhole[b] {
				pruned = append(pruned, v)
			}
			continue
		}
		if _, on := placed[v.NodeName]; on {
			pruned = append(pruned, v)
		}
	}
	if len(pruned) == 0 || len(pruned) == len(victims) {
		return plan, subJobHyperNodes
	}

	prunedPlan, prunedHyperNodes, ok := buildPlanFn(ssn, queue, job, jobDomainHyperNode, pruned, reason, enablePredCache)
	if !ok {
		klog.V(4).Infof("Job <%s/%s>: dropping %d victims off the placed nodes no longer places the job, keeping %d victims",
			job.Namespace, job.Name, len(victims)-len(pruned), len(victims))
		return plan, subJobHyperNodes
	}
	klog.V(4).Infof("Job <%s/%s>: dropped %d victims on nodes the job is not placed on, evicting %d",
		job.Namespace, job.Name, len(victims)-len(pruned), len(pruned))
	return prunedPlan, prunedHyperNodes
}
