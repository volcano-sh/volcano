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

package gangpreempt

import (
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/actions/gangevict"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	MaxDomainsKey        = "maxDomains"
	AllowWholeBundleKey  = "allowWholeBundle"
	defaultMaxDomains    = 8
	defaultWholeBundleOn = true
)

type Action struct {
	maxDomains       int
	allowWholeBundle bool
}

func New() *Action {
	return &Action{
		maxDomains:       defaultMaxDomains,
		allowWholeBundle: defaultWholeBundleOn,
	}
}

func (gp *Action) Name() string {
	return "gangpreempt"
}

func (gp *Action) Initialize() {}

func (gp *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, gp.Name())
	arguments.GetInt(&gp.maxDomains, MaxDomainsKey)
	arguments.GetBool(&gp.allowWholeBundle, AllowWholeBundleKey)
}

func (gp *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter GangPreempt ...")
	defer klog.V(5).Infof("Leaving GangPreempt ...")

	gp.parseArguments(ssn)
	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	queues := map[api.QueueID]*api.QueueInfo{}

	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}
		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			continue
		}

		queue, found := ssn.Queues[job.Queue]
		if !found {
			continue
		}
		queues[queue.UID] = queue

		if !ssn.JobStarving(job) {
			continue
		}
		if _, found := preemptorsMap[job.Queue]; !found {
			preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
		}
		preemptorsMap[job.Queue].Push(job)
	}

	for _, queue := range queues {
		for {
			preemptors := preemptorsMap[queue.UID]
			if preemptors == nil || preemptors.Empty() {
				break
			}

			preemptorJob := preemptors.Pop().(*api.JobInfo)
			stmt := framework.NewStatement(ssn)
			assigned := false

			assigned, _ = gp.preemptJobInDomains(ssn, stmt, queue, preemptorJob)

			// Mirror legacy commit behavior: commit only when the job becomes pipelined.
			if ssn.JobPipelined(preemptorJob) {
				stmt.Commit()
				if assigned {
					preemptors.Push(preemptorJob)
				}
			} else {
				stmt.Discard()
			}
		}
	}
}

func (gp *Action) UnInitialize() {}

func (gp *Action) preemptJobInDomains(ssn *framework.Session, stmt *framework.Statement, queue *api.QueueInfo, preemptorJob *api.JobInfo) (bool, error) {
	pending := gangevict.CollectPendingTasksForGangEviction(ssn, preemptorJob, ssn.SubJobOrderFn, ssn.TaskOrderFn)
	if len(pending) == 0 {
		return false, nil
	}

	jobNeed := gangevict.SumInitResreq(pending)
	domains := gangevict.GetCandidateDomains(ssn, preemptorJob, gp.maxDomains)
	for _, domain := range domains {
		if !ssn.JobStarving(preemptorJob) {
			break
		}
		domainNodes := ssn.RealNodesList[domain]
		if len(domainNodes) == 0 {
			continue
		}
		domainBundles := gp.selectDomainBundles(ssn, preemptorJob, pending, domain)
		if len(domainBundles) == 0 {
			continue
		}
		domainIdle := gangevict.SumIdleAndReleasing(domainNodes)
		selectedVictims := make([]*api.TaskInfo, 0)
		for _, bundle := range domainBundles {
			selectedVictims = append(selectedVictims, bundle.Tasks...)
			available := domainIdle.Clone()
			available.Add(gangevict.SumResreq(selectedVictims))
			if !jobNeed.LessEqual(available, api.Zero) {
				continue
			}

			attemptVictims := append([]*api.TaskInfo(nil), selectedVictims...)

			var jobHN *api.HyperNodeInfo
			if ssn.HyperNodes != nil {
				jobHN = ssn.HyperNodes[domain]
				if jobHN == nil {
					jobHN = ssn.HyperNodes[framework.ClusterTopHyperNode]
				}
			} else {
				jobHN = &api.HyperNodeInfo{Name: domain}
			}
			plan, ok := gangevict.BuildNominationPlanInDomain(ssn, queue, preemptorJob, jobHN, attemptVictims, gangevict.ReasonGangPreempt)
			if !ok {
				continue
			}
			if err := stmt.RecoverOperations(plan); err != nil {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func (gp *Action) selectDomainBundles(ssn *framework.Session, preemptorJob *api.JobInfo, pendingTasks []*api.TaskInfo, domain string) []*gangevict.Bundle {
	domainNodes := ssn.RealNodesList[domain]
	if len(domainNodes) == 0 || len(pendingTasks) == 0 {
		return nil
	}

	jobNeed := gangevict.SumInitResreq(pendingTasks)

	candidatesByJob := map[api.JobID][]*api.TaskInfo{}
	for _, node := range domainNodes {
		for _, task := range node.Tasks {
			if !api.PreemptableStatus(task.Status) || !task.Preemptable {
				continue
			}
			// NOTE: legacy preempt filters out non-BestEffort victims when
			// the preemptor is BestEffort. That check is intentionally
			// removed here; UnifiedEvictableFn plugins handle eligibility.
			victimJob, found := ssn.Jobs[task.Job]
			if !found || victimJob.Queue != preemptorJob.Queue {
				continue
			}
			if !gangevict.IsJobPreemptableForGangEviction(victimJob) {
				continue
			}
			if preemptorJob.Priority <= victimJob.Priority || task.Job == preemptorJob.UID {
				continue
			}
			candidatesByJob[task.Job] = append(candidatesByJob[task.Job], task.Clone())
		}
	}

	bundles := make([]*gangevict.Bundle, 0)
	for jobID, tasks := range candidatesByJob {
		victimJob := ssn.Jobs[jobID]
		bundles = append(bundles, gangevict.CreateJobBundles(victimJob, tasks)...)
	}
	if len(bundles) == 0 {
		return nil
	}
	gangevict.SortBundlesForPreempt(bundles, jobNeed, func(l, r *api.JobInfo) bool {
		return !ssn.JobOrderFn(l, r)
	})

	evictCtx := &api.EvictionContext{
		Kind:      api.EvictionKindGangPreempt,
		Job:       preemptorJob,
		HyperNode: domain,
	}
	orderedCandidates := gangevict.FlattenBundles(bundles)
	allowed := ssn.UnifiedEvictable(evictCtx, orderedCandidates)
	if len(allowed) == 0 {
		return nil
	}
	allowedSet := make(map[api.TaskID]struct{}, len(allowed))
	for _, t := range allowed {
		allowedSet[t.UID] = struct{}{}
	}
	valid := gangevict.ApplyAllowedTasks(bundles, allowedSet)
	if len(valid) == 0 {
		return nil
	}
	gangevict.SortBundlesForPreempt(valid, jobNeed, func(l, r *api.JobInfo) bool {
		return !ssn.JobOrderFn(l, r)
	})
	selected := make([]*gangevict.Bundle, 0, len(valid))
	for _, bundle := range valid {
		if bundle.Type == gangevict.BundleWhole && !gp.allowWholeBundle {
			continue
		}
		selected = append(selected, bundle)
	}
	return selected
}
