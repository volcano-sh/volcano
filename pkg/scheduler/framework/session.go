/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	"context"
	"fmt"
	"sort"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingscheme "volcano.sh/apis/pkg/apis/scheduling/scheme"
	vcv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// Session information for the current session
type Session struct {
	UID types.UID

	kubeClient      kubernetes.Interface
	vcClient        vcclient.Interface
	recorder        record.EventRecorder
	cache           cache.Cache
	restConfig      *rest.Config
	informerFactory informers.SharedInformerFactory

	TotalResource  *api.Resource
	TotalGuarantee *api.Resource
	// PodGroupOldState contains podgroup status and annotations during schedule
	// This should not be mutated after initiated
	api.PodGroupOldState

	Jobs           map[api.JobID]*api.JobInfo
	Nodes          map[string]*api.NodeInfo
	CSINodesStatus map[string]*api.CSINodeStatusInfo
	RevocableNodes map[string]*api.NodeInfo
	Queues         map[api.QueueID]*api.QueueInfo
	NamespaceInfo  map[api.NamespaceName]*api.NamespaceInfo

	// NodeMap is like Nodes except that it uses k8s NodeInfo api and should only
	// be used in k8s compatible api scenarios such as in predicates and nodeorder plugins.
	NodeMap   map[string]*k8sframework.NodeInfo
	PodLister *PodLister

	Tiers          []conf.Tier
	Configurations []conf.Configuration
	NodeList       []*api.NodeInfo
	// HyperNodes stores the HyperNodeInfo of each HyperNode
	HyperNodes api.HyperNodeInfoMap
	// HyperNodesSetByTier contains a set of hyperNodes by tier from down to top, nodes under the same hyperNode
	// have the same topology domain, e.g., nodes under the same switch or tor, jobs allocated in the same
	// hyperNode can gain a better performance, the lower the tier of hyperNode, the better performance.
	HyperNodesSetByTier map[int]sets.Set[string]
	HyperNodesTiers     []int
	// RealNodesList maps hyperNode Name -> nodes under the hyperNode.
	RealNodesList             map[string][]*api.NodeInfo
	HyperNodesReadyToSchedule bool

	plugins             map[string]Plugin
	eventHandlers       []*EventHandler
	jobOrderFns         map[string]api.CompareFn
	queueOrderFns       map[string]api.CompareFn
	victimQueueOrderFns map[string]api.VictimCompareFn
	taskOrderFns        map[string]api.CompareFn
	clusterOrderFns     map[string]api.CompareFn
	predicateFns        map[string]api.PredicateFn
	prePredicateFns     map[string]api.PrePredicateFn
	bestNodeFns         map[string]api.BestNodeFn
	nodeOrderFns        map[string]api.NodeOrderFn
	batchNodeOrderFns   map[string]api.BatchNodeOrderFn
	nodeMapFns          map[string]api.NodeMapFn
	nodeReduceFns       map[string]api.NodeReduceFn
	hyperNodeOrderFns   map[string]api.HyperNodeOrderFn
	preemptableFns      map[string]api.EvictableFn
	reclaimableFns      map[string]api.EvictableFn
	overusedFns         map[string]api.ValidateFn
	// preemptiveFns means whether current queue can reclaim from other queue,
	// while reclaimableFns means whether current queue's resources can be reclaimed.
	preemptiveFns          map[string]api.ValidateWithCandidateFn
	allocatableFns         map[string]api.AllocatableFn
	jobReadyFns            map[string]api.ValidateFn
	jobPipelinedFns        map[string]api.VoteFn
	jobValidFns            map[string]api.ValidateExFn
	jobEnqueueableFns      map[string]api.VoteFn
	jobEnqueuedFns         map[string]api.JobEnqueuedFn
	targetJobFns           map[string]api.TargetJobFn
	reservedNodesFns       map[string]api.ReservedNodesFn
	victimTasksFns         map[string][]api.VictimTasksFn
	jobStarvingFns         map[string]api.ValidateFn
	simulateRemoveTaskFns  map[string]api.SimulateRemoveTaskFn
	simulateAddTaskFns     map[string]api.SimulateAddTaskFn
	simulatePredicateFns   map[string]api.SimulatePredicateFn
	simulateAllocatableFns map[string]api.SimulateAllocatableFn

	// cycleStatesMap is used to temporarily store the scheduling status of each pod, its life cycle is same as Session.
	// Because state needs to be passed between different extension points (not only used in PreFilter and Filter),
	// in order to avoid different Pod scheduling states from being overwritten,
	// the state needs to be temporarily stored in cycleStatesMap when an extension point is executed.
	// The key is task's UID, value is the CycleState.
	cycleStatesMap sync.Map
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		UID:             uuid.NewUUID(),
		kubeClient:      cache.Client(),
		vcClient:        cache.VCClient(),
		restConfig:      cache.ClientConfig(),
		recorder:        cache.EventRecorder(),
		cache:           cache,
		informerFactory: cache.SharedInformerFactory(),

		TotalResource:  api.EmptyResource(),
		TotalGuarantee: api.EmptyResource(),
		PodGroupOldState: api.PodGroupOldState{
			Status:      map[api.JobID]scheduling.PodGroupStatus{},
			Annotations: map[api.JobID]map[string]string{},
		},
		Jobs:           map[api.JobID]*api.JobInfo{},
		Nodes:          map[string]*api.NodeInfo{},
		CSINodesStatus: map[string]*api.CSINodeStatusInfo{},
		RevocableNodes: map[string]*api.NodeInfo{},
		Queues:         map[api.QueueID]*api.QueueInfo{},

		plugins:                map[string]Plugin{},
		jobOrderFns:            map[string]api.CompareFn{},
		queueOrderFns:          map[string]api.CompareFn{},
		victimQueueOrderFns:    map[string]api.VictimCompareFn{},
		taskOrderFns:           map[string]api.CompareFn{},
		clusterOrderFns:        map[string]api.CompareFn{},
		predicateFns:           map[string]api.PredicateFn{},
		prePredicateFns:        map[string]api.PrePredicateFn{},
		bestNodeFns:            map[string]api.BestNodeFn{},
		nodeOrderFns:           map[string]api.NodeOrderFn{},
		batchNodeOrderFns:      map[string]api.BatchNodeOrderFn{},
		nodeMapFns:             map[string]api.NodeMapFn{},
		nodeReduceFns:          map[string]api.NodeReduceFn{},
		hyperNodeOrderFns:      map[string]api.HyperNodeOrderFn{},
		preemptableFns:         map[string]api.EvictableFn{},
		reclaimableFns:         map[string]api.EvictableFn{},
		overusedFns:            map[string]api.ValidateFn{},
		preemptiveFns:          map[string]api.ValidateWithCandidateFn{},
		allocatableFns:         map[string]api.AllocatableFn{},
		jobReadyFns:            map[string]api.ValidateFn{},
		jobPipelinedFns:        map[string]api.VoteFn{},
		jobValidFns:            map[string]api.ValidateExFn{},
		jobEnqueueableFns:      map[string]api.VoteFn{},
		jobEnqueuedFns:         map[string]api.JobEnqueuedFn{},
		targetJobFns:           map[string]api.TargetJobFn{},
		reservedNodesFns:       map[string]api.ReservedNodesFn{},
		victimTasksFns:         map[string][]api.VictimTasksFn{},
		jobStarvingFns:         map[string]api.ValidateFn{},
		simulateRemoveTaskFns:  map[string]api.SimulateRemoveTaskFn{},
		simulateAddTaskFns:     map[string]api.SimulateAddTaskFn{},
		simulatePredicateFns:   map[string]api.SimulatePredicateFn{},
		simulateAllocatableFns: map[string]api.SimulateAllocatableFn{},
	}

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		if job.PodGroup != nil {
			ssn.PodGroupOldState.Status[job.UID] = *job.PodGroup.Status.DeepCopy()
			ssn.PodGroupOldState.Annotations[job.UID] = job.PodGroup.GetAnnotations()
		}
	}
	ssn.NodeList = util.GetNodeList(snapshot.Nodes, snapshot.NodeList)
	ssn.HyperNodes = snapshot.HyperNodes
	ssn.HyperNodesSetByTier = snapshot.HyperNodesSetByTier
	ssn.parseHyperNodesTiers()
	ssn.RealNodesList = util.GetRealNodesListByHyperNode(snapshot.RealNodesSet, snapshot.Nodes)
	ssn.HyperNodesReadyToSchedule = snapshot.HyperNodesReadyToSchedule
	ssn.Nodes = snapshot.Nodes
	ssn.CSINodesStatus = snapshot.CSINodesStatus
	ssn.RevocableNodes = snapshot.RevocableNodes
	ssn.Queues = snapshot.Queues
	ssn.NamespaceInfo = snapshot.NamespaceInfo
	// calculate all nodes' resource only once in each schedule cycle, other plugins can clone it when need
	for _, n := range ssn.Nodes {
		ssn.TotalResource.Add(n.Allocatable)
	}

	klog.V(3).Infof("Open Session %v with <%d> Job and <%d> Queues",
		ssn.UID, len(ssn.Jobs), len(ssn.Queues))

	return ssn
}

func (ssn *Session) parseHyperNodesTiers() {
	if len(ssn.HyperNodesSetByTier) == 0 {
		return
	}

	// sort to guarantee the traverse order is from down to top.
	var tiers []int
	for tier := range ssn.HyperNodesSetByTier {
		tiers = append(tiers, tier)
	}
	sort.Ints(tiers)
	ssn.HyperNodesTiers = tiers
}

// updateQueueStatus updates allocated field in queue status on session close.
func updateQueueStatus(ssn *Session) {
	rootQueue := api.QueueID("root")
	// calculate allocated resources on each queue
	var allocatedResources = make(map[api.QueueID]*api.Resource, len(ssn.Queues))
	for queueID := range ssn.Queues {
		allocatedResources[queueID] = &api.Resource{}
	}
	for _, job := range ssn.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, task := range tasks {
					allocatedResources[job.Queue].Add(task.Resreq)
					// recursively updates the allocated resources of parent queues
					queue := ssn.Queues[job.Queue].Queue
					// compatibility unit testing
					for ssn.Queues[rootQueue] != nil {
						parent := string(rootQueue)
						if queue.Spec.Parent != "" {
							parent = queue.Spec.Parent
						}
						allocatedResources[api.QueueID(parent)].Add(task.Resreq)

						if parent == string(rootQueue) {
							break
						}
						queue = ssn.Queues[api.QueueID(queue.Spec.Parent)].Queue
					}
				}
			}
		}
	}

	// update queue status
	for queueID := range ssn.Queues {
		// convert api.Resource to v1.ResourceList
		var queueStatus = util.ConvertRes2ResList(allocatedResources[queueID]).DeepCopy()
		if queueID == rootQueue {
			updateRootQueueResources(ssn, queueStatus)
			continue
		}

		if equality.Semantic.DeepEqual(ssn.Queues[queueID].Queue.Status.Allocated, queueStatus) {
			klog.V(5).Infof("Queue <%s> allocated resource keeps equal, no need to update queue status <%v>.",
				queueID, ssn.Queues[queueID].Queue.Status.Allocated)
			continue
		}

		ssn.Queues[queueID].Queue.Status.Allocated = queueStatus

		if err := ssn.cache.UpdateQueueStatus(ssn.Queues[queueID]); err != nil {
			klog.Errorf("failed to update queue <%s> status: %s", ssn.Queues[queueID].Name, err.Error())
		}
	}
}

// updateRootQueueResources updates the deserved/guaranteed resource and allocated resource of the root queue
func updateRootQueueResources(ssn *Session, allocated v1.ResourceList) {
	rootQueue := api.QueueID("root")
	totalResource := util.ConvertRes2ResList(ssn.TotalResource).DeepCopy()
	totalGuarantee := util.ConvertRes2ResList(ssn.TotalGuarantee).DeepCopy()

	if equality.Semantic.DeepEqual(ssn.Queues[rootQueue].Queue.Spec.Deserved, totalResource) &&
		equality.Semantic.DeepEqual(ssn.Queues[rootQueue].Queue.Spec.Guarantee.Resource, totalGuarantee) &&
		equality.Semantic.DeepEqual(ssn.Queues[rootQueue].Queue.Status.Allocated, allocated) {
		klog.V(5).Infof("Root queue deserved/guaranteed resource and allocated resource remains the same, no need to update the queue.")
		return
	}

	queue := &vcv1beta1.Queue{}
	err := schedulingscheme.Scheme.Convert(ssn.Queues[rootQueue].Queue, queue, nil)
	if err != nil {
		klog.Errorf("failed to convert scheduling.Queue to v1beta1.Queue: %s", err.Error())
		return
	}

	if !equality.Semantic.DeepEqual(queue.Spec.Deserved, totalResource) ||
		!equality.Semantic.DeepEqual(queue.Spec.Guarantee.Resource, totalGuarantee) {
		queue.Spec.Deserved = totalResource
		queue.Spec.Guarantee.Resource = totalGuarantee
		queue, err = ssn.VCClient().SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update root queue: %s", err.Error())
			return
		}
	}

	if !equality.Semantic.DeepEqual(queue.Status.Allocated, allocated) {
		queue.Status.Allocated = allocated
		_, err = ssn.VCClient().SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), queue, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update root queue status: %s", err.Error())
			return
		}
	}
}

func closeSession(ssn *Session) {
	ju := NewJobUpdater(ssn)
	ju.UpdateAll()

	updateQueueStatus(ssn)

	ssn.Jobs = nil
	ssn.Nodes = nil
	ssn.RevocableNodes = nil
	ssn.plugins = nil
	ssn.eventHandlers = nil
	ssn.jobOrderFns = nil
	ssn.queueOrderFns = nil
	ssn.clusterOrderFns = nil
	ssn.NodeList = nil
	ssn.TotalResource = nil

	klog.V(3).Infof("Close Session %v", ssn.UID)
}

func jobStatus(ssn *Session, jobInfo *api.JobInfo) scheduling.PodGroupStatus {
	status := jobInfo.PodGroup.Status

	unschedulable := false
	for _, c := range status.Conditions {
		if c.Type == scheduling.PodGroupUnschedulableType &&
			c.Status == v1.ConditionTrue &&
			c.TransitionID == string(ssn.UID) {
			unschedulable = true
			break
		}
	}

	// If running tasks && unschedulable, unknown phase
	if len(jobInfo.TaskStatusIndex[api.Running]) != 0 && unschedulable {
		status.Phase = scheduling.PodGroupUnknown
	} else {
		allocated := 0
		for status, tasks := range jobInfo.TaskStatusIndex {
			if api.AllocatedStatus(status) || api.CompletedStatus(status) {
				allocated += len(tasks)
			}
		}

		// If there're enough allocated resource, it's running
		if int32(allocated) >= jobInfo.PodGroup.Spec.MinMember {
			status.Phase = scheduling.PodGroupRunning
			// If all allocated tasks is succeeded or failed, it's completed
			if len(jobInfo.TaskStatusIndex[api.Succeeded])+len(jobInfo.TaskStatusIndex[api.Failed]) == allocated {
				status.Phase = scheduling.PodGroupCompleted
			}
		} else if jobInfo.PodGroup.Status.Phase != scheduling.PodGroupInqueue {
			status.Phase = scheduling.PodGroupPending
		}
	}

	status.Running = int32(len(jobInfo.TaskStatusIndex[api.Running]))
	status.Failed = int32(len(jobInfo.TaskStatusIndex[api.Failed]))
	status.Succeeded = int32(len(jobInfo.TaskStatusIndex[api.Succeeded]))

	return status
}

// FilterOutUnschedulableAndUnresolvableNodesForTask filter out those node that has UnschedulableAndUnresolvable
func (ssn *Session) FilterOutUnschedulableAndUnresolvableNodesForTask(task *api.TaskInfo) []*api.NodeInfo {
	fitErrors, ok1 := ssn.Jobs[task.Job]
	if !ok1 {
		return ssn.NodeList
	}
	fitErr, ok2 := fitErrors.NodesFitErrors[task.UID]
	if !ok2 {
		return ssn.NodeList
	}

	skipNodes := fitErr.GetUnschedulableAndUnresolvableNodes()
	if len(skipNodes) == 0 {
		return ssn.NodeList
	}

	ret := make([]*api.NodeInfo, 0, len(ssn.Nodes))
	for _, node := range ssn.Nodes {
		if _, ok := skipNodes[node.Name]; !ok {
			ret = append(ret, node)
		}
	}

	return ret
}

// PredicateForAllocateAction checks if the predicate error contains
// - Unschedulable
// - UnschedulableAndUnresolvable
// - ErrorSkipOrWait
func (ssn *Session) PredicateForAllocateAction(task *api.TaskInfo, node *api.NodeInfo) error {
	err := ssn.PredicateFn(task, node)
	if err == nil {
		return nil
	}

	fitError, ok := err.(*api.FitError)
	if !ok {
		return api.NewFitError(task, node, err.Error())
	}

	statusSets := fitError.Status
	if statusSets.ContainsUnschedulable() || statusSets.ContainsUnschedulableAndUnresolvable() ||
		statusSets.ContainsErrorSkipOrWait() {
		return fitError
	}
	return nil
}

// PredicateForPreemptAction checks if the predicate error contains:
// - UnschedulableAndUnresolvable
// - ErrorSkipOrWait
func (ssn *Session) PredicateForPreemptAction(task *api.TaskInfo, node *api.NodeInfo) error {
	err := ssn.PredicateFn(task, node)
	if err == nil {
		return nil
	}

	fitError, ok := err.(*api.FitError)
	if !ok {
		return api.NewFitError(task, node, err.Error())
	}

	// When filtering candidate nodes, need to consider the node statusSets instead of the err information.
	// refer to kube-scheduler preemption code: https://github.com/kubernetes/kubernetes/blob/9d87fa215d9e8020abdc17132d1252536cd752d2/pkg/scheduler/framework/preemption/preemption.go#L422
	statusSets := fitError.Status
	if statusSets.ContainsUnschedulableAndUnresolvable() || statusSets.ContainsErrorSkipOrWait() {
		return fitError
	}
	return nil
}

// Statement returns new statement object
func (ssn *Session) Statement() *Statement {
	return &Statement{
		ssn: ssn,
	}
}

// Pipeline  the task to the node in the session
func (ssn *Session) Pipeline(task *api.TaskInfo, hostname string) error {
	// Only update status in session
	job, found := ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pipelined); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when pipeline in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when pipeline.",
			task.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s when pipeline", task.Job)
	}

	task.NodeName = hostname

	if node, found := ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			klog.Errorf("Failed to add task <%v/%v> to node <%v> when pipeline in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
			return err
		}
		klog.V(3).Infof("After pipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index when pipeline.",
			hostname, ssn.UID)
		return fmt.Errorf("failed to find node %s", hostname)
	}

	for _, eh := range ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	return nil
}

// Allocate the task to the node in the session
func (ssn *Session) Allocate(task *api.TaskInfo, nodeInfo *api.NodeInfo) (err error) {
	hostname := nodeInfo.Name
	task.Pod.Spec.NodeName = hostname

	// Only update status in session
	job, found := ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when binding in Session <%v>: %v",
				task.Namespace, task.Name, api.Allocated, ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s", task.Job)
	}

	task.NodeName = hostname

	if node, found := ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			klog.Errorf("Failed to add task <%v/%v> to node <%v>  when binding in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
			return err
		}
		klog.V(3).Infof("After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index when binding.",
			hostname, ssn.UID)
		return fmt.Errorf("failed to find node %s", hostname)
	}

	// Callbacks
	for _, eh := range ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	if ssn.JobReady(job) {
		for _, task := range job.TaskStatusIndex[api.Allocated] {
			if err := ssn.dispatch(task); err != nil {
				klog.Errorf("Failed to dispatch task <%v/%v>: %v",
					task.Namespace, task.Name, err)
				return err
			}
		}
	}

	return nil
}

func (ssn *Session) dispatch(task *api.TaskInfo) error {
	bindContext := ssn.CreateBindContext(task)
	if err := ssn.cache.AddBindTask(bindContext); err != nil {
		return err
	}

	// Update status in session
	if job, found := ssn.Jobs[task.Job]; found {
		if err := job.UpdateTaskStatus(task, api.Binding); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when binding in Session <%v>: %v",
				task.Namespace, task.Name, api.Binding, ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s", task.Job)
	}

	metrics.UpdateTaskScheduleDuration(metrics.Duration(task.Pod.CreationTimestamp.Time))
	return nil
}

func (ssn *Session) CreateBindContext(task *api.TaskInfo) *cache.BindContext {
	bindContext := &cache.BindContext{
		TaskInfo:   task,
		Extensions: make(map[string]cache.BindContextExtension),
	}

	for _, plugin := range ssn.plugins {
		// If the plugin implements the BindContextHandler interface, call the SetupBindContextExtension method.
		if handler, ok := plugin.(BindContextHandler); ok {
			handler.SetupBindContextExtension(ssn, bindContext)
		}
	}

	return bindContext
}

func (ssn *Session) InitCycleState() {
	// init cycleStatesMap for each pending pod
	for _, job := range ssn.Jobs {
		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> is not valid, reason: %s, message %s, skip initing cycle state for tasks in it",
				job.Namespace, job.Name, vr.Reason, vr.Message)
			continue
		}

		for _, task := range job.TaskStatusIndex[api.Pending] {
			// Skip tasks whose pod are scheduling gated
			if task.SchGated {
				continue
			}

			// Init cycle state for the pod to share it with other extension points
			state := k8sframework.NewCycleState()
			ssn.cycleStatesMap.Store(task.UID, state)
		}
	}
}

func (ssn *Session) GetCycleState(taskID api.TaskID) *k8sframework.CycleState {
	obj, exist := ssn.cycleStatesMap.Load(taskID)
	if !exist {
		// There may be existing pods that are already running, and their cycleState was not initialized during OpenSession,
		// because initialization was only done for pending pods during OpenSession
		state := k8sframework.NewCycleState()
		ssn.cycleStatesMap.Store(taskID, state)
		return state
	}

	return obj.(*k8sframework.CycleState)
}

// Evict the task in the session
func (ssn *Session) Evict(reclaimee *api.TaskInfo, reason string) error {
	if err := ssn.cache.Evict(reclaimee, reason); err != nil {
		return err
	}

	// Update status in session
	job, found := ssn.Jobs[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Releasing); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when evicting in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when evicting.",
			reclaimee.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s", reclaimee.Job)
	}

	// Update task in node.
	if node, found := ssn.Nodes[reclaimee.NodeName]; found {
		if err := node.UpdateTask(reclaimee); err != nil {
			klog.Errorf("Failed to update task <%v/%v> in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, ssn.UID, err)
			return err
		}
	}

	for _, eh := range ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: reclaimee,
			})
		}
	}

	return nil
}

// BindPodGroup bind PodGroup to specified cluster
func (ssn *Session) BindPodGroup(job *api.JobInfo, cluster string) error {
	return ssn.cache.BindPodGroup(job, cluster)
}

// UpdatePodGroupCondition update job condition accordingly.
func (ssn *Session) UpdatePodGroupCondition(jobInfo *api.JobInfo, cond *scheduling.PodGroupCondition) error {
	job, ok := ssn.Jobs[jobInfo.UID]
	if !ok {
		return fmt.Errorf("failed to find job <%s/%s>", jobInfo.Namespace, jobInfo.Name)
	}

	index := -1
	for i, c := range job.PodGroup.Status.Conditions {
		if c.Type == cond.Type {
			index = i
			break
		}
	}

	// Update condition to the new condition.
	if index < 0 {
		job.PodGroup.Status.Conditions = append(job.PodGroup.Status.Conditions, *cond)
	} else {
		job.PodGroup.Status.Conditions[index] = *cond
	}

	return nil
}

// AddEventHandler add event handlers
func (ssn *Session) AddEventHandler(eh *EventHandler) {
	ssn.eventHandlers = append(ssn.eventHandlers, eh)
}

// UpdateSchedulerNumaInfo update SchedulerNumaInfo
func (ssn *Session) UpdateSchedulerNumaInfo(AllocatedSets map[string]api.ResNumaSets) {
	ssn.cache.UpdateSchedulerNumaInfo(AllocatedSets)
}

// KubeClient returns the kubernetes client
func (ssn *Session) KubeClient() kubernetes.Interface {
	return ssn.kubeClient
}

// VCClient returns the volcano client
func (ssn *Session) VCClient() vcclient.Interface {
	return ssn.vcClient
}

// ClientConfig returns the rest client
func (ssn *Session) ClientConfig() *rest.Config {
	return ssn.restConfig
}

// InformerFactory returns the scheduler ShareInformerFactory
func (ssn *Session) InformerFactory() informers.SharedInformerFactory {
	return ssn.informerFactory
}

// RecordPodGroupEvent records podGroup events
func (ssn *Session) RecordPodGroupEvent(podGroup *api.PodGroup, eventType, reason, msg string) {
	if podGroup == nil {
		return
	}

	pg := &vcv1beta1.PodGroup{}
	if err := schedulingscheme.Scheme.Convert(&podGroup.PodGroup, pg, nil); err != nil {
		klog.Errorf("Error while converting PodGroup to v1alpha1.PodGroup with error: %v", err)
		return
	}
	ssn.recorder.Eventf(pg, eventType, reason, msg)
}

// SharedDRAManager returns the shared DRAManager from cache
func (ssn *Session) SharedDRAManager() k8sframework.SharedDRAManager {
	return ssn.cache.SharedDRAManager()
}

// String return nodes and jobs information in the session
func (ssn *Session) String() string {
	msg := fmt.Sprintf("Session %v: \n", ssn.UID)

	for _, job := range ssn.Jobs {
		msg = fmt.Sprintf("%s%v\n", msg, job)
	}

	for _, node := range ssn.Nodes {
		msg = fmt.Sprintf("%s%v\n", msg, node)
	}

	return msg
}
