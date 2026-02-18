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
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

const (
	// GateRemovalWorkerNumKey is the configuration key for number of async gate removal workers
	GateRemovalWorkerNumKey = "gateRemovalWorkerNum"

	// Default buffer size per worker for the gate removal channel
	gateRemovalBufferPerWorker = 200
)

type allocateContext struct {
	queues              *util.PriorityQueue                 // queue of *api.QueueInfo
	jobsByQueue         map[api.QueueID]*util.PriorityQueue // queue of *api.JobInfo
	jobWorksheet        map[api.JobID]*JobWorksheet
	tasksNoHardTopology map[api.JobID]*util.PriorityQueue // queue of *api.TaskInfo, job without any hard network topology policy use this queue
}

type JobWorksheet struct {
	subJobs          *util.PriorityQueue // queue of *api.SubJobInfo
	subJobWorksheets map[api.SubJobID]*SubJobWorksheet
}

func (w *JobWorksheet) ShallowCopyFrom(another *JobWorksheet) {
	if another == nil {
		return
	}
	w.subJobs = another.subJobs
	w.subJobWorksheets = another.subJobWorksheets
}

func (w *JobWorksheet) Empty() bool {
	return w.subJobs == nil || w.subJobs.Empty()
}

func (w *JobWorksheet) Clone() *JobWorksheet {
	subJobWorksheets := make(map[api.SubJobID]*SubJobWorksheet)
	for subJobID, tasks := range w.subJobWorksheets {
		subJobWorksheets[subJobID] = tasks.Clone()
	}
	return &JobWorksheet{
		subJobs:          w.subJobs.Clone(),
		subJobWorksheets: subJobWorksheets,
	}
}

type SubJobWorksheet struct {
	tasks *util.PriorityQueue // queue of *api.TaskInfo
}

func (w *SubJobWorksheet) ShallowCopyFrom(another *SubJobWorksheet) {
	if another == nil {
		return
	}
	w.tasks = another.tasks
}

func (w *SubJobWorksheet) Empty() bool {
	return w.tasks == nil || w.tasks.Empty()
}

func (w *SubJobWorksheet) Clone() *SubJobWorksheet {
	return &SubJobWorksheet{
		tasks: w.tasks.Clone(),
	}
}

type Action struct {
	session *framework.Session
	// configured flag for error cache
	enablePredicateErrorCache bool

	recorder *Recorder

	// Async gate management infrastructure
	kubeClient              kubernetes.Interface // Cached client for worker goroutines
	schGateRemovalCh        chan schGateRemovalOperation
	schGateRemovalWorkersWg sync.WaitGroup
	schGateRemovalStopCh    chan struct{}
	gateRemovalWorkerNum    int // Number of async gate removal workers
	initOnce                sync.Once
	shutdownOnce            sync.Once
}

// schGateRemovalOperation is a struct that contains the namespace
// and name of the pod to remove the scheduling gate from.
type schGateRemovalOperation struct {
	namespace string
	name      string
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache: true, // default to enable it
		schGateRemovalStopCh:      make(chan struct{}),
		gateRemovalWorkerNum:      5, // default value
	}
}

func (alloc *Action) Name() string {
	return "allocate"
}

func (alloc *Action) Initialize() {
	// Create channel with buffer size based on worker count (200 operations per worker)
	channelSize := alloc.gateRemovalWorkerNum * gateRemovalBufferPerWorker
	alloc.schGateRemovalCh = make(chan schGateRemovalOperation, channelSize)

	// Start async gate operation workers
	for i := 0; i < alloc.gateRemovalWorkerNum; i++ {
		alloc.schGateRemovalWorkersWg.Add(1)
		go alloc.schGateRemovalWorker()
	}
	klog.V(3).Infof("Started %d async workers for gate removal", alloc.gateRemovalWorkerNum)
}

// schGateRemovalWorker processes async gate add/remove requests
func (alloc *Action) schGateRemovalWorker() {
	defer alloc.schGateRemovalWorkersWg.Done()
	for {
		select {
		case <-alloc.schGateRemovalStopCh:
			klog.V(4).Infof("Scheduling gate operation worker shutting down")
			return
		case op := <-alloc.schGateRemovalCh:
			// Fetch fresh pod state from API server
			// Use cached kubeClient to avoid data race with session updates
			pod, err := alloc.kubeClient.CoreV1().Pods(op.namespace).Get(
				context.Background(),
				op.name,
				metav1.GetOptions{})

			if err != nil {
				klog.Errorf("Failed to get pod %s/%s for gate operation: %v", op.namespace, op.name, err)
				continue
			}

			// Remove the Volcano scheduling gate
			if err := cache.RemoveVolcanoSchGate(alloc.kubeClient, pod); err != nil {
				klog.Errorf("Failed to remove gate from %s/%s: %v", op.namespace, op.name, err)
			} else {
				klog.V(3).Infof("Removed Volcano scheduling gate from pod %s/%s", op.namespace, op.name)
			}
		}
	}
}

func (alloc *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, alloc.Name())
	arguments.GetBool(&alloc.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
	arguments.GetInt(&alloc.gateRemovalWorkerNum, GateRemovalWorkerNumKey)

	// Ensure at least 1 worker
	if alloc.gateRemovalWorkerNum < 1 {
		klog.Warningf("Invalid gateRemovalWorkerNum %d, using default value 5", alloc.gateRemovalWorkerNum)
		alloc.gateRemovalWorkerNum = 5
	}
}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Allocate ...")
	defer klog.V(5).Infof("Leaving Allocate ...")

	alloc.parseArguments(ssn)

	// Initialize workers once with the configured number.
	// Cache KubeClient for thread-safe access from workers.
	alloc.initOnce.Do(func() {
		alloc.kubeClient = ssn.KubeClient()
		alloc.Initialize()
	})

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
			if subJobWorksheet, exist := worksheet.subJobWorksheets[job.DefaultSubJobID()]; exist {
				actx.tasksNoHardTopology[job.UID] = subJobWorksheet.tasks
			}
		}
	}

	return actx
}

func (alloc *Action) organizeJobWorksheet(job *api.JobInfo) *JobWorksheet {
	ssn := alloc.session

	subJobs := make([]*api.SubJobInfo, 0, len(job.SubJobs))
	subJobCountMap := map[api.SubJobGID]int32{}
	for _, subJob := range job.SubJobs {
		if ssn.SubJobReady(job, subJob) {
			// Record the number of subJobs that have been satisfied in subGroupPolicy
			subJobCountMap[subJob.GID]++
		} else {
			// Filter out subJobs that are already ready.
			subJobs = append(subJobs, subJob)
		}
	}
	slices.SortFunc(subJobs, func(l, r *api.SubJobInfo) int {
		if !ssn.SubJobOrderFn(l, r) {
			return 1
		}
		return -1
	})
	// Find the smallest set of subJobs that meets the requirements for job execution.
	requireSubJobs := sets.Set[api.SubJobID]{}
	for _, subJob := range subJobs {
		if subJobCountMap[subJob.GID] < job.MinSubJobs[subJob.GID] {
			requireSubJobs.Insert(subJob.UID)
			subJobCountMap[subJob.GID]++
		}
	}
	jWorksheet := &JobWorksheet{
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
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}

	for subJobID, subJob := range job.SubJobs {
		sjWorksheet := &SubJobWorksheet{
			tasks: util.NewPriorityQueue(ssn.TaskOrderFn),
		}

		for _, task := range subJob.TaskStatusIndex[api.Pending] {
			// Skip tasks with external (non-Volcano) scheduling gates
			// Allow Volcano-managed gates (they'll be handled by capacity plugin)
			if task.SchGated && !api.HasOnlyVolcanoSchedulingGate(task.Pod) {
				klog.V(4).Infof("Task <%v/%v> has external scheduling gate, skip it.",
					task.Namespace, task.Name)
				continue
			}

			// Skip BestEffort task in 'allocate' action.
			if task.Resreq.IsEmpty() {
				klog.V(4).Infof("Task <%v/%v> is BestEffort task, skip it.",
					task.Namespace, task.Name)
				continue
			}
			sjWorksheet.tasks.Push(task)
		}

		if !sjWorksheet.Empty() {
			jWorksheet.subJobs.Push(subJob)
			jWorksheet.subJobWorksheets[subJobID] = sjWorksheet
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
		updateJobTier(ssn.HyperNodeTierNameMap, job)
		// Currently, both hard-mode network topology scheduling and subjob level scheduling use allocateForJob.
		// TODO: In the future, we may need to unify the logic of network topology-aware scheduling and normal scheduling.
		if job.ContainsHardTopology() || job.ContainsSubJobPolicy() {
			jobWorksheet := actx.jobWorksheet[job.UID]

			klog.V(3).InfoS("Try to allocate resource for job contains hard topology or subjob policy", "queue", queue.Name, "job", job.UID,
				"allocatedHyperNode", job.AllocatedHyperNode, "subJobNum", jobWorksheet.subJobs.Len())
			stmt := alloc.allocateForJob(job, jobWorksheet, ssn.HyperNodes[framework.ClusterTopHyperNode])
			if stmt != nil && ssn.JobReady(job) { // do not commit stmt when job is pipelined
				ssn.CleanupReservations(stmt)
				stmt.Commit()
				ssn.MarkJobDirty(job.UID)
				alloc.recorder.UpdateDecisionToJob(job, ssn.HyperNodes)

				// There are still left tasks that need to be allocated when min available < replicas, put the job back
				if !jobWorksheet.Empty() {
					jobs.Push(job)
				}
			}
		} else {
			subJob, sjExist := job.SubJobs[job.DefaultSubJobID()]
			tasks, tasksExist := actx.tasksNoHardTopology[job.UID]
			if sjExist && tasksExist {
				klog.V(3).InfoS("Try to allocate resource", "queue", queue.Name, "job", job.UID, "taskNum", tasks.Len())
				stmt := alloc.allocateResourcesForTasks(subJob, tasks, framework.ClusterTopHyperNode)
				if stmt != nil && ssn.JobReady(job) { // do not commit stmt when job is pipelined
					ssn.CleanupReservations(stmt)
					stmt.Commit()

					// There are still left tasks that need to be allocated when min available < replicas, put the job back
					if tasks.Len() > 0 {
						jobs.Push(job)
					}
				}
			} else {
				klog.ErrorS(nil, "Can not find default subJob or tasks for job", "job", job.UID,
					"subJobExist", sjExist, "tasksExist", tasksExist)
			}
		}

		// Put back the queue to priority queue after job's resource allocating finished,
		// To ensure that the priority of the queue is calculated based on the latest resource allocation situation.
		queues.Push(queue)
	}
}

// schedulingGateRemoval queues async gate removal if scheduling failed.
// This ensures cluster autoscalers can see the Unschedulable condition and trigger scale-up
func (alloc *Action) schedulingGateRemoval(task *api.TaskInfo, queueID api.QueueID) {
	// Only enqueue gate removal if the task has only Volcano scheduling gate
	if api.HasOnlyVolcanoSchedulingGate(task.Pod) {
		op := schGateRemovalOperation{
			namespace: task.Namespace,
			name:      task.Name,
		}

		select {
		case alloc.schGateRemovalCh <- op:
			klog.V(3).Infof("Queued gate removal for %s/%s", task.Namespace, task.Name)
			// Update task state immediately so it won't be queued again in this cycle
			task.SchGated = false
		default:
			klog.Warningf("Gate operation queue full, skipping gate removal for %s/%s", task.Namespace, task.Name)
		}
	}
}

func (alloc *Action) allocateForJob(job *api.JobInfo, jobWorksheet *JobWorksheet, hyperNodeToAllocate *api.HyperNodeInfo) *framework.Statement {
	ssn := alloc.session

	if jobWorksheet == nil || jobWorksheet.Empty() {
		klog.V(4).InfoS("Empty job worksheet", "job", job.UID)
		return nil
	}

	alloc.recorder.SnapshotSubJobStatus(job, jobWorksheet)

	hyperNodeGradients := ssn.HyperNodeGradientForJobFn(job, hyperNodeToAllocate)
	for gradient, hyperNodes := range hyperNodeGradients {
		stmtBackup := make(map[string]*framework.Statement)   // backup the statement after the job is allocated to a hyperNode
		jobWorksheetsBackup := make(map[string]*JobWorksheet) // backup the job worksheet after the job is allocated to a hyperNode
		subJobsAllocationScores := make(map[string]float64)   // save the subJobs allocation score of the job allocated to a hyperNode

		for _, hyperNode := range hyperNodes {
			var stmtList []*framework.Statement
			var subJobsAllocationScore float64

			// Clone jobWorksheet and rest job's fit err to make sure it's a clean cache when everytime filter a hyperNode and do not affect each other between hyperNodes.
			job.ResetFitErr()
			jobWorksheetCopy := jobWorksheet.Clone()
			klog.V(3).InfoS("Try to allocate resource for job in hyperNode", "job", job.UID, "hyperNode", hyperNode.Name)

			for !jobWorksheetCopy.subJobs.Empty() {
				subJob := jobWorksheetCopy.subJobs.Pop().(*api.SubJobInfo)
				subJobWorksheet := jobWorksheetCopy.subJobWorksheets[subJob.UID]

				stmt, allocationScore := alloc.allocateForSubJob(subJob, subJobWorksheet, hyperNode)

				if stmt != nil && len(stmt.Operations()) > 0 {
					stmtList = append(stmtList, stmt)
					subJobsAllocationScore += allocationScore
					// push back when subJob is ready and remain pending task
					if !subJobWorksheet.Empty() {
						jobWorksheetCopy.subJobs.Push(subJob)
					}

					if ssn.JobReady(job) {
						break
					}
				}
			}
			// reset the subJobs to initial status
			alloc.recorder.RecoverSubJobStatus(job)

			mergedStmt := framework.SaveOperations(stmtList...)
			if len(mergedStmt.Operations()) == 0 {
				continue // skip recording this empty solution
			}
			if ssn.JobReady(job) || ssn.JobPipelined(job) {
				stmtBackup[hyperNode.Name] = mergedStmt                          // backup successful solution
				jobWorksheetsBackup[hyperNode.Name] = jobWorksheetCopy           // backup remains subJobs
				subJobsAllocationScores[hyperNode.Name] = subJobsAllocationScore // save the subJobs allocation score of the job
			}

			// dry run in every hyperNode
			for _, stmt := range stmtList {
				stmt.Discard()
			}
		}

		if len(subJobsAllocationScores) == 0 {
			klog.V(5).InfoS("Find solution for job fail", "job", job.UID, "gradient", gradient)
			continue // try next gradient
		}

		bestHyperNode, err := alloc.selectBestHyperNodeForJob(subJobsAllocationScores, job)
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

func (alloc *Action) allocateForSubJob(subJob *api.SubJobInfo, subJobWorksheet *SubJobWorksheet, hyperNodeForJob *api.HyperNodeInfo) (*framework.Statement, float64) {
	ssn := alloc.session
	job := ssn.Jobs[subJob.Job]

	if subJobWorksheet == nil || subJobWorksheet.Empty() {
		klog.V(4).InfoS("Empty subJob worksheet", "job", subJob.Job, "subJob", subJob.UID)
		return nil, 0
	}

	klog.V(3).InfoS("Try to allocate resource for subJob", "job", subJob.Job,
		"subJob", subJob.UID, "allocatedHyperNode", subJob.AllocatedHyperNode, "taskNum", subJobWorksheet.tasks.Len())

	hyperNodeGradients := ssn.HyperNodeGradientForSubJobFn(subJob, hyperNodeForJob)
	for gradient, hyperNodes := range hyperNodeGradients {
		stmtBackup := make(map[string]*framework.Statement)         // backup the statement after the subJob is allocated to a hyperNode
		subJobWorksheetsBackup := make(map[string]*SubJobWorksheet) // backup the subJob worksheet after the subJob is allocated to a hyperNode

		for _, hyperNode := range hyperNodes {
			// Clone subJobWorksheet and rest subJob's fit err to make sure it's a clean cache when everytime filter a hyperNode and do not affect each other between hyperNodes.
			job.ResetSubJobFitErr(subJob.UID)
			subJobWorksheetCopy := subJobWorksheet.Clone()

			klog.V(3).InfoS("Try to allocate resource for tasks in subJob", "job", subJob.Job,
				"subJob", subJob.UID, "taskNum", subJobWorksheetCopy.tasks.Len(), "hyperNode", hyperNode.Name)
			stmt := alloc.allocateResourcesForTasks(subJob, subJobWorksheetCopy.tasks, hyperNode.Name)

			if stmt != nil && len(stmt.Operations()) > 0 {
				stmtBackup[hyperNode.Name] = framework.SaveOperations(stmt)  // backup successful solution
				subJobWorksheetsBackup[hyperNode.Name] = subJobWorksheetCopy // backup remains tasks
				stmt.Discard()                                               // dry run in every hyperNode
			}
		}

		if len(stmtBackup) == 0 {
			klog.V(5).InfoS("Find solution for subJob fail", "subJob", subJob.UID, "gradient", gradient)
			continue // try next gradient
		}

		// select the best solution
		bestHyperNode, bestScore, err := alloc.selectBestHyperNodeForSubJob(stmtBackup, subJob)
		if err != nil {
			klog.ErrorS(err, "Cannot find best hyper node for subJob", "subJob", subJob.UID, "gradient", gradient)
			return nil, 0
		}

		// recover the stmt and update subJob's allocatedHyperNode field
		bestStmt := stmtBackup[bestHyperNode]
		finalStmt := framework.NewStatement(ssn)
		if err = finalStmt.RecoverOperations(bestStmt); err != nil {
			klog.ErrorS(err, "Failed to recover operations", "subJob", subJob.UID, "hyperNode", bestHyperNode)
			return nil, 0
		}
		newAllocatedHyperNode := ssn.HyperNodes.GetLCAHyperNode(subJob.AllocatedHyperNode, bestHyperNode)
		subJob.AllocatedHyperNode = newAllocatedHyperNode

		// inherit the remains worksheet after allocate to the best hyperNode
		subJobWorksheet.ShallowCopyFrom(subJobWorksheetsBackup[bestHyperNode])

		alloc.recorder.SaveSubJobDecision(subJob.Job, hyperNodeForJob.Name, subJob.UID, newAllocatedHyperNode)
		klog.V(3).InfoS("Allocate subJob to hyperNode success", "subJob", subJob.UID,
			"hyperNode", bestHyperNode, "score", bestScore, "newAllocatedHyperNode", newAllocatedHyperNode)

		return finalStmt, bestScore
	}

	klog.V(5).InfoS("Cannot find any solution for subJob", "subJob", subJob.UID)
	return nil, 0
}

// selectBestHyperNodeForJob return the best hyperNode for the job,
// it will score and select the best hyperNode among all available hyperNodes.
func (alloc *Action) selectBestHyperNodeForJob(subJobsAllocationScores map[string]float64, job *api.JobInfo) (string, error) {
	highestScore := math.Inf(-1)
	bestHyperNode := ""
	for hyperNode, score := range subJobsAllocationScores {
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

// selectBestHyperNodeForSubJob return the best hyperNode for the subJob,
// it will score and select the best hyperNode among all available hyperNodes.
func (alloc *Action) selectBestHyperNodeForSubJob(stmts map[string]*framework.Statement, subJob *api.SubJobInfo) (string, float64, error) {
	if len(stmts) <= 0 {
		return "", 0, fmt.Errorf("no solution found for subJob %s", subJob.UID)
	}

	ssn := alloc.session
	candidateHyperNodeGroups := make(map[string][]*api.NodeInfo)
	for hyperNode := range stmts {
		candidateHyperNodeGroups[hyperNode] = ssn.RealNodesList[hyperNode]
	}

	hyperNodeScores, err := util.PrioritizeHyperNodes(candidateHyperNodeGroups, subJob, ssn.HyperNodeOrderMapFn)
	if err != nil {
		return "", 0, fmt.Errorf("prioritize hyperNodes for subJob %s fail: %w", subJob.UID, err)
	}

	bestHyperNode, bestScore := util.SelectBestHyperNodeAndScore(hyperNodeScores)
	if bestHyperNode == "" {
		return "", 0, fmt.Errorf("cannot find best hyperNode for subJob %s", subJob.UID)
	}
	return bestHyperNode, bestScore, nil
}

func (alloc *Action) allocateResourcesForTasks(subJob *api.SubJobInfo, tasks *util.PriorityQueue, hyperNode string) *framework.Statement {
	ssn := alloc.session

	job := ssn.Jobs[subJob.Job]
	queue := ssn.Queues[job.Queue]
	nodes, exist := ssn.RealNodesList[hyperNode]
	if !exist || len(nodes) == 0 {
		klog.V(4).InfoS("There is no node in hyperNode", "job", job.UID, "hyperNode", hyperNode)
		return nil
	}

	stmt := framework.NewStatement(ssn)
	ph := util.NewPredicateHelper()

	allocatedHyperNode := subJob.AllocatedHyperNode

	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)
		if !ssn.Allocatable(queue, task) {
			klog.V(3).Infof("Queue <%s> is overused when considering task <%s>, ignore it.", queue.Name, task.Name)
			continue
		}

		// If task passed allocation check and has the QueueAllocationGate, initiate async gate removal.
		// Gate will be removed by the background worker (best effort). During the bind operation, we need
		// to ensure the gate is not present, otherwise the bind will fail.
		if task.SchGated && api.HasQueueAllocationGateAnnotation(task.Pod) {
			klog.V(3).Infof("Task %s/%s has the QueueAllocationGate, queue async gate removal", task.Namespace, task.Name)
			alloc.schedulingGateRemoval(task, queue.UID)
		}

		// Skip tasks with external (non-Volcano) scheduling gates
		if task.SchGated {
			if api.HasOnlyVolcanoSchedulingGate(task.Pod) {
				klog.Warningf("Task %s/%s has Volcano scheduling gate but missing annotation %q, gate will not be removed automatically; add the annotation or remove the gate manually",
					task.Namespace, task.Name, schedulingv1beta1.QueueAllocationGateKey)
			} else {
				klog.V(4).Infof("Task %s/%s has non-Volcano scheduling gate, skipping", task.Namespace, task.Name)
			}
			continue
		}

		// check if the task with its spec has already predicates failed
		if job.TaskHasFitErrors(subJob.UID, task) {
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
				predicateNodes, fitErrors = ph.PredicateNodes(task, []*api.NodeInfo{nominatedNodeInfo}, alloc.predicate, alloc.enablePredicateErrorCache, ssn.NodesInShard)
			}
		}

		// If the nominated node is not found or the nominated node is not suitable for the task, we need to find a suitable node for the task from all nodes.
		if len(predicateNodes) == 0 {
			predicateNodes, fitErrors = ph.PredicateNodes(task, nodes, alloc.predicate, alloc.enablePredicateErrorCache, ssn.NodesInShard)
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
			if job.NeedContinueAllocating(subJob.UID) {
				continue
			} else {
				break
			}
		}

		if subJob.WithNetworkTopology() {
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

		if subJob.WithNetworkTopology() {
			allocatedHyperNode = getNewAllocatedHyperNode(ssn, bestNode.Name, allocatedHyperNode)
		}

		if ssn.SubJobReady(job, subJob) {
			break
		}
	}

	if ssn.SubJobReady(job, subJob) {
		klog.V(3).InfoS("SubJob ready, return statement", "job", job.UID, "subJob", subJob.UID)
		if subJob.IsSoftTopologyMode() {
			subJob.AllocatedHyperNode = allocatedHyperNode
		}
		return stmt
	} else if ssn.SubJobPipelined(job, subJob) {
		klog.V(3).InfoS("SubJob pipelined, return statement", "job", job.UID, "subJob", subJob.UID)
		return stmt
	}

	stmt.Discard()
	return nil
}

func updateJobTier(hyperNodeTierNameMap api.HyperNodeTierNameMap, job *api.JobInfo) {
	klog.V(4).InfoS("updateJobTier", "job", job.UID, "hyperNodeTierNameMap", hyperNodeTierNameMap)
	if job.PodGroup.Spec.NetworkTopology != nil && job.PodGroup.Spec.NetworkTopology.HighestTierName != "" && job.PodGroup.Spec.NetworkTopology.HighestTierAllowed == nil {
		if tier, ok := hyperNodeTierNameMap[job.PodGroup.Spec.NetworkTopology.HighestTierName]; ok {
			job.PodGroup.Spec.NetworkTopology.HighestTierAllowed = &tier
			job.PodGroup.Spec.NetworkTopology.HighestTierName = ""
		} else {
			klog.Warningf("The tier corresponding to highestTierName %s is not found, job <%s>",
				job.PodGroup.Spec.NetworkTopology.HighestTierName, job.UID)
		}
	}
	for _, subGroupPolicy := range job.PodGroup.Spec.SubGroupPolicy {
		if subGroupPolicy.NetworkTopology != nil && subGroupPolicy.NetworkTopology.HighestTierName != "" && subGroupPolicy.NetworkTopology.HighestTierAllowed == nil {
			if tier, ok := hyperNodeTierNameMap[subGroupPolicy.NetworkTopology.HighestTierName]; ok {
				subGroupPolicy.NetworkTopology.HighestTierAllowed = &tier
				subGroupPolicy.NetworkTopology.HighestTierName = ""
			} else {
				klog.Warningf("The tier corresponding to highestTierName %s in subGroupPolicy %s is not found, job <%s>",
					subGroupPolicy.NetworkTopology.HighestTierName, subGroupPolicy.Name, job.UID)
			}
		}
	}
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
	shardingMode := options.ServerOpts.ShardingMode
	var candidateNodes [][]*api.NodeInfo
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
		} else {
			klog.V(5).Infof("Predicate filtered node %v, idle: %v and future idle: %v do not meet the requirements of task: %v",
				n.Name, n.Idle, n.FutureIdle(), task.Name)
		}
	}

	// To allocate to nodes with enough resource and nodes within shard of this scheduler first, allocation of Pod follow below order:
	// 1. Node with IDLE resource in shard for this scheduler
	// 2. Node with IDLE resource in shard for other scheduler  (empty if sharding mode is not soft)
	// 3. Node with Future IDLE resource in shard for this scheduler
	// 4. Node with Future IDLE resource in shard for other scheduler (empty if sharding mode is not soft)
	candidateNodes = append(candidateNodes, idleCandidateNodes)
	candidateNodes = append(candidateNodes, idleCandidateNodesInOtherShards)
	candidateNodes = append(candidateNodes, futureIdleCandidateNodes)
	candidateNodes = append(candidateNodes, futureIdleCandidateNodesInOtherShards)

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

func (alloc *Action) UnInitialize() {
	alloc.shutdownOnce.Do(func() {
		// Signal workers to shutdown
		close(alloc.schGateRemovalStopCh)
		// Wait for all workers to finish
		alloc.schGateRemovalWorkersWg.Wait()
		// Close the channel (only if Initialize was ever called)
		if alloc.schGateRemovalCh != nil {
			close(alloc.schGateRemovalCh)
		}
		klog.V(3).Infof("Async gate removal workers shut down")
	})
}
