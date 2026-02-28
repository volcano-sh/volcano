/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added k8s client integration and event recording capabilities
- Enhanced with HyperNode support for network topology aware scheduling
- Extended plugin system with additional extension points

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
	"fmt"
	"maps"
	"sort"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingscheme "volcano.sh/apis/pkg/apis/scheduling/scheme"
	vcv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// ClusterTopHyperNode is the common root for all HyperNodes in the snapshot.
	// During the session open phase, a virtual cluster-level top-tier HyperNode is created in the snapshot, simplifying subsequent scheduling logic.
	// Its Tier value is set to the maximum existing Tier + 1 among real HyperNodes. This is recalculated every time a session opens.
	// If no real HyperNodes exist in the cluster, this virtual top-tier HyperNode will still exist with Tier = 1 and will encompass all Nodes in the cluster.
	ClusterTopHyperNode = "<cluster-top-hypernode>"
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

	TotalResource *api.Resource
	// PodGroupOldState contains podgroup status and annotations during schedule
	// This should not be mutated after initiated
	PodGroupOldState *api.PodGroupOldState
	// DirtyJobs include the jobs that need to flush to SchedulerCache on session close
	DirtyJobs sets.Set[api.JobID]

	Jobs           map[api.JobID]*api.JobInfo
	Nodes          map[string]*api.NodeInfo
	CSINodesStatus map[string]*api.CSINodeStatusInfo
	RevocableNodes map[string]*api.NodeInfo
	Queues         map[api.QueueID]*api.QueueInfo
	NamespaceInfo  map[api.NamespaceName]*api.NamespaceInfo

	// NodeMap is like Nodes except that it uses k8s NodeInfo api and should only
	// be used in k8s compatible api scenarios such as in predicates and nodeorder plugins.
	NodeMap   map[string]fwk.NodeInfo
	PodLister *PodLister

	Tiers          []conf.Tier
	Configurations []conf.Configuration
	NodeList       []*api.NodeInfo
	// HyperNodes stores the HyperNodeInfo of each HyperNode
	HyperNodes           api.HyperNodeInfoMap
	HyperNodeTierNameMap api.HyperNodeTierNameMap
	// HyperNodesSetByTier contains a set of hyperNodes by tier from down to top, nodes under the same hyperNode
	// have the same topology domain, e.g., nodes under the same switch or tor, jobs allocated in the same
	// hyperNode can gain a better performance, the lower the tier of hyperNode, the better performance.
	HyperNodesSetByTier map[int]sets.Set[string]
	HyperNodesTiers     []int
	// RealNodesList maps hyperNode Name -> nodes under the hyperNode.
	RealNodesList             map[string][]*api.NodeInfo
	RealNodesSet              map[string]sets.Set[string]
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
	preemptiveFns                 map[string]api.ValidateWithCandidateFn
	allocatableFns                map[string]api.AllocatableFn
	jobReadyFns                   map[string]api.ValidateFn
	jobPipelinedFns               map[string]api.VoteFn
	jobValidFns                   map[string]api.ValidateExFn
	jobEnqueueableFns             map[string]api.VoteFn
	jobEnqueuedFns                map[string]api.JobEnqueuedFn
	targetJobFns                  map[string]api.TargetJobFn
	reservedNodesFns              map[string]api.ReservedNodesFn
	victimTasksFns                map[string][]api.VictimTasksFn
	jobStarvingFns                map[string]api.ValidateFn
	simulateRemoveTaskFns         map[string]api.SimulateRemoveTaskFn
	simulateAddTaskFns            map[string]api.SimulateAddTaskFn
	simulatePredicateFns          map[string]api.SimulatePredicateFn
	simulateAllocatableFns        map[string]api.SimulateAllocatableFn
	subJobReadyFns                map[string]api.ValidateFn
	subJobPipelinedFns            map[string]api.VoteFn
	subJobOrderFns                map[string]api.CompareFn
	hyperNodeGradientForJobFns    map[string]api.HyperNodeGradientForJobFn
	hyperNodeGradientForSubJobFns map[string]api.HyperNodeGradientForSubJobFn

	// cycleStatesMap is used to temporarily store the scheduling status of each pod, its life cycle is same as Session.
	// Because state needs to be passed between different extension points (not only used in PreFilter and Filter),
	// in order to avoid different Pod scheduling states from being overwritten,
	// the state needs to be temporarily stored in cycleStatesMap when an extension point is executed.
	// The key is task's UID, value is the CycleState.
	cycleStatesMap sync.Map

	NodesInShard sets.Set[string]
}

func openSession(cache cache.Cache) *Session {
	cache.OnSessionOpen()
	ssn := &Session{
		UID:             uuid.NewUUID(),
		kubeClient:      cache.Client(),
		vcClient:        cache.VCClient(),
		restConfig:      cache.ClientConfig(),
		recorder:        cache.EventRecorder(),
		cache:           cache,
		informerFactory: cache.SharedInformerFactory(),

		TotalResource: api.EmptyResource(),
		PodGroupOldState: &api.PodGroupOldState{
			Status:      map[api.JobID]scheduling.PodGroupStatus{},
			Annotations: map[api.JobID]map[string]string{},
		},
		DirtyJobs:      sets.New[api.JobID](),
		Jobs:           map[api.JobID]*api.JobInfo{},
		Nodes:          map[string]*api.NodeInfo{},
		CSINodesStatus: map[string]*api.CSINodeStatusInfo{},
		RevocableNodes: map[string]*api.NodeInfo{},
		Queues:         map[api.QueueID]*api.QueueInfo{},

		plugins:                       map[string]Plugin{},
		jobOrderFns:                   map[string]api.CompareFn{},
		queueOrderFns:                 map[string]api.CompareFn{},
		victimQueueOrderFns:           map[string]api.VictimCompareFn{},
		taskOrderFns:                  map[string]api.CompareFn{},
		clusterOrderFns:               map[string]api.CompareFn{},
		predicateFns:                  map[string]api.PredicateFn{},
		prePredicateFns:               map[string]api.PrePredicateFn{},
		bestNodeFns:                   map[string]api.BestNodeFn{},
		nodeOrderFns:                  map[string]api.NodeOrderFn{},
		batchNodeOrderFns:             map[string]api.BatchNodeOrderFn{},
		nodeMapFns:                    map[string]api.NodeMapFn{},
		nodeReduceFns:                 map[string]api.NodeReduceFn{},
		hyperNodeOrderFns:             map[string]api.HyperNodeOrderFn{},
		preemptableFns:                map[string]api.EvictableFn{},
		reclaimableFns:                map[string]api.EvictableFn{},
		overusedFns:                   map[string]api.ValidateFn{},
		preemptiveFns:                 map[string]api.ValidateWithCandidateFn{},
		allocatableFns:                map[string]api.AllocatableFn{},
		jobReadyFns:                   map[string]api.ValidateFn{},
		jobPipelinedFns:               map[string]api.VoteFn{},
		jobValidFns:                   map[string]api.ValidateExFn{},
		jobEnqueueableFns:             map[string]api.VoteFn{},
		jobEnqueuedFns:                map[string]api.JobEnqueuedFn{},
		targetJobFns:                  map[string]api.TargetJobFn{},
		reservedNodesFns:              map[string]api.ReservedNodesFn{},
		victimTasksFns:                map[string][]api.VictimTasksFn{},
		jobStarvingFns:                map[string]api.ValidateFn{},
		simulateRemoveTaskFns:         map[string]api.SimulateRemoveTaskFn{},
		simulateAddTaskFns:            map[string]api.SimulateAddTaskFn{},
		simulatePredicateFns:          map[string]api.SimulatePredicateFn{},
		simulateAllocatableFns:        map[string]api.SimulateAllocatableFn{},
		subJobReadyFns:                map[string]api.ValidateFn{},
		subJobPipelinedFns:            map[string]api.VoteFn{},
		subJobOrderFns:                map[string]api.CompareFn{},
		hyperNodeGradientForJobFns:    map[string]api.HyperNodeGradientForJobFn{},
		hyperNodeGradientForSubJobFns: map[string]api.HyperNodeGradientForSubJobFn{},
	}

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		if job.PodGroup != nil {
			ssn.PodGroupOldState.Status[job.UID] = *job.PodGroup.Status.DeepCopy()
			ssn.PodGroupOldState.Annotations[job.UID] = maps.Clone(job.PodGroup.GetAnnotations())
		}
	}
	ssn.NodeList = util.GetNodeList(snapshot.Nodes, snapshot.NodeList)

	ssn.HyperNodes = snapshot.HyperNodes
	ssn.HyperNodesSetByTier = snapshot.HyperNodesSetByTier
	ssn.HyperNodeTierNameMap = snapshot.HyperNodeTierNameMap
	ssn.RealNodesList, ssn.RealNodesSet = util.GetRealNodesByHyperNode(snapshot.RealNodesSet, snapshot.Nodes)
	ssn.HyperNodesReadyToSchedule = snapshot.HyperNodesReadyToSchedule
	ssn.addClusterTopHyperNode(ssn.NodeList)
	ssn.parseHyperNodesTiers()

	if ssn.HyperNodesReadyToSchedule {
		// hyperNodes in ssn has ClusterTopHyperNode
		hyperNodeSet := sets.New[string]()
		for hn := range ssn.HyperNodes {
			hyperNodeSet.Insert(hn)
		}
		for _, job := range ssn.Jobs {
			ssn.removeInvalidAllocatedHyperNode(job, ssn.HyperNodes)
			ssn.recoverAllocatedHyperNode(job, hyperNodeSet, ssn.HyperNodes, ssn.RealNodesSet)
		}
	}

	if klog.V(5).Enabled() {
		for _, hn := range ssn.HyperNodes {
			klog.InfoS("hyperNode in session", "name", hn.Name, "tier", hn.Tier(), "parent", hn.Parent, "children", hn.Children)
		}
	}

	ssn.Nodes = snapshot.Nodes
	ssn.CSINodesStatus = snapshot.CSINodesStatus
	ssn.RevocableNodes = snapshot.RevocableNodes
	ssn.Queues = snapshot.Queues
	ssn.NamespaceInfo = snapshot.NamespaceInfo
	// calculate all nodes' resource only once in each schedule cycle, other plugins can clone it when need
	for _, n := range ssn.Nodes {
		ssn.TotalResource.Add(n.Allocatable)
	}

	ssn.NodesInShard = snapshot.NodesInShard

	klog.V(3).Infof("Open Session %v with <%d> Job and <%d> Queues. HyperNodesReadyToSchedule: %v",
		ssn.UID, len(ssn.Jobs), len(ssn.Queues), ssn.HyperNodesReadyToSchedule)

	return ssn
}

// addClusterTopHyperNode adds a virtual top hyperNode of all hyperNodes in the cluster into the session
func (ssn *Session) addClusterTopHyperNode(nodes []*api.NodeInfo) {
	topTier := 1
	for tier := range ssn.HyperNodesSetByTier {
		if tier >= topTier {
			topTier = tier + 1 // topTier should be greater than the highest tier of real hyperNodes
		}
	}

	topHn := &topologyv1alpha1.HyperNode{}
	topHn.Name = ClusterTopHyperNode
	topHn.Spec.Tier = topTier
	topHni := api.NewHyperNodeInfo(topHn)

	for _, hni := range ssn.HyperNodes {
		if hni.Parent != "" {
			continue
		}
		hni.Parent = topHn.Name
		topHni.Children.Insert(hni.Name)
	}

	ssn.HyperNodes[topHni.Name] = topHni
	ssn.HyperNodesSetByTier[topHni.Tier()] = sets.New(topHni.Name)
	ssn.RealNodesList[topHni.Name] = nodes
	ssn.RealNodesSet[topHni.Name] = sets.New[string]()
	for _, node := range nodes {
		ssn.RealNodesSet[topHni.Name].Insert(node.Name)
	}
}

// removeInvalidAllocatedHyperNode removes the non-existent allocated hyperNode for job and subJobs in the job.
// The allocated hyperNode will also be removed if there is no allocated task in the job or subJob.
func (ssn *Session) removeInvalidAllocatedHyperNode(job *api.JobInfo, hyperNodes api.HyperNodeInfoMap) {
	// remove job AllocatedHyperNode if invalid
	if job.AllocatedHyperNode != "" {
		remove := false
		var reason string
		if _, found := hyperNodes[job.AllocatedHyperNode]; !found {
			remove = true
			reason = "allocated hyperNode not exist"
		}
		if job.AllocatedTaskNum() == 0 {
			remove = true
			reason = "there is no allocated task in the job"
		}
		if remove {
			job.AllocatedHyperNode = ""
			ssn.MarkJobDirty(job.UID)
			klog.V(3).InfoS("remove allocated hyperNode for job", "job", job.UID, "AllocatedHyperNode", job.AllocatedHyperNode, "reason", reason)
		}
	}

	// remove subJob AllocatedHyperNode if invalid
	for _, subJob := range job.SubJobs {
		if subJob.AllocatedHyperNode != "" {
			remove := false
			var reason string
			if _, found := hyperNodes[subJob.AllocatedHyperNode]; !found {
				remove = true
				reason = "allocated hyperNode not exist"
			}
			if subJob.AllocatedTaskNum() == 0 {
				remove = true
				reason = "there is no allocated task in the subJob"
			}
			if remove {
				subJob.AllocatedHyperNode = ""
				ssn.MarkJobDirty(subJob.Job)
				klog.V(3).InfoS("remove allocated hyperNode for subJob", "subJob", subJob.UID, "AllocatedHyperNode", subJob.AllocatedHyperNode, "reason", reason)
			}
		}
	}
}

// recoverAllocatedHyperNode recover the allocated hyperNode for the job and subJobs in the job with empty AllocatedHyperNode field.
// When the scheduler reboot, the allocated hyperNode of job will be lost.
// We recover this information through the nodes that tasks are running on.
func (ssn *Session) recoverAllocatedHyperNode(job *api.JobInfo, hyperNodeSet sets.Set[string], hyperNodes api.HyperNodeInfoMap, nodesByHyperNode map[string]sets.Set[string]) {
	if !job.ContainsNetworkTopology() {
		return
	}

	subJobUpdated := false

	// update subJob AllocatedHyperNode based on allocated nodes
	for _, subJob := range job.SubJobs {
		if !subJob.WithNetworkTopology() || subJob.AllocatedHyperNode != "" {
			continue
		}

		// pick up allocated tasks in the subJob
		allocatedTasks := make([]*api.TaskInfo, 0, subJob.AllocatedTaskNum())
		for status, tasks := range subJob.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}
			for _, task := range tasks {
				allocatedTasks = append(allocatedTasks, task)
			}
		}

		// find the allocated hyperNode through the nodes that tasks are running on
		var subJobAllocatedHyperNode sets.Set[string]
		for _, task := range allocatedTasks {
			if task.NodeName == "" {
				klog.Warningf("task %s/%s in allocated status %s with empty nodeName", task.Namespace, task.Name, task.Status)
				continue
			}

			// For the first task, we search among all the hyperNodes.
			// For the other tasks, we search from the hyperNodes that previous tasks were allocated to.
			var search sets.Set[string]
			if subJobAllocatedHyperNode == nil {
				search = hyperNodeSet
			} else {
				search = subJobAllocatedHyperNode
			}

			taskAllocatedHyperNode := sets.New[string]()
			for hn := range search {
				if nodes, found := nodesByHyperNode[hn]; found && nodes.Has(task.NodeName) {
					taskAllocatedHyperNode.Insert(hn)
				}
			}
			klog.V(4).Infof("find allocated hyperNode %v for task %s/%s by node %s", taskAllocatedHyperNode, task.Namespace, task.Name, task.NodeName)

			subJobAllocatedHyperNode = taskAllocatedHyperNode
			if subJobAllocatedHyperNode.Len() == 0 {
				klog.Errorf("failed to find allocated hyperNode for subJob %s by allocated nodes", subJob.UID)
				break
			}
		}
		minimumHyperNode := getLowestTierHyperNode(subJobAllocatedHyperNode, hyperNodes)

		if subJob.AllocatedHyperNode != minimumHyperNode {
			subJobUpdated = true
			subJob.AllocatedHyperNode = minimumHyperNode
			ssn.MarkJobDirty(subJob.Job)
			klog.V(3).InfoS("update subJob allocated hyperNode", "subJob", subJob.UID, "AllocatedHyperNode", minimumHyperNode)
		}
	}

	// update job AllocatedHyperNode based on subJob allocated hyperNode
	if job.AllocatedHyperNode == "" || subJobUpdated {
		var lca string
		for _, subJob := range job.SubJobs {
			if subJob.AllocatedHyperNode == "" {
				continue
			}
			lca = hyperNodes.GetLCAHyperNode(lca, subJob.AllocatedHyperNode)
			if lca == "" {
				klog.Errorf("failed to find allocated hyperNode for job %s by subJob allocated hyperNodes", job.UID)
				break
			}
		}
		if job.AllocatedHyperNode != lca {
			job.AllocatedHyperNode = lca
			ssn.MarkJobDirty(job.UID)
			klog.V(3).InfoS("update job allocated hyperNode", "job", job.UID, "AllocatedHyperNode", lca)
		}
	}
}

func getLowestTierHyperNode(hyperNodes sets.Set[string], hyperNodeInfos api.HyperNodeInfoMap) string {
	if hyperNodes == nil || hyperNodes.Len() == 0 {
		return ""
	}

	var lowestTierHyperNode *api.HyperNodeInfo
	for name := range hyperNodes {
		if hn, found := hyperNodeInfos[name]; found {
			if lowestTierHyperNode == nil || hn.Tier() < lowestTierHyperNode.Tier() {
				lowestTierHyperNode = hn
			}
		}
	}

	if lowestTierHyperNode == nil {
		klog.Warningf("No valid hypernodes found in hyperNodeInfos for the given set: %v", hyperNodes.UnsortedList())
		return ""
	}

	return lowestTierHyperNode.Name
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

func addNodeSharableDeviceUsage(ssn *Session, task *api.TaskInfo) {
	node, ok := ssn.Nodes[task.NodeName]
	taskReq := task.Resreq
	if ok {
		for _, sharedDevices := range node.Others {
			if devices, ok := sharedDevices.(api.Devices); ok && devices.HasDeviceRequest(task.Pod) {
				sResources := devices.AddQueueResource(task.Pod)
				for k, v := range sResources {
					taskReq.ScalarResources[v1.ResourceName(k)] = v
				}
			}
		}
	}
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
					addNodeSharableDeviceUsage(ssn, task)
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

	ssn.cache.OnSessionClose()

	klog.V(3).Infof("Close Session %v", ssn.UID)
}

func getPodGroupPhase(jobInfo *api.JobInfo, unschedulable bool) scheduling.PodGroupPhase {
	// If running tasks && unschedulable, unknown phase
	if len(jobInfo.TaskStatusIndex[api.Running]) != 0 && unschedulable {
		return scheduling.PodGroupUnknown
	}

	scheduled := 0
	completed := 0
	for s, tasks := range jobInfo.TaskStatusIndex {
		if api.ScheduledStatus(s) {
			scheduled += len(tasks)
		}
		if api.CompletedStatus(s) {
			completed += len(tasks)
		}
	}

	if int32(scheduled) >= jobInfo.PodGroup.Spec.MinMember {
		// If all scheduled tasks are completed, then the podgroup is completed
		if scheduled == completed {
			return scheduling.PodGroupCompleted
		}
		return scheduling.PodGroupRunning
	}

	if jobInfo.PodGroup.Status.Phase != scheduling.PodGroupInqueue {
		return scheduling.PodGroupPending
	}

	return jobInfo.PodGroup.Status.Phase
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

	status.Phase = getPodGroupPhase(jobInfo, unschedulable)
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
			state := ssn.GetCycleState(task.UID)
			handler.SetupBindContextExtension(state, bindContext)
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
func (ssn *Session) SharedDRAManager() fwk.SharedDRAManager {
	return ssn.cache.SharedDRAManager()
}

// HierarchyEnabled returns whether plugin enabled hierarchical queues
func (ssn *Session) HierarchyEnabled(pluginName string) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name != pluginName {
				continue
			}
			return plugin.EnabledHierarchy != nil && *plugin.EnabledHierarchy
		}
	}
	return false
}

func (ssn *Session) MarkJobDirty(jobID api.JobID) {
	ssn.DirtyJobs.Insert(jobID)
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

func (ssn *Session) IsJobTerminated(jobId api.JobID) bool {
	return ssn.cache.IsJobTerminated(jobId)
}
