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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/conf"
)

type Session struct {
	UID types.UID

	cache cache.Cache

	Jobs       []*api.JobInfo
	JobIndex   map[api.JobID]*api.JobInfo
	Nodes      []*api.NodeInfo
	NodeIndex  map[string]*api.NodeInfo
	Queues     []*api.QueueInfo
	QueueIndex map[api.QueueID]*api.QueueInfo
	Others     []*api.TaskInfo
	Backlog    []*api.JobInfo
	Tiers      []conf.Tier

	plugins        map[string]Plugin
	eventHandlers  []*EventHandler
	jobOrderFns    map[string]api.CompareFn
	queueOrderFns  map[string]api.CompareFn
	taskOrderFns   map[string]api.CompareFn
	predicateFns   map[string]api.PredicateFn
	preemptableFns map[string]api.EvictableFn
	reclaimableFns map[string]api.EvictableFn
	overusedFns    map[string]api.ValidateFn
	jobReadyFns    map[string]api.ValidateFn
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		UID:        uuid.NewUUID(),
		cache:      cache,
		JobIndex:   map[api.JobID]*api.JobInfo{},
		NodeIndex:  map[string]*api.NodeInfo{},
		QueueIndex: map[api.QueueID]*api.QueueInfo{},

		plugins:        map[string]Plugin{},
		jobOrderFns:    map[string]api.CompareFn{},
		queueOrderFns:  map[string]api.CompareFn{},
		taskOrderFns:   map[string]api.CompareFn{},
		predicateFns:   map[string]api.PredicateFn{},
		preemptableFns: map[string]api.EvictableFn{},
		reclaimableFns: map[string]api.EvictableFn{},
		overusedFns:    map[string]api.ValidateFn{},
		jobReadyFns:    map[string]api.ValidateFn{},
	}

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		ssn.JobIndex[job.UID] = job
	}

	ssn.Nodes = snapshot.Nodes
	for _, node := range ssn.Nodes {
		ssn.NodeIndex[node.Name] = node
	}

	ssn.Queues = snapshot.Queues
	for _, queue := range ssn.Queues {
		ssn.QueueIndex[queue.UID] = queue
	}

	ssn.Others = snapshot.Others

	glog.V(3).Infof("Open Session %v with <%d> Job and <%d> Queues",
		ssn.UID, len(ssn.Jobs), len(ssn.Queues))

	return ssn
}

func closeSession(ssn *Session) {
	ssn.Jobs = nil
	ssn.JobIndex = nil
	ssn.Nodes = nil
	ssn.NodeIndex = nil
	ssn.Backlog = nil
	ssn.plugins = nil
	ssn.eventHandlers = nil
	ssn.jobOrderFns = nil
	ssn.queueOrderFns = nil

	glog.V(3).Infof("Close Session %v", ssn.UID)
}

func (ssn *Session) Statement() *Statement {
	return &Statement{
		ssn: ssn,
	}
}

func (ssn *Session) Pipeline(task *api.TaskInfo, hostname string) error {
	// Only update status in session
	job, found := ssn.JobIndex[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pipelined); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
	}

	task.NodeName = hostname

	if node, found := ssn.NodeIndex[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
		}
		glog.V(3).Infof("After added Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, ssn.UID)
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

func (ssn *Session) Allocate(task *api.TaskInfo, hostname string) error {
	if err := ssn.cache.AllocateVolumes(task, hostname); err != nil {
		return err
	}

	// Only update status in session
	job, found := ssn.JobIndex[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Allocated, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
	}

	task.NodeName = hostname

	if node, found := ssn.NodeIndex[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
		}
		glog.V(3).Infof("After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, ssn.UID)
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
	if job, found := ssn.JobIndex[task.Job]; found {
		if err := job.UpdateTaskStatus(task, api.Binding); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Binding, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
	}

	return nil
}

func (ssn *Session) Evict(reclaimee *api.TaskInfo, reason string) error {
	if err := ssn.cache.Evict(reclaimee, reason); err != nil {
		return err
	}

	// Update status in session
	job, found := ssn.JobIndex[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Releasing); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			reclaimee.Job, ssn.UID)
	}

	// Update task in node.
	if node, found := ssn.NodeIndex[reclaimee.NodeName]; found {
		if err := node.UpdateTask(reclaimee); err != nil {
			glog.Errorf("Failed to update task <%v/%v> in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, ssn.UID, err)
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

// Backoff discards a job from session, so no plugin/action handles it.
func (ssn *Session) Backoff(job *api.JobInfo, event kbv1.Event, reason string) error {
	jobErrMsg := job.FitError()

	// Update podCondition for tasks Allocated and Pending before job discarded
	for _, taskInfo := range job.TaskStatusIndex[api.Pending] {
		ssn.TaskUnschedulable(taskInfo, kbv1.FailedSchedulingEvent, jobErrMsg)
	}

	for _, taskInfo := range job.TaskStatusIndex[api.Allocated] {
		ssn.TaskUnschedulable(taskInfo, kbv1.FailedSchedulingEvent, jobErrMsg)
	}

	if err := ssn.cache.Backoff(job, event, reason); err != nil {
		glog.Errorf("Failed to backoff job <%s/%s>: %v",
			job.Namespace, job.Name, err)
		return err
	}

	glog.V(3).Infof("Discard Job <%s/%s> because %s",
		job.Namespace, job.Name, reason)

	// Delete Job from Session after recording event.
	delete(ssn.JobIndex, job.UID)
	for i, j := range ssn.Jobs {
		if j.UID == job.UID {
			ssn.Jobs[i] = ssn.Jobs[len(ssn.Jobs)-1]
			ssn.Jobs = ssn.Jobs[:len(ssn.Jobs)-1]
			break
		}
	}

	return nil
}

// TaskUnschedulable updates task status
func (ssn *Session) TaskUnschedulable(task *api.TaskInfo, event kbv1.Event, reason string) error {
	if err := ssn.cache.TaskUnschedulable(task, event, reason); err != nil {
		glog.Errorf("Failed to update unschedulable task status <%s/%s>: %v",
			task.Namespace, task.Name, err)
		return err
	}

	return nil
}

func (ssn *Session) AddEventHandler(eh *EventHandler) {
	ssn.eventHandlers = append(ssn.eventHandlers, eh)
}

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
