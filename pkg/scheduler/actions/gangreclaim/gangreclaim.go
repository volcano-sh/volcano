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

func (gr *Action) Name() string {
	return "gangreclaim"
}

func (gr *Action) Initialize() {}

func (gr *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, gr.Name())
	arguments.GetInt(&gr.maxDomains, MaxDomainsKey)
	arguments.GetBool(&gr.allowWholeBundle, AllowWholeBundleKey)
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
			_ = gr.reclaimJobInDomains(ssn, stmt, queue, job)

			// Mirror legacy commit behavior: commit only when the job becomes pipelined.
			if ssn.JobPipelined(job) {
				stmt.Commit()
			} else {
				stmt.Discard()
			}
		}
	}
}

func (gr *Action) UnInitialize() {}

func (gr *Action) reclaimJobInDomains(ssn *framework.Session, stmt *framework.Statement, queue *api.QueueInfo, job *api.JobInfo) bool {
	pending := gangevict.CollectPendingTasksForGangEviction(ssn, job, ssn.SubJobOrderFn, ssn.TaskOrderFn)
	if len(pending) == 0 {
		return false
	}
	if queue != nil && !ssn.Preemptive(queue, pending) {
		klog.V(3).Infof("Queue <%s> cannot reclaim for job <%s/%s>, skip", queue.Name, job.Namespace, job.Name)
		return false
	}
	jobNeed := gangevict.SumInitResreq(pending)
	domains := gangevict.GetCandidateDomains(ssn, job, gr.maxDomains)
	for _, domain := range domains {
		if !ssn.JobStarving(job) {
			break
		}
		nodes := ssn.RealNodesList[domain]
		if len(nodes) == 0 {
			continue
		}
		domainBundles := gr.selectDomainBundles(ssn, queueLessFn(ssn, job.Queue), job, pending, domain)
		if len(domainBundles) == 0 {
			continue
		}
		domainIdle := gangevict.SumIdleAndReleasing(nodes)
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
			plan, ok := gangevict.BuildNominationPlanInDomain(ssn, queue, job, jobHN, attemptVictims, gangevict.ReasonGangReclaim)
			if !ok {
				continue
			}
			if err := stmt.RecoverOperations(plan); err != nil {
				continue
			}
			return true
		}
	}
	return false
}

func (gr *Action) selectDomainBundles(ssn *framework.Session, lessQueueFn func(l, r *api.QueueInfo) bool, reclaimerJob *api.JobInfo, pendingTasks []*api.TaskInfo, domain string) []*gangevict.Bundle {
	domainNodes := ssn.RealNodesList[domain]
	if len(domainNodes) == 0 || len(pendingTasks) == 0 {
		return nil
	}
	jobNeed := gangevict.SumInitResreq(pendingTasks)

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
			if !gangevict.IsJobPreemptableForGangEviction(victimJob) {
				continue
			}
			q := ssn.Queues[victimJob.Queue]
			if q == nil || !q.Reclaimable() {
				continue
			}
			candidatesByJob[taskOnNode.Job] = append(candidatesByJob[taskOnNode.Job], taskOnNode.Clone())
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
	gangevict.SortBundlesForReclaim(bundles, jobNeed, lessQueueFn, ssn.Queues)

	evictCtx := &api.EvictionContext{
		Kind:      api.EvictionKindGangReclaim,
		Job:       reclaimerJob,
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
	gangevict.SortBundlesForReclaim(valid, jobNeed, lessQueueFn, ssn.Queues)
	selected := make([]*gangevict.Bundle, 0, len(valid))
	for _, bundle := range valid {
		if bundle.Type == gangevict.BundleWhole && !gr.allowWholeBundle {
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
