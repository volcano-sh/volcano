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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"volcano.sh/apis/pkg/apis/scheduling"
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
	cache           cache.Cache
	informerFactory informers.SharedInformerFactory

	TotalResource *api.Resource
	// podGroupStatus cache podgroup status during schedule
	// This should not be mutated after initiated
	podGroupStatus map[api.JobID]scheduling.PodGroupStatus

	Jobs           map[api.JobID]*api.JobInfo
	Nodes          map[string]*api.NodeInfo
	RevocableNodes map[string]*api.NodeInfo
	Queues         map[api.QueueID]*api.QueueInfo
	NamespaceInfo  map[api.NamespaceName]*api.NamespaceInfo

	Tiers          []conf.Tier
	Configurations []conf.Configuration
	NodeList       []*api.NodeInfo

	plugins           map[string]Plugin
	eventHandlers     []*EventHandler
	jobOrderFns       map[string]api.CompareFn
	queueOrderFns     map[string]api.CompareFn
	taskOrderFns      map[string]api.CompareFn
	namespaceOrderFns map[string]api.CompareFn
	clusterOrderFns   map[string]api.CompareFn
	predicateFns      map[string]api.PredicateFn
	bestNodeFns       map[string]api.BestNodeFn
	nodeOrderFns      map[string]api.NodeOrderFn
	batchNodeOrderFns map[string]api.BatchNodeOrderFn
	nodeMapFns        map[string]api.NodeMapFn
	nodeReduceFns     map[string]api.NodeReduceFn
	preemptableFns    map[string]api.EvictableFn
	reclaimableFns    map[string]api.EvictableFn
	overusedFns       map[string]api.ValidateFn
	allocatableFns    map[string]api.AllocatableFn
	jobReadyFns       map[string]api.ValidateFn
	jobPipelinedFns   map[string]api.VoteFn
	jobValidFns       map[string]api.ValidateExFn
	jobEnqueueableFns map[string]api.VoteFn
	jobEnqueuedFns    map[string]api.JobEnqueuedFn
	targetJobFns      map[string]api.TargetJobFn
	reservedNodesFns  map[string]api.ReservedNodesFn
	victimTasksFns    map[string]api.VictimTasksFn
	jobStarvingFns    map[string]api.ValidateFn
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		UID:             uuid.NewUUID(),
		kubeClient:      cache.Client(),
		cache:           cache,
		informerFactory: cache.SharedInformerFactory(),

		TotalResource:  api.EmptyResource(),
		podGroupStatus: map[api.JobID]scheduling.PodGroupStatus{},

		Jobs:           map[api.JobID]*api.JobInfo{},
		Nodes:          map[string]*api.NodeInfo{},
		RevocableNodes: map[string]*api.NodeInfo{},
		Queues:         map[api.QueueID]*api.QueueInfo{},

		plugins:           map[string]Plugin{},
		jobOrderFns:       map[string]api.CompareFn{},
		queueOrderFns:     map[string]api.CompareFn{},
		taskOrderFns:      map[string]api.CompareFn{},
		namespaceOrderFns: map[string]api.CompareFn{},
		clusterOrderFns:   map[string]api.CompareFn{},
		predicateFns:      map[string]api.PredicateFn{},
		bestNodeFns:       map[string]api.BestNodeFn{},
		nodeOrderFns:      map[string]api.NodeOrderFn{},
		batchNodeOrderFns: map[string]api.BatchNodeOrderFn{},
		nodeMapFns:        map[string]api.NodeMapFn{},
		nodeReduceFns:     map[string]api.NodeReduceFn{},
		preemptableFns:    map[string]api.EvictableFn{},
		reclaimableFns:    map[string]api.EvictableFn{},
		overusedFns:       map[string]api.ValidateFn{},
		allocatableFns:    map[string]api.AllocatableFn{},
		jobReadyFns:       map[string]api.ValidateFn{},
		jobPipelinedFns:   map[string]api.VoteFn{},
		jobValidFns:       map[string]api.ValidateExFn{},
		jobEnqueueableFns: map[string]api.VoteFn{},
		jobEnqueuedFns:    map[string]api.JobEnqueuedFn{},
		targetJobFns:      map[string]api.TargetJobFn{},
		reservedNodesFns:  map[string]api.ReservedNodesFn{},
		victimTasksFns:    map[string]api.VictimTasksFn{},
		jobStarvingFns:    map[string]api.ValidateFn{},
	}

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		// only conditions will be updated periodically
		if job.PodGroup != nil && job.PodGroup.Status.Conditions != nil {
			ssn.podGroupStatus[job.UID] = *job.PodGroup.Status.DeepCopy()
		}

		if vjr := ssn.JobValid(job); vjr != nil {
			if !vjr.Pass {
				jc := &scheduling.PodGroupCondition{
					Type:               scheduling.PodGroupUnschedulableType,
					Status:             v1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					TransitionID:       string(ssn.UID),
					Reason:             vjr.Reason,
					Message:            vjr.Message,
				}

				if err := ssn.UpdatePodGroupCondition(job, jc); err != nil {
					klog.Errorf("Failed to update job condition: %v", err)
				}
			}

			delete(ssn.Jobs, job.UID)
		}
	}
	ssn.NodeList = util.GetNodeList(snapshot.Nodes, snapshot.NodeList)
	ssn.Nodes = snapshot.Nodes
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

func closeSession(ssn *Session) {
	ju := newJobUpdater(ssn)
	ju.UpdateAll()

	ssn.Jobs = nil
	ssn.Nodes = nil
	ssn.RevocableNodes = nil
	ssn.plugins = nil
	ssn.eventHandlers = nil
	ssn.jobOrderFns = nil
	ssn.namespaceOrderFns = nil
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
			if api.AllocatedStatus(status) || status == api.Succeeded {
				allocated += len(tasks)
			}
		}

		// If there're enough allocated resource, it's running
		if int32(allocated) >= jobInfo.PodGroup.Spec.MinMember {
			status.Phase = scheduling.PodGroupRunning
		} else if jobInfo.PodGroup.Status.Phase != scheduling.PodGroupInqueue {
			status.Phase = scheduling.PodGroupPending
		}
	}

	status.Running = int32(len(jobInfo.TaskStatusIndex[api.Running]))
	status.Failed = int32(len(jobInfo.TaskStatusIndex[api.Failed]))
	status.Succeeded = int32(len(jobInfo.TaskStatusIndex[api.Succeeded]))

	return status
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
			klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s when binding", task.Job)
	}

	task.NodeName = hostname

	if node, found := ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			klog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
			return err
		}
		klog.V(3).Infof("After added Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index when binding.",
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

//Allocate the task to the node in the session
func (ssn *Session) Allocate(task *api.TaskInfo, nodeInfo *api.NodeInfo) (err error) {
	podVolumes, err := ssn.cache.GetPodVolumes(task, nodeInfo.Node)
	if err != nil {
		return err
	}

	hostname := nodeInfo.Name
	if err := ssn.cache.AllocateVolumes(task, hostname, podVolumes); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			ssn.cache.RevertVolumes(task, podVolumes)
		}
	}()

	task.Pod.Spec.NodeName = hostname
	task.PodVolumes = podVolumes

	// Only update status in session
	job, found := ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
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
			klog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
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
	} else {
		ssn.cache.RevertVolumes(task, podVolumes)
	}

	return nil
}

func (ssn *Session) dispatch(task *api.TaskInfo) error {
	if err := ssn.cache.AddBindTask(task); err != nil {
		return err
	}

	// Update status in session
	if job, found := ssn.Jobs[task.Job]; found {
		if err := job.UpdateTaskStatus(task, api.Binding); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
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

//Evict the task in the session
func (ssn *Session) Evict(reclaimee *api.TaskInfo, reason string) error {
	if err := ssn.cache.Evict(reclaimee, reason); err != nil {
		return err
	}

	// Update status in session
	job, found := ssn.Jobs[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Releasing); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
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
func (ssn Session) KubeClient() kubernetes.Interface {
	return ssn.kubeClient
}

// InformerFactory returns the scheduler ShareInformerFactory
func (ssn Session) InformerFactory() informers.SharedInformerFactory {
	return ssn.informerFactory
}

//String return nodes and jobs information in the session
func (ssn Session) String() string {
	msg := fmt.Sprintf("Session %v: \n", ssn.UID)

	for _, job := range ssn.Jobs {
		msg = fmt.Sprintf("%s%v\n", msg, job)
	}

	for _, node := range ssn.Nodes {
		msg = fmt.Sprintf("%s%v\n", msg, node)
	}

	return msg
}
