/*
 Copyright 2021 The Volcano Authors.

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

package allocate

import (
	"fmt"
	"math"
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type allocateContext struct {
	queues              *util.PriorityQueue                 // queue of *api.QueueInfo
	jobsByQueue         map[api.QueueID]*util.PriorityQueue // queue of *api.JobInfo
	jobWorksheet        map[api.JobID]*JobWorksheet
	tasksNoHardTopology map[api.JobID]*util.PriorityQueue // queue of *api.TaskInfo, job without any hard network topology policy use this queue
}

type JobWorksheet struct {
	podBunches         *util.PriorityQueue // queue of *api.PodBunchInfo
	podBunchWorksheets map[api.BunchID]*PodBunchWorksheet
}

func (w *JobWorksheet) ShallowCopyFrom(another *JobWorksheet) {
	if another == nil {
		return
	}
	w.podBunches = another.podBunches
	w.podBunchWorksheets = another.podBunchWorksheets
}

func (w *JobWorksheet) Empty() bool {
	return w.podBunches == nil || w.podBunches.Empty()
}

func (w *JobWorksheet) Clone() *JobWorksheet {
	podBunchWorksheets := make(map[api.BunchID]*PodBunchWorksheet)
	for bunchID, tasks := range w.podBunchWorksheets {
		podBunchWorksheets[bunchID] = tasks.Clone()
	}
	return &JobWorksheet{
		podBunches:         w.podBunches.Clone(),
		podBunchWorksheets: podBunchWorksheets,
	}
}

type PodBunchWorksheet struct {
	tasks *util.PriorityQueue // queue of *api.TaskInfo
}

func (w *PodBunchWorksheet) ShallowCopyFrom(another *PodBunchWorksheet) {
	if another == nil {
		return
	}
	w.tasks = another.tasks
}

func (w *PodBunchWorksheet) Empty() bool {
	return w.tasks == nil || w.tasks.Empty()
}

func (w *PodBunchWorksheet) Clone() *PodBunchWorksheet {
	return &PodBunchWorksheet{
		tasks: w.tasks.Clone(),
	}
}

type Action struct {
	session *framework.Session
	// configured flag for error cache
	enablePredicateErrorCache bool

	recorder *Recorder
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache: true, // default to enable it
	}
}

func (alloc *Action) Name() string {
	return "allocate"
}

func (alloc *Action) Initialize() {}

func (alloc *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, alloc.Name())
	arguments.GetBool(&alloc.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Allocate ...")
	defer klog.V(5).Infof("Leaving Allocate ...")

	alloc.parseArguments(ssn)

	// the allocation for pod may have many stages
	// 1. pick a queue named Q (using ssn.QueueOrderFn)
	// 2. pick a job named J from Q (using ssn.JobOrderFn)
	// 3. pick a task T from J (using ssn.TaskOrderFn)
	// 4. use predicateFn to filter out node that T can not be allocated on.
	// 5. use ssn.NodeOrderFn to judge the best node and assign it to T

	alloc.session = ssn
	alloc.recorder = NewRecorder()
	actx := alloc.buildAllocateContext()
	klog.V(3).Infof("Try to allocate resource to %d Queues", actx.queues.Len())
	alloc.allocateResources(actx)
}

func (alloc *Action) buildAllocateContext() *allocateContext {
	ssn := alloc.session

	actx := &allocateContext{
		queues:              util.NewPriorityQueue(ssn.QueueOrderFn), // queues sort queues by QueueOrderFn.
		jobsByQueue:         make(map[api.QueueID]*util.PriorityQueue),
		jobWorksheet:        make(map[api.JobID]*JobWorksheet),
		tasksNoHardTopology: make(map[api.JobID]*util.PriorityQueue),
	}

	for _, job := range ssn.Jobs {
		// If not config enqueue action, change Pending pg into Inqueue state to avoid blocking job scheduling.
		if job.IsPending() {
			if conf.EnabledActionMap["enqueue"] {
				klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: job status is pending.",
					job.Namespace, job.Name, job.Queue)
				continue
			} else {
				klog.V(4).Infof("Job <%s/%s> Queue <%s> status update from pending to inqueue, reason: no enqueue action is configured.",
					job.Namespace, job.Name, job.Queue)
				job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			}
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if _, found := ssn.Queues[job.Queue]; !found {
			klog.Warningf("Skip adding Job <%s/%s> because its queue %s is not found",
				job.Namespace, job.Name, job.Queue)
			continue
		}

		if !ssn.HyperNodesReadyToSchedule && job.ContainsNetworkTopology() {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: hyperNodes are not ready for scheduling",
				job.Namespace, job.Name, job.Queue)
			continue
		}

		worksheet := alloc.organizeJobWorksheet(job)
		if worksheet.Empty() {
			continue
		}

		if _, found := actx.jobsByQueue[job.Queue]; !found {
			actx.jobsByQueue[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			actx.queues.Push(ssn.Queues[job.Queue])
		}

		klog.V(4).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
		actx.jobsByQueue[job.Queue].Push(job)
		actx.jobWorksheet[job.UID] = worksheet

		// job without any hard network topology policy use actx.tasksNoHardTopology
		if !job.ContainsHardTopology() {
			if podbunchWorksheet, exist := worksheet.podBunchWorksheets[job.DefaultPodBunchID()]; exist {
				actx.tasksNoHardTopology[job.UID] = podbunchWorksheet.tasks
			}
		}
	}

	return actx
}

func (alloc *Action) organizeJobWorksheet(job *api.JobInfo) *JobWorksheet {
	ssn := alloc.session

	jWorksheet := &JobWorksheet{
		podBunches:         util.NewPriorityQueue(ssn.PodBunchOrderFn),
		podBunchWorksheets: make(map[api.BunchID]*PodBunchWorksheet),
	}

	for bunchID, podBunch := range job.PodBunches {
		pbWorksheet := &PodBunchWorksheet{
			tasks: util.NewPriorityQueue(ssn.TaskOrderFn),
		}

		for _, task := range podBunch.TaskStatusIndex[api.Pending] {
			// Skip tasks whose pod are scheduling gated
			if task.SchGated {
				continue
			}

			// Skip BestEffort task in 'allocate' action.
			if task.Resreq.IsEmpty() {
				klog.V(4).Infof("Task <%v/%v> is BestEffort task, skip it.",
					task.Namespace, task.Name)
				continue
			}
			pbWorksheet.tasks.Push(task)
		}

		if !pbWorksheet.Empty() {
			jWorksheet.podBunches.Push(podBunch)
			jWorksheet.podBunchWorksheets[bunchID] = pbWorksheet
		}
	}

	return jWorksheet
}

func (alloc *Action) allocateResources(actx *allocateContext) {
	ssn := alloc.session

	queues := actx.queues
	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		jobs, found := actx.jobsByQueue[queue.UID]
		if !found || jobs.Empty() {
			klog.V(4).Infof("Can not find jobs for queue %s.", queue.Name)
			continue
		}

		job := jobs.Pop().(*api.JobInfo)

		if job.ContainsHardTopology() {
			jobWorksheet := actx.jobWorksheet[job.UID]

			klog.V(3).InfoS("Try to allocate resource for job contains hard topology", "queue", queue.Name, "job", job.UID,
				"allocatedHyperNode", job.AllocatedHyperNode, "podBunchNum", jobWorksheet.podBunches.Len())
			stmt := alloc.allocateForJob(job, jobWorksheet, ssn.HyperNodes[framework.ClusterTopHyperNode])
			if stmt != nil && ssn.JobReady(job) { // do not commit stmt when job is pipelined
				stmt.Commit()
				ssn.MarkJobDirty(job.UID)
				alloc.recorder.UpdateDecisionToJob(job, ssn.HyperNodes)

				// There are still left tasks that need to be allocated when min available < replicas, put the job back
				if !jobWorksheet.Empty() {
					jobs.Push(job)
				}
			}
		} else {
			podBunch, pbExist := job.PodBunches[job.DefaultPodBunchID()]
			tasks, tasksExist := actx.tasksNoHardTopology[job.UID]
			if pbExist && tasksExist {
				klog.V(3).InfoS("Try to allocate resource", "queue", queue.Name, "job", job.UID, "taskNum", tasks.Len())
				stmt := alloc.allocateResourcesForTasks(podBunch, tasks, framework.ClusterTopHyperNode)
				if stmt != nil && ssn.JobReady(job) { // do not commit stmt when job is pipelined
					stmt.Commit()

					// There are still left tasks that need to be allocated when min available < replicas, put the job back
					if tasks.Len() > 0 {
						jobs.Push(job)
					}
				}
			} else {
				klog.ErrorS(nil, "Can not find default podBunch or tasks for job", "job", job.UID,
					"podBunchExist", pbExist, "tasksExist", tasksExist)
			}
		}

		// Put back the queue to priority queue after job's resource allocating finished,
		// To ensure that the priority of the queue is calculated based on the latest resource allocation situation.
		queues.Push(queue)
	}
}

func (alloc *Action) allocateForJob(job *api.JobInfo, jobWorksheet *JobWorksheet, hyperNodeToAllocate *api.HyperNodeInfo) *framework.Statement {
	ssn := alloc.session

	if jobWorksheet == nil || jobWorksheet.Empty() {
		klog.V(4).InfoS("Empty job worksheet", "job", job.UID)
		return nil
	}

	alloc.recorder.SnapshotPodBunchStatus(job, jobWorksheet)

	hyperNodeGradients := ssn.HyperNodeGradientForJobFn(job, hyperNodeToAllocate)
	for gradient, hyperNodes := range hyperNodeGradients {
		stmtBackup := make(map[string]*framework.Statement)    // backup the statement after the job is allocated to a hyperNode
		jobWorksheetsBackup := make(map[string]*JobWorksheet)  // backup the job worksheet after the job is allocated to a hyperNode
		podBunchesAllocationScores := make(map[string]float64) // save the podBunches allocation score of the job allocated to a hyperNode

		for _, hyperNode := range hyperNodes {
			var stmtList []*framework.Statement
			var podBunchesAllocationScore float64

			// Clone jobWorksheet and rest job's fit err to make sure it's a clean cache when everytime filter a hyperNode and do not affect each other between hyperNodes.
			job.ResetFitErr()
			jobWorksheetCopy := jobWorksheet.Clone()
			klog.V(3).InfoS("Try to allocate resource for job in hyperNode", "job", job.UID, "hyperNode", hyperNode.Name)

			for !jobWorksheetCopy.podBunches.Empty() {
				podBunch := jobWorksheetCopy.podBunches.Pop().(*api.PodBunchInfo)
				bunchWorksheet := jobWorksheetCopy.podBunchWorksheets[podBunch.UID]

				stmt, allocationScore := alloc.allocateForPodBunch(podBunch, bunchWorksheet, hyperNode)

				if stmt != nil && len(stmt.Operations()) > 0 {
					stmtList = append(stmtList, stmt)
					podBunchesAllocationScore += allocationScore
					// push back when podBunch is ready and remain pending task
					if !bunchWorksheet.Empty() {
						jobWorksheetCopy.podBunches.Push(podBunch)
					}

					if ssn.JobReady(job) {
						break
					}
				}
			}
			// reset the podBunches to initial status
			alloc.recorder.RecoverPodBunchStatus(job)

			mergedStmt := framework.SaveOperations(stmtList...)
			if len(mergedStmt.Operations()) == 0 {
				continue // skip recording this empty solution
			}
			if ssn.JobReady(job) || ssn.JobPipelined(job) {
				stmtBackup[hyperNode.Name] = mergedStmt                                // backup successful solution
				jobWorksheetsBackup[hyperNode.Name] = jobWorksheetCopy                 // backup remains podBunches
				podBunchesAllocationScores[hyperNode.Name] = podBunchesAllocationScore // save the podBunches allocation score of the job
			}

			// dry run in every hyperNode
			for _, stmt := range stmtList {
				stmt.Discard()
			}
		}

		if len(podBunchesAllocationScores) == 0 {
			klog.V(5).InfoS("Find solution for job fail", "job", job.UID, "gradient", gradient)
			continue // try next gradient
		}

		bestHyperNode, err := alloc.selectBestHyperNodeForJob(podBunchesAllocationScores, job)
		if err != nil {
			klog.ErrorS(err, "Cannot find best hyper node for job", "job", job.UID, "gradient", gradient)
			return nil
		}

		// recover the stmt
		bestStmt := stmtBackup[bestHyperNode]
		finalStmt := framework.NewStatement(ssn)
		if err = finalStmt.RecoverOperations(bestStmt); err != nil {
			klog.ErrorS(err, "Failed to recover operations", "job", job.UID, "hyperNode", bestHyperNode)
			return nil
		}

		// inherit the remains worksheet after allocate to the best hyperNode
		jobWorksheet.ShallowCopyFrom(jobWorksheetsBackup[bestHyperNode])

		alloc.recorder.SaveJobDecision(job.UID, bestHyperNode)
		klog.V(3).InfoS("Allocate job to hyperNode success", "job", job.UID, "hyperNode", bestHyperNode)

		return finalStmt
	}

	klog.V(5).InfoS("Cannot find any solution for job", "job", job.UID)
	return nil
}

func (alloc *Action) allocateForPodBunch(podBunch *api.PodBunchInfo, podBunchWorksheet *PodBunchWorksheet, hyperNodeForJob *api.HyperNodeInfo) (*framework.Statement, float64) {
	ssn := alloc.session
	job := ssn.Jobs[podBunch.Job]

	if podBunchWorksheet == nil || podBunchWorksheet.Empty() {
		klog.V(4).InfoS("Empty podBunch worksheet", "job", podBunch.Job, "podBunch", podBunch.UID)
		return nil, 0
	}

	klog.V(3).InfoS("Try to allocate resource for podBunch", "job", podBunch.Job,
		"podBunch", podBunch.UID, "allocatedHyperNode", podBunch.AllocatedHyperNode, "taskNum", podBunchWorksheet.tasks.Len())

	hyperNodeGradients := ssn.HyperNodeGradientForPodBunchFn(podBunch, hyperNodeForJob)
	for gradient, hyperNodes := range hyperNodeGradients {
		stmtBackup := make(map[string]*framework.Statement)             // backup the statement after the podBunch is allocated to a hyperNode
		podBunchWorksheetsBackup := make(map[string]*PodBunchWorksheet) // backup the podBunch worksheet after the podBunch is allocated to a hyperNode

		for _, hyperNode := range hyperNodes {
			// Clone podBunchWorksheet and rest podBunch's fit err to make sure it's a clean cache when everytime filter a hyperNode and do not affect each other between hyperNodes.
			job.ResetPodBunchFitErr(podBunch.UID)
			podBunchWorksheetCopy := podBunchWorksheet.Clone()

			klog.V(3).InfoS("Try to allocate resource for tasks in podBunch", "job", podBunch.Job,
				"podBunch", podBunch.UID, "taskNum", podBunchWorksheetCopy.tasks.Len(), "hyperNode", hyperNode.Name)
			stmt := alloc.allocateResourcesForTasks(podBunch, podBunchWorksheetCopy.tasks, hyperNode.Name)

			if stmt != nil && len(stmt.Operations()) > 0 {
				stmtBackup[hyperNode.Name] = framework.SaveOperations(stmt)      // backup successful solution
				podBunchWorksheetsBackup[hyperNode.Name] = podBunchWorksheetCopy // backup remains tasks
				stmt.Discard()                                                   // dry run in every hyperNode
			}
		}

		if len(stmtBackup) == 0 {
			klog.V(5).InfoS("Find solution for podBunch fail", "podBunch", podBunch.UID, "gradient", gradient)
			continue // try next gradient
		}

		// select the best solution
		bestHyperNode, bestScore, err := alloc.selectBestHyperNodeForPodBunch(stmtBackup, podBunch)
		if err != nil {
			klog.ErrorS(err, "Cannot find best hyper node for podBunch", "podBunch", podBunch.UID, "gradient", gradient)
			return nil, 0
		}

		// recover the stmt and update podBunch's allocatedHyperNode field
		bestStmt := stmtBackup[bestHyperNode]
		finalStmt := framework.NewStatement(ssn)
		if err = finalStmt.RecoverOperations(bestStmt); err != nil {
			klog.ErrorS(err, "Failed to recover operations", "podBunch", podBunch.UID, "hyperNode", bestHyperNode)
			return nil, 0
		}
		newAllocatedHyperNode := ssn.HyperNodes.GetLCAHyperNode(podBunch.AllocatedHyperNode, bestHyperNode)
		podBunch.AllocatedHyperNode = newAllocatedHyperNode

		// inherit the remains worksheet after allocate to the best hyperNode
		podBunchWorksheet.ShallowCopyFrom(podBunchWorksheetsBackup[bestHyperNode])

		alloc.recorder.SavePodBunchDecision(podBunch.Job, hyperNodeForJob.Name, podBunch.UID, newAllocatedHyperNode)
		klog.V(3).InfoS("Allocate podBunch to hyperNode success", "podBunch", podBunch.UID,
			"hyperNode", bestHyperNode, "score", bestScore, "newAllocatedHyperNode", newAllocatedHyperNode)

		return finalStmt, bestScore
	}

	klog.V(5).InfoS("Cannot find any solution for podBunch", "podBunch", podBunch.UID)
	return nil, 0
}

// selectBestHyperNodeForJob return the best hyperNode for the job,
// it will score and select the best hyperNode among all available hyperNodes.
func (alloc *Action) selectBestHyperNodeForJob(podBunchesAllocationScores map[string]float64, job *api.JobInfo) (string, error) {
	highestScore := math.Inf(-1)
	bestHyperNode := ""
	for hyperNode, score := range podBunchesAllocationScores {
		if score > highestScore {
			highestScore = score
			bestHyperNode = hyperNode
		}
	}

	if bestHyperNode == "" {
		return "", fmt.Errorf("no solution found for job %s", job.UID)
	}

	return bestHyperNode, nil
}

// selectBestHyperNodeForPodBunch return the best hyperNode for the podBunch,
// it will score and select the best hyperNode among all available hyperNodes.
func (alloc *Action) selectBestHyperNodeForPodBunch(stmts map[string]*framework.Statement, podBunch *api.PodBunchInfo) (string, float64, error) {
	if len(stmts) <= 0 {
		return "", 0, fmt.Errorf("no solution found for podBunch %s", podBunch.UID)
	}

	ssn := alloc.session
	candidateHyperNodeGroups := make(map[string][]*api.NodeInfo)
	for hyperNode := range stmts {
		candidateHyperNodeGroups[hyperNode] = ssn.RealNodesList[hyperNode]
	}

	hyperNodeScores, err := util.PrioritizeHyperNodes(candidateHyperNodeGroups, podBunch, ssn.HyperNodeOrderMapFn)
	if err != nil {
		return "", 0, fmt.Errorf("prioritize hyperNodes for podBunch %s fail: %w", podBunch.UID, err)
	}

	bestHyperNode, bestScore := util.SelectBestHyperNodeAndScore(hyperNodeScores)
	if bestHyperNode == "" {
		return "", 0, fmt.Errorf("cannot find best hyperNode for podBunch %s", podBunch.UID)
	}
	return bestHyperNode, bestScore, nil
}

func (alloc *Action) allocateResourcesForTasks(podBunch *api.PodBunchInfo, tasks *util.PriorityQueue, hyperNode string) *framework.Statement {
	ssn := alloc.session

	job := ssn.Jobs[podBunch.Job]
	queue := ssn.Queues[job.Queue]
	nodes, exist := ssn.RealNodesList[hyperNode]
	if !exist || len(nodes) == 0 {
		klog.V(4).InfoS("There is no node in hyperNode", "job", job.UID, "hyperNode", hyperNode)
		return nil
	}

	stmt := framework.NewStatement(ssn)
	ph := util.NewPredicateHelper()

	allocatedHyperNode := podBunch.AllocatedHyperNode

	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)
		if !ssn.Allocatable(queue, task) {
			klog.V(3).Infof("Queue <%s> is overused when considering task <%s>, ignore it.", queue.Name, task.Name)
			continue
		}

		// check if the task with its spec has already predicates failed
		if job.TaskHasFitErrors(podBunch.UID, task) {
			msg := fmt.Sprintf("Task %s with role spec %s has already predicated failed, skip", task.Name, task.TaskRole)
			klog.V(5).Info(msg)
			fitErrors := api.NewFitErrors()
			fitErrors.SetError(msg)
			job.NodesFitErrors[task.UID] = fitErrors
			continue
		}

		klog.V(3).Infof("There are <%d> nodes for Job <%v/%v>", len(nodes), job.Namespace, job.Name)

		if err := ssn.PrePredicateFn(task); err != nil {
			klog.V(3).Infof("PrePredicate for task %s/%s failed for: %v", task.Namespace, task.Name, err)
			fitErrors := api.NewFitErrors()
			for _, ni := range nodes {
				fitErrors.SetNodeError(ni.Name, err)
			}
			job.NodesFitErrors[task.UID] = fitErrors
			break
		}

		var predicateNodes []*api.NodeInfo
		var fitErrors *api.FitErrors

		// "NominatedNodeName" can potentially be set in a previous scheduling cycle as a result of preemption.
		// This node is likely the only candidate that will fit the pod, and hence we try it first before iterating over all nodes.
		if len(task.Pod.Status.NominatedNodeName) > 0 {
			if nominatedNodeInfo, ok := ssn.Nodes[task.Pod.Status.NominatedNodeName]; ok && task.InitResreq.LessEqual(nominatedNodeInfo.FutureIdle(), api.Zero) {
				predicateNodes, fitErrors = ph.PredicateNodes(task, []*api.NodeInfo{nominatedNodeInfo}, alloc.predicate, alloc.enablePredicateErrorCache)
			}
		}

		// If the nominated node is not found or the nominated node is not suitable for the task, we need to find a suitable node for the task from all nodes.
		if len(predicateNodes) == 0 {
			predicateNodes, fitErrors = ph.PredicateNodes(task, nodes, alloc.predicate, alloc.enablePredicateErrorCache)
		}

		if len(predicateNodes) == 0 {
			// TODO: Need to add PostFilter extension point implementation here. For example, the DRA plugin includes the PostFilter extension point,
			// but the DRA's PostFilter only occurs in extreme error conditions: Suppose a pod uses two claims. In the first scheduling attempt,
			// a node is picked and PreBind manages to update the first claim so that it is allocated and reserved for the pod.
			// But then updating the second claim fails (e.g., apiserver down) and the scheduler has to retry. During the next pod scheduling attempt,
			// the original node is no longer usable for other reasons. Other nodes are not usable either because of the allocated claim.
			// The DRA scheduler plugin detects that and then when scheduling fails (= no node passed filtering), it recovers by de-allocating the allocated claim in PostFilter.
			if fitErrors != nil && hyperNode != framework.ClusterTopHyperNode {
				fitErrors.SetHyperNode(hyperNode)
			}
			job.NodesFitErrors[task.UID] = fitErrors
			// Assume that all left tasks are allocatable, but can not meet gang-scheduling min member,
			// so we should break from continuously allocating.
			// otherwise, should continue to find other allocatable task
			if job.NeedContinueAllocating(podBunch.UID) {
				continue
			} else {
				break
			}
		}

		if podBunch.WithNetworkTopology() {
			task.JobAllocatedHyperNode = allocatedHyperNode
		}

		bestNode, _ := alloc.prioritizeNodes(ssn, task, predicateNodes)
		if bestNode == nil {
			continue
		}

		if err := alloc.allocateResourcesForTask(stmt, task, bestNode, job); err != nil {
			klog.ErrorS(err, "Allocate resources for task fail", "task", task.Name)
			continue
		}

		if podBunch.WithNetworkTopology() {
			allocatedHyperNode = getNewAllocatedHyperNode(ssn, bestNode.Name, allocatedHyperNode)
		}

		if ssn.PodBunchReady(job, podBunch) {
			break
		}
	}

	if ssn.PodBunchReady(job, podBunch) {
		klog.V(3).InfoS("PodBunch ready, return statement", "job", job.UID, "podBunch", podBunch.UID)
		if podBunch.IsSoftTopologyMode() {
			podBunch.AllocatedHyperNode = allocatedHyperNode
		}
		return stmt
	} else if ssn.PodBunchPipelined(job, podBunch) {
		klog.V(3).InfoS("PodBunch pipelined, return statement", "job", job.UID, "podBunch", podBunch.UID)
		return stmt
	}

	stmt.Discard()
	return nil
}

// getNewAllocatedHyperNode Obtain the newly allocated hyperNode for the job in soft topology mode
func getNewAllocatedHyperNode(ssn *framework.Session, bestNode string, jobAllocatedHyperNode string) string {
	hyperNode := util.FindHyperNodeForNode(bestNode, ssn.RealNodesList, ssn.HyperNodesTiers, ssn.HyperNodesSetByTier)
	if hyperNode != "" {
		if jobAllocatedHyperNode == "" {
			return hyperNode
		}
		return ssn.HyperNodes.GetLCAHyperNode(hyperNode, jobAllocatedHyperNode)
	}
	return jobAllocatedHyperNode
}

// prioritizeNodes selects the highest score node.
func (alloc *Action) prioritizeNodes(ssn *framework.Session, task *api.TaskInfo, predicateNodes []*api.NodeInfo) (*api.NodeInfo, float64) {
	// Candidate nodes are divided into two gradients:
	// - the first gradient node: a list of free nodes that satisfy the task resource request;
	// - The second gradient node: the node list whose sum of node idle resources and future idle meets the task resource request;
	// Score the first gradient node first. If the first gradient node meets the requirements, ignore the second gradient node list,
	// otherwise, score the second gradient node and select the appropriate node.
	var candidateNodes [][]*api.NodeInfo
	var idleCandidateNodes []*api.NodeInfo
	var futureIdleCandidateNodes []*api.NodeInfo
	for _, n := range predicateNodes {
		if task.InitResreq.LessEqual(n.Idle, api.Zero) {
			idleCandidateNodes = append(idleCandidateNodes, n)
		} else if task.InitResreq.LessEqual(n.FutureIdle(), api.Zero) {
			futureIdleCandidateNodes = append(futureIdleCandidateNodes, n)
		} else {
			klog.V(5).Infof("Predicate filtered node %v, idle: %v and future idle: %v do not meet the requirements of task: %v",
				n.Name, n.Idle, n.FutureIdle(), task.Name)
		}
	}
	candidateNodes = append(candidateNodes, idleCandidateNodes)
	candidateNodes = append(candidateNodes, futureIdleCandidateNodes)

	var bestNode *api.NodeInfo
	var higestScore float64
	for index, nodes := range candidateNodes {
		if klog.V(5).Enabled() {
			for _, node := range nodes {
				klog.V(5).Infof("node %v, idle: %v, future idle: %v", node.Name, node.Idle, node.FutureIdle())
			}
		}
		switch {
		case len(nodes) == 0:
			klog.V(5).Infof("Task: %v, no matching node is found in the candidateNodes（index: %d） list.", task.Name, index)
		case len(nodes) == 1: // If only one node after predicate, just use it.
			bestNode = nodes[0]
		case len(nodes) > 1: // If more than one node after predicate, using "the best" one
			nodeScores := util.PrioritizeNodes(task, nodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

			bestNode = ssn.BestNodeFn(task, nodeScores)
			if bestNode == nil {
				bestNode, higestScore = util.SelectBestNodeAndScore(nodeScores)
			}
		}

		// If a proper node is found in idleCandidateNodes, skip futureIdleCandidateNodes and directly return the node information.
		if bestNode != nil {
			break
		}
	}
	return bestNode, higestScore
}

func (alloc *Action) allocateResourcesForTask(stmt *framework.Statement, task *api.TaskInfo, node *api.NodeInfo, job *api.JobInfo) (err error) {
	// Allocate idle resource to the task.
	if task.InitResreq.LessEqual(node.Idle, api.Zero) {
		klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, node.Name)
		if err = stmt.Allocate(task, node); err != nil {
			klog.Errorf("Failed to bind Task %v on %v in Session %v, err: %v",
				task.UID, node.Name, alloc.session.UID, err)
			if rollbackErr := stmt.UnAllocate(task); rollbackErr != nil {
				klog.Errorf("Failed to unallocate Task %v on %v in Session %v for %v.",
					task.UID, node.Name, alloc.session.UID, rollbackErr)
			}
		} else {
			metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
			metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
		}
		return
	}

	klog.V(3).Infof("Predicates failed in allocate for task <%s/%s> on node <%s> with limited resources",
		task.Namespace, task.Name, node.Name)

	// Allocate releasing resource to the task if any.
	if task.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
		klog.V(3).Infof("Pipelining Task <%v/%v> to node <%v> for <%v> on <%v>",
			task.Namespace, task.Name, node.Name, task.InitResreq, node.Releasing)
		if err = stmt.Pipeline(task, node.Name, false); err != nil {
			klog.Errorf("Failed to pipeline Task %v on %v in Session %v for %v.",
				task.UID, node.Name, alloc.session.UID, err)
		} else {
			metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
			metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
		}
	}
	return
}

func (alloc *Action) predicate(task *api.TaskInfo, node *api.NodeInfo) error {
	// Check for Resource Predicate
	var statusSets api.StatusSets
	if ok, resources := task.InitResreq.LessEqualWithResourcesName(node.FutureIdle(), api.Zero); !ok {
		statusSets = append(statusSets, &api.Status{Code: api.Unschedulable, Reason: api.WrapInsufficientResourceReason(resources)})
		return api.NewFitErrWithStatus(task, node, statusSets...)
	}
	return alloc.session.PredicateForAllocateAction(task, node)
}

func (alloc *Action) UnInitialize() {}
