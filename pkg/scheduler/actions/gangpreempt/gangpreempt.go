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

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// MaxDomainsKey caps the number of candidate hyper-node domains scanned per starving preemptor.
	MaxDomainsKey = "maxDomains"
	// AllowWholeBundleKey toggles whether whole-job ("whole-bundle") victim bundles may be selected.
	AllowWholeBundleKey = "allowWholeBundle"
	// defaultMaxDomains is used when maxDomains is unset or invalid (<= 0).
	defaultMaxDomains = 8
	// defaultWholeBundleOn is the default for allowWholeBundle.
	defaultWholeBundleOn = true
)

type Action struct {
	// maxDomains caps the number of candidate hyper-node domains scanned per starving preemptor.
	maxDomains int
	// allowWholeBundle permits selecting whole-job victim bundles when true.
	allowWholeBundle bool
	// configured flag for predicate error cache
	enablePredicateErrorCache bool
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
	if gp.maxDomains <= 0 {
		klog.Warningf("Invalid %s value %d for action %s, falling back to default %d",
			MaxDomainsKey, gp.maxDomains, gp.Name(), defaultMaxDomains)
		gp.maxDomains = defaultMaxDomains
	}
	arguments.GetBool(&gp.allowWholeBundle, AllowWholeBundleKey)

	// Honor allocate's predicateErrorCacheEnable for the per-sub-job simulation, defaulting to
	// enabled when allocate is not configured.
	gp.enablePredicateErrorCache = true
	framework.GetArgOfActionFromConf(ssn.Configurations, "allocate").GetBool(&gp.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
}

func (gp *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter GangPreempt ...")
	defer klog.V(5).Infof("Leaving GangPreempt ...")

	gp.parseArguments(ssn)
	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}

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
		if _, ok := queueMap[queue.UID]; !ok {
			queueMap[queue.UID] = queue
			queues.Push(queue)
		}

		if !ssn.JobStarving(job) {
			continue
		}
		if _, found := preemptorsMap[job.Queue]; !found {
			preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
		}
		preemptorsMap[job.Queue].Push(job)
	}

	for !queues.Empty() {
		queue := queues.Pop().(*api.QueueInfo)
		for {
			preemptors := preemptorsMap[queue.UID]
			if preemptors == nil || preemptors.Empty() {
				break
			}

			preemptorJob := preemptors.Pop().(*api.JobInfo)
			stmt := framework.NewStatement(ssn)
			subJobHyperNodes := gp.preemptJobInDomains(ssn, stmt, queue, preemptorJob)

			// Mirror legacy commit behavior: commit only when the job becomes pipelined.
			if ssn.JobPipelined(preemptorJob) {
				stmt.Commit()
				utils.ApplySubJobNominations(ssn, preemptorJob, subJobHyperNodes)
			} else {
				stmt.Discard()
			}
		}
	}
}

func (gp *Action) UnInitialize() {}

func (gp *Action) preemptJobInDomains(ssn *framework.Session, stmt *framework.Statement, queue *api.QueueInfo, preemptorJob *api.JobInfo) map[api.SubJobID]string {
	pending := utils.CollectPendingTasksForGangEviction(ssn, preemptorJob)
	if len(pending) == 0 {
		return nil
	}

	jobNeed := utils.SumInitResreq(pending)
	domains := utils.GetCandidateDomains(ssn, preemptorJob, gp.maxDomains)
	for _, domain := range domains {
		domainNodes := ssn.RealNodesList[domain]
		if len(domainNodes) == 0 {
			continue
		}
		domainBundles := gp.selectDomainBundles(ssn, preemptorJob, pending, jobNeed, domain)
		if len(domainBundles) == 0 {
			continue
		}
		domainIdle := utils.SumIdleAndReleasing(domainNodes)
		selectedVictims := make([]*api.TaskInfo, 0)
		for _, bundle := range domainBundles {
			selectedVictims = append(selectedVictims, bundle.Tasks...)
			available := domainIdle.Clone()
			available.Add(utils.SumResreq(selectedVictims))
			if !jobNeed.LessEqual(available, api.Zero) {
				continue
			}

			attemptVictims := append([]*api.TaskInfo(nil), selectedVictims...)

			jobHN := ssn.HyperNodes[domain]
			if jobHN == nil {
				jobHN = ssn.HyperNodes[framework.ClusterTopHyperNode]
			}
			plan, subJobHyperNodes, ok := utils.BuildNominationPlanInDomain(ssn, queue, preemptorJob, jobHN, attemptVictims, utils.ReasonGangPreempt, gp.enablePredicateErrorCache)
			if !ok {
				continue
			}
			if err := stmt.RecoverOperations(plan); err != nil {
				continue
			}
			return subJobHyperNodes
		}
	}
	return nil
}

func (gp *Action) selectDomainBundles(ssn *framework.Session, preemptorJob *api.JobInfo, pendingTasks []*api.TaskInfo, jobNeed *api.Resource, domain string) []*utils.Bundle {
	domainNodes := ssn.RealNodesList[domain]
	if len(domainNodes) == 0 || len(pendingTasks) == 0 {
		return nil
	}

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
			if !utils.IsJobPreemptableForGangEviction(victimJob) {
				continue
			}
			if preemptorJob.Priority <= victimJob.Priority || task.Job == preemptorJob.UID {
				continue
			}
			candidatesByJob[task.Job] = append(candidatesByJob[task.Job], task.Clone())
		}
	}

	bundles := make([]*utils.Bundle, 0)
	for jobID, tasks := range candidatesByJob {
		victimJob := ssn.Jobs[jobID]
		bundles = append(bundles, utils.CreateJobBundles(victimJob, tasks)...)
	}
	if len(bundles) == 0 {
		return nil
	}
	utils.SortBundlesForPreempt(bundles, jobNeed, func(l, r *api.JobInfo) bool {
		return !ssn.JobOrderFn(l, r)
	})

	evictCtx := &api.EvictionContext{
		Kind:      api.EvictionKindGangPreempt,
		Job:       preemptorJob,
		HyperNode: domain,
	}
	orderedCandidates := utils.FlattenBundles(bundles)
	allowed := ssn.UnifiedEvictable(evictCtx, orderedCandidates)
	if len(allowed) == 0 {
		return nil
	}
	allowedSet := make(map[api.TaskID]struct{}, len(allowed))
	for _, t := range allowed {
		allowedSet[t.UID] = struct{}{}
	}
	valid := utils.ApplyAllowedTasks(bundles, allowedSet)
	if len(valid) == 0 {
		return nil
	}
	utils.SortBundlesForPreempt(valid, jobNeed, func(l, r *api.JobInfo) bool {
		return !ssn.JobOrderFn(l, r)
	})
	selected := make([]*utils.Bundle, 0, len(valid))
	for _, bundle := range valid {
		if bundle.Type == utils.BundleWhole && !gp.allowWholeBundle {
			continue
		}
		selected = append(selected, bundle)
	}
	return selected
}
