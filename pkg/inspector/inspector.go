package inspector

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

var (
	noNodeAllocated = NewScheduleError(fmt.Errorf("no node allocated"))

	someTaskNotAllocated = NewScheduleError(fmt.Errorf("some tasks are not allocated"))
)

type ScheduleError struct {
	Err error
}

func (e *ScheduleError) Error() string {
	if e == nil || e.Err == nil {
		return "unknown error"
	}

	return e.Err.Error()
}

func (e *ScheduleError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Error())
}

func NewScheduleError(err error) *ScheduleError {
	return &ScheduleError{
		Err: err,
	}
}

type TaskResult struct {
	Error    *ScheduleError `json:"error"`
	BadNodes map[string]any `json:"bad_nodes"`
	Node     string         `json:"node"`
}

func (tr *TaskResult) initError() {
	if tr.Error != nil {
		return
	}
	if tr.Node == "" {
		tr.Error = noNodeAllocated
	}
}
func (tr *TaskResult) AllocateNode() (string, *ScheduleError) {
	tr.initError()
	return tr.Node, tr.Error
}

type ScheduleResult struct {
	Error *ScheduleError

	Tasks map[string]*TaskResult
}

func FailResult(e error) *ScheduleResult {
	switch err := e.(type) {
	case *ScheduleError:
		return &ScheduleResult{
			Error: err,
		}

	}
	return &ScheduleResult{
		Error: NewScheduleError(e),
	}
}

func (sr *ScheduleResult) InitError() {
	if sr.Error != nil {
		return
	}
	someError := false
	for _, tr := range sr.Tasks {
		tr.initError()
		if tr.Error != nil {
			someError = true
		}
	}
	if someError {
		sr.Error = someTaskNotAllocated
	}
}

func (sr *ScheduleResult) AllocateNodes() (set.Set[string], *ScheduleError) {
	if sr == nil {
		return nil, noNodeAllocated
	}
	if sr.Error != nil {
		return nil, sr.Error
	}
	res := set.New[string]()
	for _, tr := range sr.Tasks {
		res.Insert(tr.Node)
	}
	return res, nil
}

func (sr *ScheduleResult) OneTaskResult() (tr *TaskResult) {
	if sr == nil {
		return &TaskResult{
			Error: noNodeAllocated,
		}
	}
	if sr.Error != nil {
		return &TaskResult{
			Error: sr.Error,
		}
	}
	for _, tr := range sr.Tasks {
		tr.initError()
		return tr
	}
	return &TaskResult{
		Error: noNodeAllocated,
	}
}

func (sr *ScheduleResult) String() string {
	if sr == nil {
		return ""
	}
	bx, e := json.Marshal(sr)
	if e != nil {
		return e.Error()
	}
	return string(bx)
}
func Execute(ssn *framework.Session, jobs map[api.JobID]*api.JobInfo) (res *ScheduleResult) {
	klog.V(5).Infof("Enter Allocate ... ssn:%v", ssn.UID)
	defer klog.V(5).Infof("Leaving Allocate ... ssn:%v", ssn.UID)

	// queues sort queues by QueueOrderFn.
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	// jobsMap is used to find job with the highest priority in given queue.
	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	if len(ssn.Jobs) != len(jobs) {
		klog.Errorf("ssn.Jobs length is not equal to len(jobs): %v != %v", len(ssn.Jobs), len(jobs))
		res = &ScheduleResult{
			Error: NewScheduleError(fmt.Errorf("some bad happend ssn:%v jobs:%v len(jobs):%v", ssn.UID, ssn.Jobs, len(jobs))),
		}
		return res
	}

	err := pickUpQueuesAndJobs(ssn, queues, jobsMap)
	if err != nil {
		return FailResult(err)
	}
	if len(jobsMap) == 0 {
		return FailResult(fmt.Errorf("no jobs to allocate"))
	}
	klog.V(3).Infof("Try to allocate resource to %d Queues, ssn:%v", len(jobsMap), ssn.UID)
	res = allocateResources(ssn, queues, jobsMap)
	klog.V(3).Infof("Allocate result: %v, ssn:%v", res, ssn.UID)
	res.InitError()
	return
}

func pickUpQueuesAndJobs(ssn *framework.Session, queues *util.PriorityQueue, jobsMap map[api.QueueID]*util.PriorityQueue) error {
	for _, job := range ssn.Jobs {
		// If not config enqueue action, change Pending pg into Inqueue state to avoid blocking job scheduling.
		if job.IsPending() {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> status update from pending to inqueue, reason: no enqueue action is configured.",
				job.Namespace, job.Name, job.Queue)
			job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			return fmt.Errorf("job %s/%s is not valid", job.Namespace, job.Name)
		}

		if _, found := ssn.Queues[job.Queue]; !found {
			klog.Warningf("Skip adding Job <%s/%s> because its queue %s is not found",
				job.Namespace, job.Name, job.Queue)
			return fmt.Errorf("queue %s is not found", job.Queue)
		}

		if _, found := jobsMap[job.Queue]; !found {
			jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			queues.Push(ssn.Queues[job.Queue])
		}

		klog.V(4).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
		jobsMap[job.Queue].Push(job)
	}

	return nil
}

func allocateResources(ssn *framework.Session, queues *util.PriorityQueue, jobsMap map[api.QueueID]*util.PriorityQueue) (res *ScheduleResult) {
	pendingTasks := map[api.JobID]*util.PriorityQueue{}

	allNodes := ssn.NodeList

	// To pick <namespace, queue> tuple for job, we choose to pick namespace firstly.
	// Because we believe that number of queues would less than namespaces in most case.
	// And, this action would make the resource usage among namespace balanced.
	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			e := fmt.Errorf("queue %s is overused", queue.UID)
			res = &ScheduleResult{
				Error: NewScheduleError(e),
			}
			continue
		}

		klog.V(3).Infof("Try to allocate resource to Jobs in Queue <%s>", queue.Name)

		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			klog.V(4).Infof("Can not find jobs for queue %s/%s.", queue.UID, queue.Name)
			e := fmt.Errorf("can not find jobs for queue %s/%s", queue.UID, queue.Name)
			res = &ScheduleResult{
				Error: NewScheduleError(e),
			}
			continue
		}

		job := jobs.Pop().(*api.JobInfo)
		if _, found = pendingTasks[job.UID]; !found {
			tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
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

				tasks.Push(task)
			}
			pendingTasks[job.UID] = tasks
		}
		tasks := pendingTasks[job.UID]

		if tasks.Empty() {
			// put queue back again and try other jobs in this queue
			queues.Push(queue)
			continue
		}

		klog.V(3).Infof("Try to allocate resource to %d tasks of Job <%v/%v>",
			tasks.Len(), job.Namespace, job.Name)

		subJob, sjExist := job.SubJobs[job.DefaultSubJobID()]
		if !sjExist {
			klog.Errorf("Job <%v/%v> does not have default subjob", job.Namespace, job.Name)
			continue
		}
		res = allocateResourcesForTasks(ssn, subJob, tasks, job, queue, allNodes, "")
		// There are still left tasks that need to be allocated when min available < replicas, put the job back
		if tasks.Len() > 0 {
			jobs.Push(job)
			// Put back the queue to priority queue after job's resource allocating finished,
			// To ensure that the priority of the queue is calculated based on the latest resource allocation situation.
			queues.Push(queue)
			continue
		}

	}

	klog.V(3).Infof("allocateResources result: %v", res)
	return res
}

func allocateResourcesForTasks(ssn *framework.Session, subJob *api.SubJobInfo, tasks *util.PriorityQueue, job *api.JobInfo, queue *api.QueueInfo, allNodes []*api.NodeInfo, hyperNode string) (res *ScheduleResult) {
	ph := util.NewPredicateHelper()

	stmt := framework.NewStatement(ssn)
	defer stmt.Discard()

	res = &ScheduleResult{
		Tasks: make(map[string]*TaskResult),
	}

	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)
		if !ssn.Allocatable(queue, task) {
			klog.V(3).Infof("Queue <%s> is overused when considering task <%s>, ignore it.", queue.Name, task.Name)
			err := fmt.Errorf("Queue %v is overused", queue.Name)
			res.Tasks[task.Name] = &TaskResult{
				Error: NewScheduleError(err),
			}
			res.Error = NewScheduleError(err)

			klog.V(3).Infof("allocateResourcesForTasks result: %v, err:=%v", res, err)
			break
		}

		// check if the task with its spec has already predicates failed
		if job.TaskHasFitErrors(subJob.UID, task) {
			klog.V(5).Infof("Task %s with role spec %s has already predicated failed, skip", task.Name, task.TaskRole)

			err := fmt.Errorf("Task %s with role spec %s has already predicated failed", task.Name, task.TaskRole)
			res.Tasks[task.Name] = &TaskResult{
				Error: NewScheduleError(err),
			}
			res.Error = NewScheduleError(err)
			break
		}

		klog.V(3).Infof("There are <%d> nodes for Job <%v/%v>", len(ssn.Nodes), job.Namespace, job.Name)

		if err := ssn.PrePredicateFn(task); err != nil {
			klog.V(3).Infof("PrePredicate for task %s/%s failed for: %v", task.Namespace, task.Name, err)
			fitErrors := api.NewFitErrors()
			for _, ni := range allNodes {
				fitErrors.SetNodeError(ni.Name, err)
			}
			job.NodesFitErrors[task.UID] = fitErrors

			res.Error = NewScheduleError(err)
			res.Tasks[task.Name] = &TaskResult{
				Error: NewScheduleError(err),
			}
			break
		}

		var predicateNodes []*api.NodeInfo
		var fitErrors *api.FitErrors

		// If the nominated node is not found or the nominated node is not suitable for the task, we need to find a suitable node for the task from all nodes.
		if len(predicateNodes) == 0 {
			predicateNodes, fitErrors = ph.PredicateNodes(task, allNodes, predicate(ssn), true)
		}

		if len(predicateNodes) == 0 {
			// TODO: Need to add PostFilter extension point implementation here. For example, the DRA plugin includes the PostFilter extension point,
			// but the DRA's PostFilter only occurs in extreme error conditions: Suppose a pod uses two claims. In the first scheduling attempt,
			// a node is picked and PreBind manages to update the first claim so that it is allocated and reserved for the pod.
			// But then updating the second claim fails (e.g., apiserver down) and the scheduler has to retry. During the next pod scheduling attempt,
			// the original node is no longer usable for other reasons. Other nodes are not usable either because of the allocated claim.
			// The DRA scheduler plugin detects that and then when scheduling fails (= no node passed filtering), it recovers by de-allocating the allocated claim in PostFilter.
			job.NodesFitErrors[task.UID] = fitErrors

			res.Tasks[task.Name] = &TaskResult{
				BadNodes: make(map[string]any),
			}
			for _, nodeFitErr := range fitErrors.Nodes() {
				res.Tasks[task.Name].BadNodes[nodeFitErr.NodeName] = nodeFitErr.Reasons()
			}

			// Assume that all left tasks are allocatable, but can not meet gang-scheduling min member,
			// so we should break from continuously allocating.
			// otherwise, should continue to find other allocatable task
			if job.NeedContinueAllocating(subJob.UID) {
				continue
			} else {
				break
			}
		}

		bestNode, _ := prioritizeNodes(ssn, task, predicateNodes)
		if bestNode == nil {
			continue
		}

		err := allocateResourcesForTask(ssn, stmt, task, bestNode, job)
		if err != nil {
			res.Tasks[task.Name] = &TaskResult{
				Error: NewScheduleError(err),
			}
			continue
		}

		res.Tasks[task.Name] = &TaskResult{
			Node: bestNode.Name,
		}
	}

	klog.V(3).Infof("allocateResourcesForTasks result: %v", res)
	return res
}

type predicateFn = func(task *api.TaskInfo, node *api.NodeInfo) error

func predicate(ssn *framework.Session) predicateFn {
	return func(task *api.TaskInfo, node *api.NodeInfo) error {
		var statusSets api.StatusSets
		if ok, resources := task.InitResreq.LessEqualWithResourcesName(node.FutureIdle(), api.Zero); !ok {
			statusSets = append(statusSets, &api.Status{Code: api.Unschedulable, Reason: api.WrapInsufficientResourceReason(resources)})
			return api.NewFitErrWithStatus(task, node, statusSets...)
		}
		return ssn.PredicateForAllocateAction(task, node)
	}
}

// prioritizeNodes selects the highest score node.
func prioritizeNodes(ssn *framework.Session, task *api.TaskInfo, predicateNodes []*api.NodeInfo) (*api.NodeInfo, float64) {
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

func allocateResourcesForTask(ssn *framework.Session, stmt *framework.Statement, task *api.TaskInfo, node *api.NodeInfo, job *api.JobInfo) (err error) {
	// Allocate idle resource to the task.
	if task.InitResreq.LessEqual(node.Idle, api.Zero) {
		klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, node.Name)
		if err = stmt.Allocate(task, node); err != nil {
			klog.Errorf("Failed to bind Task %v on %v in Session %v, err: %v",
				task.UID, node.Name, ssn.UID, err)
			if rollbackErr := stmt.UnAllocate(task); rollbackErr != nil {
				klog.Errorf("Failed to unallocate Task %v on %v in Session %v for %v.",
					task.UID, node.Name, ssn.UID, rollbackErr)
			}
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
				task.UID, node.Name, ssn.UID, err)
		}
	}
	return
}
