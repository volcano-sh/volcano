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

// BuildNominationPlanInDomain performs a dry-run in the given domain and returns
// a statement plan only when the target job reaches JobPipelined.
//
// jobDomainHyperNode is the job-level hypernode context for this eviction domain (same role as
// hyperNodeForJob in allocate's allocateForSubJob). Callers must pass a non-nil hypernode whose
// Name keys ssn.RealNodesList for the node universe used in simulation.
func BuildNominationPlanInDomain(ssn *framework.Session, queue *api.QueueInfo, job *api.JobInfo, jobDomainHyperNode *api.HyperNodeInfo, victims []*api.TaskInfo, reason string) (*framework.Statement, bool) {
	if ssn == nil || job == nil || jobDomainHyperNode == nil {
		return nil, false
	}
	nodesForDomain := ssn.RealNodesList[jobDomainHyperNode.Name]
	if len(nodesForDomain) == 0 {
		return nil, false
	}

	placementBefore := placementProgressCount(job)
	jobWasReady := ssn.JobReady(job)

	evictStmt := framework.NewStatement(ssn)
	for _, victim := range victims {
		evictStmt.Evict(victim, reason)
	}
	cumulativePlan := framework.SaveOperations(evictStmt)
	evictStmt.Discard()

	victimsPresent := len(victims) > 0
	enablePredCache := true
	framework.GetArgOfActionFromConf(ssn.Configurations, "allocate").GetBool(&enablePredCache, conf.EnablePredicateErrCacheKey)

	worksheet := organizeEvictionWorksheet(ssn, job)
	if worksheet.Empty() {
		// TODO: Worksheet is empty while allocate may still have eligible pending (e.g. SchGated /
		// empty-resreq filtered here only, pending tasks missing from SubJobs maps with fallback
		// ordering in allocate, or subjob index skew). Model those paths before returning a plan.
		return nil, false
	}

	for !worksheet.subJobs.Empty() {
		subJob := worksheet.subJobs.Pop().(*api.SubJobInfo)
		sjWS := worksheet.subJobWorksheets[subJob.UID]
		if sjWS == nil || sjWS.tasks.Empty() {
			continue
		}

		savedAllocHN := subJob.AllocatedHyperNode

		var winner hyperNodeSimTrial
		if job.ContainsHardTopology() {
			gradients := ssn.HyperNodeGradientForSubJobFn(subJob, jobDomainHyperNode, api.PurposeEvict)
		gradientSearch:
			for _, row := range gradients {
				for _, hn := range row {
					if hn == nil {
						continue
					}
					var ok bool
					winner, ok = runSimulateTrialAtHyperNode(ssn, cumulativePlan, queue, job, subJob, sjWS, savedAllocHN, hn.Name, victimsPresent, enablePredCache)
					if ok {
						break gradientSearch
					}
				}
			}
		} else {
			winner, _ = runSimulateTrialAtHyperNode(ssn, cumulativePlan, queue, job, subJob, sjWS, savedAllocHN, hyperNodeKeyForFlat(jobDomainHyperNode), victimsPresent, enablePredCache)
		}
		if winner.plan == nil {
			return nil, false
		}

		cumulativePlan = winner.plan
		if ssn.HyperNodes != nil && job.ContainsHardTopology() {
			subJob.AllocatedHyperNode = ssn.HyperNodes.GetLCAHyperNode(savedAllocHN, winner.hyperNode)
		}

		if winner.remaining != nil && !winner.remaining.Empty() {
			sjWS.tasks = winner.remaining
			worksheet.subJobs.Push(subJob)
		}

		if !jobWasReady && ssn.JobPipelined(job) {
			return finalizeNominationPlan(ssn, job, cumulativePlan, placementBefore)
		}
		if jobWasReady {
			break
		}
	}

	return finalizeNominationPlan(ssn, job, cumulativePlan, placementBefore)
}

func placementProgressCount(job *api.JobInfo) int {
	if job == nil {
		return 0
	}
	return len(job.TaskStatusIndex[api.Pipelined]) + len(job.TaskStatusIndex[api.Binding]) + len(job.TaskStatusIndex[api.Allocated])
}

func finalizeNominationPlan(ssn *framework.Session, job *api.JobInfo, cumulativePlan *framework.Statement, placementBefore int) (*framework.Statement, bool) {
	verify := framework.NewStatement(ssn)
	if err := verify.RecoverOperations(cumulativePlan); err != nil {
		verify.Discard()
		return nil, false
	}
	defer verify.Discard()
	if placementProgressCount(job) <= placementBefore {
		return nil, false
	}
	if !ssn.JobPipelined(job) {
		return nil, false
	}
	return framework.SaveOperations(verify), true
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

// hyperNodeSimTrial holds the outcome of a successful runSimulateTrialAtHyperNode attempt.
type hyperNodeSimTrial struct {
	plan      *framework.Statement
	hyperNode string
	remaining *util.PriorityQueue
}

// runSimulateTrialAtHyperNode dry-runs placing sjWS tasks onto RealNodesList[hyperNodeKey]. On success
// returns a populated hyperNodeSimTrial and true; on failure returns zero value and false. Always restores
// subJob.AllocatedHyperNode to savedAllocHN.
func runSimulateTrialAtHyperNode(
	ssn *framework.Session,
	cumulativePlan *framework.Statement,
	queue *api.QueueInfo,
	job *api.JobInfo,
	subJob *api.SubJobInfo,
	sjWS *evictionSubWorksheet,
	savedAllocHN string,
	hyperNodeKey string,
	victimsPresent bool,
	enablePredCache bool,
) (hyperNodeSimTrial, bool) {
	if len(ssn.RealNodesList[hyperNodeKey]) == 0 {
		return hyperNodeSimTrial{}, false
	}
	trial := framework.NewStatement(ssn)
	if err := trial.RecoverOperations(cumulativePlan); err != nil {
		trial.Discard()
		subJob.AllocatedHyperNode = savedAllocHN
		return hyperNodeSimTrial{}, false
	}
	tasksClone := sjWS.tasks.Clone()
	if !simulateResourcesForTasks(ssn, trial, queue, job, subJob, tasksClone, victimsPresent, hyperNodeKey, enablePredCache) {
		trial.Discard()
		subJob.AllocatedHyperNode = savedAllocHN
		return hyperNodeSimTrial{}, false
	}
	plan := framework.SaveOperations(trial)
	trial.Discard()
	subJob.AllocatedHyperNode = savedAllocHN
	return hyperNodeSimTrial{plan: plan, hyperNode: hyperNodeKey, remaining: tasksClone}, true
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

func simulateResourcesForTasks(
	ssn *framework.Session,
	stmt *framework.Statement,
	queue *api.QueueInfo,
	job *api.JobInfo,
	subJob *api.SubJobInfo,
	tasks *util.PriorityQueue,
	victimsPresent bool,
	hyperNodeName string,
	enablePredicateErrorCache bool,
) bool {
	initialStmtOps := len(stmt.Operations())
	ph := util.NewPredicateHelper()
	allocatedHyperNode := subJob.AllocatedHyperNode

	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)
		if queue != nil && !ssn.Allocatable(queue, task) {
			return false
		}

		candidate := ssn.RealNodesList[hyperNodeName]
		if len(candidate) == 0 {
			return false
		}
		schedulable := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
		nodes := intersectNodesByName(candidate, schedulable)
		if len(nodes) == 0 {
			return false
		}

		if err := ssn.PrePredicateFn(task); err != nil {
			return false
		}

		predicateNodes, _ := ph.PredicateNodes(task, nodes, func(t *api.TaskInfo, n *api.NodeInfo) error {
			return simulatePredicate(ssn, t, n)
		}, enablePredicateErrorCache, ssn.NodesInShard)
		if len(predicateNodes) == 0 {
			return false
		}

		if subJob.WithNetworkTopology() {
			task.JobAllocatedHyperNode = allocatedHyperNode
		}

		bestNode := prioritizeNodesForSimulate(ssn, task, predicateNodes)
		if bestNode == nil {
			return false
		}

		if task.InitResreq.LessEqual(bestNode.Idle, api.Zero) {
			if err := stmt.Allocate(task, bestNode); err != nil {
				klog.ErrorS(err, "Simulate allocate failed", "task", task.Name)
				_ = stmt.UnAllocate(task)
				if task.Pod != nil {
					task.Pod.Spec.NodeName = ""
				}
				return false
			}
		} else if task.InitResreq.LessEqual(bestNode.FutureIdle(), api.Zero) {
			if err := stmt.Pipeline(task, bestNode.Name, victimsPresent); err != nil {
				_ = stmt.UnPipeline(task)
				return false
			}
		} else {
			return false
		}

		if subJob.WithNetworkTopology() {
			allocatedHyperNode = getNewAllocatedHyperNodeForSimulate(ssn, bestNode.Name, allocatedHyperNode)
		}

		if ssn.SubJobReady(job, subJob) {
			break
		}
	}

	if len(stmt.Operations()) == initialStmtOps {
		return false
	}
	if ssn.SubJobReady(job, subJob) {
		if subJob.IsSoftTopologyMode() {
			subJob.AllocatedHyperNode = allocatedHyperNode
		}
		return true
	}
	return ssn.SubJobPipelined(job, subJob)
}

func getNewAllocatedHyperNodeForSimulate(ssn *framework.Session, bestNode string, jobAllocatedHyperNode string) string {
	if ssn.HyperNodes == nil {
		return jobAllocatedHyperNode
	}
	hyperNode := util.FindHyperNodeForNode(bestNode, ssn.RealNodesList, ssn.HyperNodesTiers, ssn.HyperNodesSetByTier)
	if hyperNode != "" {
		if jobAllocatedHyperNode == "" {
			return hyperNode
		}
		return ssn.HyperNodes.GetLCAHyperNode(hyperNode, jobAllocatedHyperNode)
	}
	return jobAllocatedHyperNode
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

// CollectPendingTasksForJobStartup flattens organizeEvictionWorksheet: every subJob on the worksheet (PQ
// order, including MinSubJobs boosting), each subJob’s pending tasks in TaskOrderFn order. Returns nil if the
// worksheet is empty. Intended for sizing when JobReady(job) is false.
//
// subJobLessFn and taskLessFn are unused; kept for call-site compatibility.
func CollectPendingTasksForJobStartup(ssn *framework.Session, job *api.JobInfo, subJobLessFn func(l, r interface{}) bool, taskLessFn api.LessFn) []*api.TaskInfo {
	_, _ = subJobLessFn, taskLessFn
	return collectPendingTasksForJobStartup(ssn, job)
}

// CollectPendingTasksForRunningJob returns pending tasks for the first worksheet subJob (PQ / MinSubJobs
// order) whose pending task queue is non-empty. Returns nil if the worksheet is empty or no such subJob.
// Intended for sizing when JobReady(job) is true (one starving subJob at a time).
func CollectPendingTasksForRunningJob(ssn *framework.Session, job *api.JobInfo) []*api.TaskInfo {
	return collectPendingTasksForRunningJob(ssn, job)
}

// CollectAllPendingTasksOrderedForJob returns all eligible pending tasks: subJob-ordered traversal (using
// subJobLessFn / taskLessFn when provided) plus any fallback pending pods not indexed under SubJobs.
func CollectAllPendingTasksOrderedForJob(job *api.JobInfo, subJobLessFn func(l, r interface{}) bool, taskLessFn api.LessFn) []*api.TaskInfo {
	return collectPendingTasks(job, subJobLessFn, taskLessFn)
}

// CollectPendingTasksForJob is an alias for CollectAllPendingTasksOrderedForJob.
func CollectPendingTasksForJob(job *api.JobInfo, subJobLessFn func(l, r interface{}) bool, taskLessFn api.LessFn) []*api.TaskInfo {
	return CollectAllPendingTasksOrderedForJob(job, subJobLessFn, taskLessFn)
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
