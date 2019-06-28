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

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/conf"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/metrics"
)

// Session information for the current session
type Session struct {
	UID types.UID

	cache cache.Cache

	podGroupStatus map[api.JobID]*v1alpha1.PodGroupStatus

	Jobs    map[api.JobID]*api.JobInfo
	Nodes   map[string]*api.NodeInfo
	Queues  map[api.QueueID]*api.QueueInfo
	Backlog []*api.JobInfo
	Tiers   []conf.Tier

	plugins           map[string]Plugin
	eventHandlers     []*EventHandler
	jobOrderFns       map[string]api.CompareFn
	queueOrderFns     map[string]api.CompareFn
	taskOrderFns      map[string]api.CompareFn
	predicateFns      map[string]api.PredicateFn
	nodeOrderFns      map[string]api.NodeOrderFn
	batchNodeOrderFns map[string]api.BatchNodeOrderFn
	nodeMapFns        map[string]api.NodeMapFn
	nodeReduceFns     map[string]api.NodeReduceFn
	preemptableFns    map[string]api.EvictableFn
	reclaimableFns    map[string]api.EvictableFn
	overusedFns       map[string]api.ValidateFn
	jobReadyFns       map[string]api.ValidateFn
	jobPipelinedFns   map[string]api.ValidateFn
	jobValidFns       map[string]api.ValidateExFn
	jobEnqueueableFns map[string]api.ValidateFn
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		UID:   uuid.NewUUID(),
		cache: cache,

		podGroupStatus: map[api.JobID]*v1alpha1.PodGroupStatus{},

		Jobs:   map[api.JobID]*api.JobInfo{},
		Nodes:  map[string]*api.NodeInfo{},
		Queues: map[api.QueueID]*api.QueueInfo{},

		plugins:           map[string]Plugin{},
		jobOrderFns:       map[string]api.CompareFn{},
		queueOrderFns:     map[string]api.CompareFn{},
		taskOrderFns:      map[string]api.CompareFn{},
		predicateFns:      map[string]api.PredicateFn{},
		nodeOrderFns:      map[string]api.NodeOrderFn{},
		batchNodeOrderFns: map[string]api.BatchNodeOrderFn{},
		nodeMapFns:        map[string]api.NodeMapFn{},
		nodeReduceFns:     map[string]api.NodeReduceFn{},
		preemptableFns:    map[string]api.EvictableFn{},
		reclaimableFns:    map[string]api.EvictableFn{},
		overusedFns:       map[string]api.ValidateFn{},
		jobReadyFns:       map[string]api.ValidateFn{},
		jobPipelinedFns:   map[string]api.ValidateFn{},
		jobValidFns:       map[string]api.ValidateExFn{},
		jobEnqueueableFns: map[string]api.ValidateFn{},
	}

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		// only conditions will be updated periodically
		if job.PodGroup != nil && job.PodGroup.Status.Conditions != nil {
			ssn.podGroupStatus[job.UID] = job.PodGroup.Status.DeepCopy()
		}

		if vjr := ssn.JobValid(job); vjr != nil {
			if !vjr.Pass {
				jc := &v1alpha1.PodGroupCondition{
					Type:               v1alpha1.PodGroupUnschedulableType,
					Status:             v1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					TransitionID:       string(ssn.UID),
					Reason:             vjr.Reason,
					Message:            vjr.Message,
				}

				if err := ssn.UpdateJobCondition(job, jc); err != nil {
					glog.Errorf("Failed to update job condition: %v", err)
				}
			}

			delete(ssn.Jobs, job.UID)
		}
	}

	ssn.Nodes = snapshot.Nodes
	ssn.Queues = snapshot.Queues

	glog.V(3).Infof("Open Session %v with <%d> Job and <%d> Queues",
		ssn.UID, len(ssn.Jobs), len(ssn.Queues))

	return ssn
}

func closeSession(ssn *Session) {
	ju := newJobUpdater(ssn)
	ju.UpdateAll()

	ssn.Jobs = nil
	ssn.Nodes = nil
	ssn.Backlog = nil
	ssn.plugins = nil
	ssn.eventHandlers = nil
	ssn.jobOrderFns = nil
	ssn.queueOrderFns = nil

	glog.V(3).Infof("Close Session %v", ssn.UID)
}

func jobStatus(ssn *Session, jobInfo *api.JobInfo) v1alpha1.PodGroupStatus {
	status := jobInfo.PodGroup.Status

	unschedulable := false
	for _, c := range status.Conditions {
		if c.Type == v1alpha1.PodGroupUnschedulableType &&
			c.Status == v1.ConditionTrue &&
			c.TransitionID == string(ssn.UID) {

			unschedulable = true
			break
		}
	}

	// If running tasks && unschedulable, unknown phase
	if len(jobInfo.TaskStatusIndex[api.Running]) != 0 && unschedulable {
		status.Phase = v1alpha1.PodGroupUnknown
	} else {
		allocated := 0
		for status, tasks := range jobInfo.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				allocated += len(tasks)
			}
		}

		// If there're enough allocated resource, it's running
		if int32(allocated) >= jobInfo.PodGroup.Spec.MinMember {
			status.Phase = v1alpha1.PodGroupRunning
		} else if jobInfo.PodGroup.Status.Phase != v1alpha1.PodGroupInqueue {
			status.Phase = v1alpha1.PodGroupPending
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
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, ssn.UID, err)
			return err
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s when binding", task.Job)
	}

	task.NodeName = hostname

	if node, found := ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
			return err
		}
		glog.V(3).Infof("After added Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
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
func (ssn *Session) Allocate(task *api.TaskInfo, hostname string) error {
	if err := ssn.cache.AllocateVolumes(task, hostname); err != nil {
		return err
	}

	// Only update status in session
	job, found := ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Allocated, ssn.UID, err)
			return err
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s", task.Job)
	}

	task.NodeName = hostname

	if node, found := ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
			return err
		}
		glog.V(3).Infof("After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
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
				glog.Errorf("Failed to dispatch task <%v/%v>: %v",
					task.Namespace, task.Name, err)
				return err
			}
		}
	}

	return nil
}

func (ssn *Session) dispatch(task *api.TaskInfo) error {
	if err := ssn.cache.BindVolumes(task); err != nil {
		return err
	}

	if err := ssn.cache.Bind(task, task.NodeName); err != nil {
		return err
	}

	// Update status in session
	if job, found := ssn.Jobs[task.Job]; found {
		if err := job.UpdateTaskStatus(task, api.Binding); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Binding, ssn.UID, err)
			return err
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
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
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, ssn.UID, err)
			return err
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			reclaimee.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s", reclaimee.Job)
	}

	// Update task in node.
	if node, found := ssn.Nodes[reclaimee.NodeName]; found {
		if err := node.UpdateTask(reclaimee); err != nil {
			glog.Errorf("Failed to update task <%v/%v> in Session <%v>: %v",
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

// UpdateJobCondition update job condition accordingly.
func (ssn *Session) UpdateJobCondition(jobInfo *api.JobInfo, cond *v1alpha1.PodGroupCondition) error {
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
