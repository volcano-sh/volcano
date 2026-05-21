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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

const (
	ReasonGangPreempt = "gangpreempt"
	ReasonGangReclaim = "gangreclaim"
)

// BuildNominationPlanInDomain dry-runs eviction + placement in jobDomainHyperNode and returns a
// fresh plan Statement only when the target job reaches JobPipelined. session is clean on return;
// callers must RecoverOperations(plan) to commit, or may drop the plan if not proceeding.
func BuildNominationPlanInDomain(ssn *framework.Session, queue *api.QueueInfo, job *api.JobInfo, jobDomainHyperNode *api.HyperNodeInfo, victims []*api.TaskInfo, reason string) (*framework.Statement, map[api.SubJobID]string, bool) {
	if ssn == nil || job == nil || jobDomainHyperNode == nil {
		return nil, nil, false
	}
	nodesForDomain := ssn.RealNodesList[jobDomainHyperNode.Name]
	if len(nodesForDomain) == 0 {
		return nil, nil, false
	}

	jobWasReady := ssn.JobReady(job)

	// Statement chain: one victimStmt (if any victims) plus one per successfully pipelined
	// sub-job, each kept applied to session so subsequent trials see the cumulative state. Failed
	// gradient trials Discard themselves. On success the chain is Saved into the returned plan
	// and then Discarded; on failure the chain is Discarded outright.
	var chain []*framework.Statement
	discardChain := func() {
		for i := len(chain) - 1; i >= 0; i-- {
			chain[i].Discard()
		}
	}

	if len(victims) > 0 {
		victimStmt := framework.NewStatement(ssn)
		for _, victim := range victims {
			victimStmt.Evict(victim, reason)
		}
		chain = append(chain, victimStmt)
	}

	victimsPresent := len(victims) > 0
	enablePredCache := true
	framework.GetArgOfActionFromConf(ssn.Configurations, "allocate").GetBool(&enablePredCache, conf.EnablePredicateErrCacheKey)

	worksheet := organizeEvictionWorksheet(ssn, job)
	if worksheet.Empty() {
		// TODO: Worksheet is empty while allocate may still have eligible pending (e.g. SchGated /
		// empty-resreq filtered here only, pending tasks missing from SubJobs maps with fallback
		// ordering in allocate, or subjob index skew). Model those paths before returning a plan.
		discardChain()
		return nil, nil, false
	}

	subJobHyperNodes := make(map[api.SubJobID]string)
	jobPipelinedAfterLastSubJob := false

	for !worksheet.subJobs.Empty() {
		subJob := worksheet.subJobs.Pop().(*api.SubJobInfo)
		sjWS := worksheet.subJobWorksheets[subJob.UID]
		if sjWS == nil || sjWS.tasks.Empty() {
			continue
		}

		var winnerStmt *framework.Statement
		var winnerHN string
		if job.ContainsHardTopology() {
			gradients := ssn.HyperNodeGradientForSubJobFn(subJob, jobDomainHyperNode, api.PurposeEvict)
		gradientSearch:
			for _, row := range gradients {
				for _, hn := range row {
					if hn == nil {
						continue
					}
					if trial, ok := runSimulateTrialAtHyperNode(ssn, queue, job, subJob, sjWS, hn.Name, victimsPresent, enablePredCache); ok {
						winnerStmt = trial
						winnerHN = hn.Name
						break gradientSearch
					}
				}
			}
		} else {
			hn := hyperNodeKeyForFlat(jobDomainHyperNode)
			if trial, ok := runSimulateTrialAtHyperNode(ssn, queue, job, subJob, sjWS, hn, victimsPresent, enablePredCache); ok {
				winnerStmt = trial
				winnerHN = hn
			}
		}
		if winnerStmt == nil {
			discardChain()
			return nil, nil, false
		}

		chain = append(chain, winnerStmt)
		subJobHyperNodes[subJob.UID] = winnerHN
		jobPipelinedAfterLastSubJob = ssn.JobPipelined(job)

		// Stop once the cumulative chain satisfies the job.
		//   jobWasReady:                 job already JobReady; caller only requested one sub-job's pending placement.
		//   jobPipelinedAfterLastSubJob: starting-up job; cumulative chain now flips JobPipelined.
		if jobWasReady || jobPipelinedAfterLastSubJob {
			break
		}
	}

	if !jobPipelinedAfterLastSubJob {
		discardChain()
		return nil, nil, false
	}
	// Even on success, the chain is discarded so session is clean for the caller. SaveOperations
	// clones the ops into the returned plan first; callers replay via RecoverOperations to
	// actually commit (or ignore the plan, which is safe since session was already cleaned here).
	plan := framework.SaveOperations(chain...)
	discardChain()
	return plan, subJobHyperNodes, true
}

func hyperNodeKeyForFlat(jobHN *api.HyperNodeInfo) string {
	if jobHN != nil {
		return jobHN.Name
	}
	return framework.ClusterTopHyperNode
}

type evictionWorksheet struct {
	subJobs          *util.PriorityQueue
	subJobWorksheets map[api.SubJobID]*evictionSubWorksheet
}

type evictionSubWorksheet struct {
	tasks *util.PriorityQueue
}

func (w *evictionWorksheet) Empty() bool {
	return w.subJobs == nil || w.subJobs.Empty()
}

// organizeEvictionWorksheet mirrors allocate.organizeJobWorksheet ordering and MinSubJobs gating.
func organizeEvictionWorksheet(ssn *framework.Session, job *api.JobInfo) *evictionWorksheet {
	subJobs := make([]*api.SubJobInfo, 0, len(job.SubJobs))
	subJobCountMap := map[api.SubJobGID]int32{}
	for _, subJob := range job.SubJobs {
		if ssn.SubJobReady(job, subJob) {
			subJobCountMap[subJob.GID]++
		} else {
			subJobs = append(subJobs, subJob)
		}
	}
	slices.SortFunc(subJobs, func(l, r *api.SubJobInfo) int {
		if !ssn.SubJobOrderFn(l, r) {
			return 1
		}
		return -1
	})
	requireSubJobs := sets.Set[api.SubJobID]{}
	for _, subJob := range subJobs {
		if subJobCountMap[subJob.GID] < job.MinSubJobs[subJob.GID] {
			requireSubJobs.Insert(subJob.UID)
			subJobCountMap[subJob.GID]++
		}
	}
	jWorksheet := &evictionWorksheet{
		subJobs: util.NewPriorityQueue(func(l, r interface{}) bool {
			lv := l.(*api.SubJobInfo)
			rv := r.(*api.SubJobInfo)
			lreq := requireSubJobs.Has(lv.UID)
			rreq := requireSubJobs.Has(rv.UID)
			if lreq != rreq {
				return lreq
			}
			return ssn.SubJobOrderFn(l, r)
		}),
		subJobWorksheets: make(map[api.SubJobID]*evictionSubWorksheet),
	}

	for subJobID, subJob := range job.SubJobs {
		sjWorksheet := &evictionSubWorksheet{
			tasks: util.NewPriorityQueue(ssn.TaskOrderFn),
		}
		for _, task := range subJob.TaskStatusIndex[api.Pending] {
			if task.SchGated || task.Resreq.IsEmpty() {
				continue
			}
			sjWorksheet.tasks.Push(task)
		}
		if !sjWorksheet.tasks.Empty() {
			jWorksheet.subJobs.Push(subJob)
			jWorksheet.subJobWorksheets[subJobID] = sjWorksheet
		}
	}
	return jWorksheet
}

// runSimulateTrialAtHyperNode pipelines a sub-job's pending tasks onto nodes within the given
// hyperNode in a fresh Statement. On success returns that Statement with its ops still applied
// to session; the caller owns it and must keep, Discard, or Commit it. On failure session is
// left unchanged.
func runSimulateTrialAtHyperNode(
	ssn *framework.Session,
	queue *api.QueueInfo,
	job *api.JobInfo,
	subJob *api.SubJobInfo,
	sjWS *evictionSubWorksheet,
	hyperNodeKey string,
	victimsPresent bool,
	enablePredCache bool,
) (*framework.Statement, bool) {
	candidate := ssn.RealNodesList[hyperNodeKey]
	if len(candidate) == 0 {
		return nil, false
	}

	trial := framework.NewStatement(ssn)
	success := false
	defer func() {
		if !success {
			trial.Discard()
		}
	}()

	tasks := sjWS.tasks.Clone()
	ph := util.NewPredicateHelper()
	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)
		if queue != nil && !ssn.Allocatable(queue, task) {
			return nil, false
		}
		schedulable := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
		nodes := intersectNodesByName(candidate, schedulable)
		if len(nodes) == 0 {
			return nil, false
		}
		if err := ssn.PrePredicateFn(task); err != nil {
			return nil, false
		}
		predicateNodes, _ := ph.PredicateNodes(task, nodes, func(t *api.TaskInfo, n *api.NodeInfo) error {
			return simulatePredicate(ssn, t, n)
		}, enablePredCache, ssn.NodesInShard)
		if len(predicateNodes) == 0 {
			return nil, false
		}
		bestNode := prioritizeNodesForSimulate(ssn, task, predicateNodes)
		if bestNode == nil {
			return nil, false
		}
		if !task.InitResreq.LessEqual(bestNode.FutureIdle(), api.Zero) {
			return nil, false
		}
		if err := trial.Pipeline(task, bestNode.Name, victimsPresent); err != nil {
			klog.ErrorS(err, "Simulate pipeline failed", "task", task.Name)
			_ = trial.UnPipeline(task)
			return nil, false
		}
	}

	if len(trial.Operations()) == 0 {
		return nil, false
	}
	if !ssn.SubJobReady(job, subJob) && !ssn.SubJobPipelined(job, subJob) {
		return nil, false
	}
	success = true
	return trial, true
}

func simulatePredicate(ssn *framework.Session, task *api.TaskInfo, node *api.NodeInfo) error {
	if ok, resources := task.InitResreq.LessEqualWithResourcesName(node.FutureIdle(), api.Zero); !ok {
		var statusSets api.StatusSets
		statusSets = append(statusSets, &api.Status{Code: api.Unschedulable, Reason: api.WrapInsufficientResourceReason(resources)})
		return api.NewFitErrWithStatus(task, node, statusSets...)
	}
	return ssn.PredicateForPreemptAction(task, node)
}

// intersectNodesByName returns nodes from primary whose names appear in filter (legacy reclaim/preempt
// intersect global schedulable nodes with a domain-specific list). When filter is empty, returns primary
// (e.g. session has no NodeList yet or FilterOut returned no restriction).
func intersectNodesByName(primary, filter []*api.NodeInfo) []*api.NodeInfo {
	if len(primary) == 0 {
		return nil
	}
	if len(filter) == 0 {
		return primary
	}
	allow := make(map[string]struct{}, len(filter))
	for _, n := range filter {
		if n != nil {
			allow[n.Name] = struct{}{}
		}
	}
	out := make([]*api.NodeInfo, 0, len(primary))
	for _, n := range primary {
		if n != nil {
			if _, ok := allow[n.Name]; ok {
				out = append(out, n)
			}
		}
	}
	return out
}

// prioritizeNodesForSimulate mirrors allocate.prioritizeNodes (idle / future-idle gradients and soft sharding).
func prioritizeNodesForSimulate(ssn *framework.Session, task *api.TaskInfo, predicateNodes []*api.NodeInfo) *api.NodeInfo {
	shardingMode := options.ServerOpts.ShardingMode
	var idleCandidateNodes []*api.NodeInfo
	var futureIdleCandidateNodes []*api.NodeInfo
	var idleCandidateNodesInOtherShards []*api.NodeInfo
	var futureIdleCandidateNodesInOtherShards []*api.NodeInfo
	for _, n := range predicateNodes {
		if task.InitResreq.LessEqual(n.Idle, api.Zero) {
			if shardingMode == commonutil.SoftShardingMode && !ssn.NodesInShard.Has(n.Name) {
				idleCandidateNodesInOtherShards = append(idleCandidateNodesInOtherShards, n)
			} else {
				idleCandidateNodes = append(idleCandidateNodes, n)
			}
		} else if task.InitResreq.LessEqual(n.FutureIdle(), api.Zero) {
			if shardingMode == commonutil.SoftShardingMode && !ssn.NodesInShard.Has(n.Name) {
				futureIdleCandidateNodesInOtherShards = append(futureIdleCandidateNodesInOtherShards, n)
			} else {
				futureIdleCandidateNodes = append(futureIdleCandidateNodes, n)
			}
		}
	}
	candidateNodes := [][]*api.NodeInfo{
		idleCandidateNodes,
		idleCandidateNodesInOtherShards,
		futureIdleCandidateNodes,
		futureIdleCandidateNodesInOtherShards,
	}
	var bestNode *api.NodeInfo
	for _, nodes := range candidateNodes {
		if len(nodes) == 0 {
			continue
		}
		switch {
		case len(nodes) == 1:
			bestNode = nodes[0]
		default:
			nodeScores := util.PrioritizeNodes(task, nodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)
			bestNode = ssn.BestNodeFn(task, nodeScores)
			if bestNode == nil {
				bestNode, _ = util.SelectBestNodeAndScore(nodeScores)
			}
		}
		if bestNode != nil {
			break
		}
	}
	return bestNode
}

// CollectPendingTasksForGangEviction returns pending tasks for gang preempt/reclaim (demand + policyTask).
// If JobReady: first worksheet subJob with pending; else: all worksheet pending; nil if none.
func CollectPendingTasksForGangEviction(ssn *framework.Session, job *api.JobInfo, subJobLessFn func(l, r interface{}) bool, taskLessFn api.LessFn) []*api.TaskInfo {
	if ssn == nil || job == nil {
		return nil
	}
	if ssn.JobReady(job) {
		return collectPendingTasksForRunningJob(ssn, job)
	}
	return collectPendingTasksForJobStartup(ssn, job)
}

func collectPendingTasksForJobStartup(ssn *framework.Session, job *api.JobInfo) []*api.TaskInfo {
	ws := organizeEvictionWorksheet(ssn, job)
	if ws.Empty() {
		return nil
	}
	out := make([]*api.TaskInfo, 0)
	q := ws.subJobs.Clone()
	for !q.Empty() {
		sj := q.Pop().(*api.SubJobInfo)
		sjWS := ws.subJobWorksheets[sj.UID]
		if sjWS == nil {
			continue
		}
		tq := sjWS.tasks.Clone()
		for !tq.Empty() {
			out = append(out, tq.Pop().(*api.TaskInfo))
		}
	}
	return out
}

func collectPendingTasksForRunningJob(ssn *framework.Session, job *api.JobInfo) []*api.TaskInfo {
	ws := organizeEvictionWorksheet(ssn, job)
	if ws.Empty() {
		return nil
	}
	q := ws.subJobs.Clone()
	for !q.Empty() {
		sj := q.Pop().(*api.SubJobInfo)
		sjWS := ws.subJobWorksheets[sj.UID]
		if sjWS == nil || sjWS.tasks.Empty() {
			continue
		}
		tq := sjWS.tasks.Clone()
		out := make([]*api.TaskInfo, 0, tq.Len())
		for !tq.Empty() {
			out = append(out, tq.Pop().(*api.TaskInfo))
		}
		return out
	}
	return nil
}

func collectPendingTasks(job *api.JobInfo, subJobLessFn func(l, r interface{}) bool, taskLessFn api.LessFn) []*api.TaskInfo {
	tasks := make([]*api.TaskInfo, 0, len(job.TaskStatusIndex[api.Pending]))
	seen := make(map[api.TaskID]struct{}, len(job.TaskStatusIndex[api.Pending]))
	for _, task := range collectSubJobOrderedPendingTasks(job, subJobLessFn, taskLessFn) {
		if _, ok := seen[task.UID]; ok {
			continue
		}
		seen[task.UID] = struct{}{}
		tasks = append(tasks, task)
	}
	fallback := make([]*api.TaskInfo, 0)
	for _, task := range job.TaskStatusIndex[api.Pending] {
		if task.SchGated || task.Resreq.IsEmpty() {
			continue
		}
		if _, ok := seen[task.UID]; ok {
			continue
		}
		fallback = append(fallback, task)
	}
	sortPendingByLessFn(fallback, taskLessFn)
	for _, task := range fallback {
		seen[task.UID] = struct{}{}
		tasks = append(tasks, task)
	}
	return tasks
}

func sortPendingByLessFn(fallback []*api.TaskInfo, taskLessFn api.LessFn) {
	slices.SortStableFunc(fallback, func(a, b *api.TaskInfo) int {
		if taskLessFn != nil {
			if taskLessFn(a, b) {
				return -1
			}
			if taskLessFn(b, a) {
				return 1
			}
			return 0
		}
		if a.UID < b.UID {
			return -1
		}
		if a.UID > b.UID {
			return 1
		}
		return 0
	})
}

func collectSubJobOrderedPendingTasks(job *api.JobInfo, subJobLessFn func(l, r interface{}) bool, taskLessFn api.LessFn) []*api.TaskInfo {
	if len(job.SubJobs) == 0 {
		return nil
	}
	subJobs := make([]*api.SubJobInfo, 0, len(job.SubJobs))
	for _, subJob := range job.SubJobs {
		subJobs = append(subJobs, subJob)
	}
	slices.SortStableFunc(subJobs, func(a, b *api.SubJobInfo) int {
		if subJobLessFn != nil {
			if subJobLessFn(a, b) {
				return -1
			}
			if subJobLessFn(b, a) {
				return 1
			}
			return 0
		}
		if a.MatchIndex != b.MatchIndex {
			if a.MatchIndex < b.MatchIndex {
				return -1
			}
			return 1
		}
		if a.UID < b.UID {
			return -1
		}
		if a.UID > b.UID {
			return 1
		}
		return 0
	})

	ordered := make([]*api.TaskInfo, 0)
	for _, subJob := range subJobs {
		pending := make([]*api.TaskInfo, 0, len(subJob.TaskStatusIndex[api.Pending]))
		for _, task := range subJob.TaskStatusIndex[api.Pending] {
			if task.SchGated || task.Resreq.IsEmpty() {
				continue
			}
			pending = append(pending, task)
		}
		sortPendingByLessFn(pending, taskLessFn)
		ordered = append(ordered, pending...)
	}
	return ordered
}
