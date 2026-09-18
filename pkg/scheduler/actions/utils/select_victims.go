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
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// SelectMinimalVictimsAndPlan walks pre-sorted victim bundles and commits a nomination
// plan that evicts the fewest victims sufficient to place job's pending tasks. It is the
// shared victim-selection path for the gangpreempt and gangreclaim actions.
//
// Bundles are consumed in order. Every bundle before the one that first satisfies the
// resource demand is resource-necessary — even the full sum up to it was still short — so
// it is taken whole. Only that boundary bundle is trimmed: its tasks (already sorted
// safe-first) are added one at a time, and the first prefix that both covers jobNeed and
// yields a successful placement simulation is committed. If no prefix of the boundary
// bundle places the job, the whole bundle is kept and selection widens to the next bundle,
// matching the previous whole-bundle behavior as a fallback, so the result is never worse
// than selecting whole bundles.
//
// available is kept equal to domainIdle + Resreq(victims) at all times, grown in lockstep
// with the victims slice, so the resource gate needs no re-summation.
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

	for _, bundle := range bundles {
		// Would this whole bundle still leave us short? Then it is resource-necessary;
		// take it whole and move on without simulating.
		probe := available.Clone()
		probe.Add(bundle.LocalRes)
		if !jobNeed.LessEqual(probe, api.Zero) {
			available.Add(bundle.LocalRes)
			victims = append(victims, bundle.Tasks...)
			continue
		}

		// Boundary bundle: grow a safe-first prefix and stop at the smallest one that
		// both covers jobNeed and actually places the job in a simulation.
		//
		// SAFE tasks are surplus above the gang minimum, so any prefix of them is a
		// valid victim set. A WHOLE bundle instead represents tearing down the victim
		// job's minimum and is all-or-nothing, so it is never attempted as a partial
		// prefix -- only once every task in the WHOLE bundle has been included.
		for i, task := range bundle.Tasks {
			available.Add(task.Resreq)
			victims = append(victims, task)
			if bundle.Type == BundleWhole && i < len(bundle.Tasks)-1 {
				continue
			}
			if !jobNeed.LessEqual(available, api.Zero) {
				continue
			}

			attempt := append([]*api.TaskInfo(nil), victims...)
			plan, subJobHyperNodes, ok := BuildNominationPlanInDomain(ssn, queue, job, jobDomainHyperNode, attempt, reason, enablePredCache)
			if !ok {
				// Resources cover jobNeed but placement failed (predicate/topology);
				// widen by adding the next task.
				continue
			}
			if err := stmt.RecoverOperations(plan); err != nil {
				continue
			}
			return subJobHyperNodes, true
		}
	}
	return nil, false
}
