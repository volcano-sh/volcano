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

package gangreclaim

import (
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// MaxDomainsKey caps the number of candidate hyper-node domains scanned per starving reclaimer.
	MaxDomainsKey = "maxDomains"
	// AllowWholeBundleKey toggles whether whole-job ("whole-bundle") victim bundles may be selected.
	AllowWholeBundleKey = "allowWholeBundle"
	// defaultMaxDomains is used when maxDomains is unset or invalid (<= 0).
	defaultMaxDomains = 8
	// defaultWholeBundleOn is the default for allowWholeBundle.
	defaultWholeBundleOn = true
)

type Action struct {
	// maxDomains caps the number of candidate hyper-node domains scanned per starving reclaimer.
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

func (gr *Action) Name() string {
	return "gangreclaim"
}

func (gr *Action) Initialize() {}

func (gr *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, gr.Name())
	arguments.GetInt(&gr.maxDomains, MaxDomainsKey)
	if gr.maxDomains <= 0 {
		klog.Warningf("Invalid %s value %d for action %s, falling back to default %d",
			MaxDomainsKey, gr.maxDomains, gr.Name(), defaultMaxDomains)
		gr.maxDomains = defaultMaxDomains
	}
	arguments.GetBool(&gr.allowWholeBundle, AllowWholeBundleKey)

	// Honor allocate's predicateErrorCacheEnable for the per-sub-job simulation, defaulting to
	// enabled when allocate is not configured.
	gr.enablePredicateErrorCache = true
	framework.GetArgOfActionFromConf(ssn.Configurations, "allocate").GetBool(&gr.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
}

func (gr *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter GangReclaim ...")
	defer klog.V(5).Infof("Leaving GangReclaim ...")

	gr.parseArguments(ssn)
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}
	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}

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
		if ssn.Overused(queue) {
			continue
		}

		for {
			jobsQ, found := preemptorsMap[queue.UID]
			if !found || jobsQ.Empty() {
				break
			}
			job := jobsQ.Pop().(*api.JobInfo)
			stmt := framework.NewStatement(ssn)
			subJobHyperNodes := gr.reclaimJobInDomains(ssn, stmt, queue, job)

			// Mirror legacy commit behavior: commit only when the job becomes pipelined.
			if ssn.JobPipelined(job) {
				stmt.Commit()
				utils.ApplySubJobNominations(ssn, job, subJobHyperNodes)
			} else {
				stmt.Discard()
			}
		}
	}
}

func (gr *Action) UnInitialize() {}

func (gr *Action) reclaimJobInDomains(ssn *framework.Session, stmt *framework.Statement, queue *api.QueueInfo, job *api.JobInfo) map[api.SubJobID]string {
	pending := utils.CollectPendingTasksForGangEviction(ssn, job)
	if len(pending) == 0 {
		return nil
	}
	if queue != nil && !ssn.Preemptive(queue, pending) {
		klog.V(3).Infof("Queue <%s> cannot reclaim for job <%s/%s>, skip", queue.Name, job.Namespace, job.Name)
		return nil
	}
	jobNeed := utils.SumInitResreq(pending)
	domains := utils.GetCandidateDomains(ssn, job, gr.maxDomains)
	for _, domain := range domains {
		nodes := ssn.RealNodesList[domain]
		if len(nodes) == 0 {
			continue
		}
		domainBundles := gr.selectDomainBundles(ssn, queueLessFn(ssn, job.Queue), job, pending, jobNeed, domain)
		if len(domainBundles) == 0 {
			continue
		}
		domainIdle := utils.SumIdleAndReleasing(nodes)
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
			plan, subJobHyperNodes, ok := utils.BuildNominationPlanInDomain(ssn, queue, job, jobHN, attemptVictims, utils.ReasonGangReclaim, gr.enablePredicateErrorCache)
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

func (gr *Action) selectDomainBundles(ssn *framework.Session, lessQueueFn func(l, r *api.QueueInfo) bool, reclaimerJob *api.JobInfo, pendingTasks []*api.TaskInfo, jobNeed *api.Resource, domain string) []*utils.Bundle {
	domainNodes := ssn.RealNodesList[domain]
	if len(domainNodes) == 0 || len(pendingTasks) == 0 {
		return nil
	}

	candidatesByJob := map[api.JobID][]*api.TaskInfo{}
	for _, n := range domainNodes {
		for _, taskOnNode := range n.Tasks {
			if taskOnNode.Status != api.Running || !taskOnNode.Preemptable {
				continue
			}
			victimJob, found := ssn.Jobs[taskOnNode.Job]
			if !found || victimJob.Queue == reclaimerJob.Queue {
				continue
			}
			if !utils.IsJobPreemptableForGangEviction(victimJob) {
				continue
			}
			q := ssn.Queues[victimJob.Queue]
			if q == nil || !q.Reclaimable() {
				continue
			}
			candidatesByJob[taskOnNode.Job] = append(candidatesByJob[taskOnNode.Job], taskOnNode.Clone())
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
	utils.SortBundlesForReclaim(bundles, jobNeed, lessQueueFn, ssn.Queues)

	evictCtx := &api.EvictionContext{
		Kind:      api.EvictionKindGangReclaim,
		Job:       reclaimerJob,
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
	utils.SortBundlesForReclaim(valid, jobNeed, lessQueueFn, ssn.Queues)
	selected := make([]*utils.Bundle, 0, len(valid))
	for _, bundle := range valid {
		if bundle.Type == utils.BundleWhole && !gr.allowWholeBundle {
			continue
		}
		selected = append(selected, bundle)
	}
	return selected
}

func queueLessFn(ssn *framework.Session, preemptorQueue api.QueueID) func(l, r *api.QueueInfo) bool {
	return func(l, r *api.QueueInfo) bool {
		pq := ssn.Queues[preemptorQueue]
		if pq == nil {
			return ssn.QueueOrderFn(l, r)
		}
		return ssn.VictimQueueOrderFn(l, r, pq)
	}
}
