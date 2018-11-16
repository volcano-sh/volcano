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

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/cache"
)

type Session struct {
	UID types.UID

	cache cache.Cache

	Jobs      []*api.JobInfo
	JobIndex  map[api.JobID]*api.JobInfo
	Nodes     []*api.NodeInfo
	NodeIndex map[string]*api.NodeInfo
	Backlog   []*api.JobInfo

	plugins        []Plugin
	eventHandlers  []*EventHandler
	jobOrderFns    []api.CompareFn
	taskOrderFns   []api.CompareFn
	preemptableFns []api.LessFn
	jobReadyFns    []api.ValidateFn
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		UID:       uuid.NewUUID(),
		cache:     cache,
		JobIndex:  map[api.JobID]*api.JobInfo{},
		NodeIndex: map[string]*api.NodeInfo{},
	}

	glog.V(3).Infof("Open Session %v", ssn.UID)

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		ssn.JobIndex[job.UID] = job
	}

	ssn.Nodes = snapshot.Nodes
	for _, node := range ssn.Nodes {
		ssn.NodeIndex[node.Name] = node
	}

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

	glog.V(3).Infof("Close Session %v", ssn.UID)
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
		if err := node.PipelineTask(task); err != nil {
			glog.Errorf("Failed to pipeline task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
		}
		glog.V(3).Infof("After pipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
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
			ssn.dispatch(task)
		}
	}

	return nil
}

func (ssn *Session) dispatch(task *api.TaskInfo) error {
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

func (ssn *Session) Preemptable(preemptor, preemptee *api.TaskInfo) bool {
	if len(ssn.preemptableFns) == 0 {
		return false
	}

	for _, preemptable := range ssn.preemptableFns {
		if !preemptable(preemptor, preemptee) {
			return false
		}
	}

	return true
}

func (ssn *Session) Preempt(preemptor, preemptee *api.TaskInfo) error {
	if err := ssn.cache.Evict(preemptee); err != nil {
		return err
	}

	// Update status in session
	job, found := ssn.JobIndex[preemptee.Job]
	if found {
		if err := job.UpdateTaskStatus(preemptee, api.Releasing); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				preemptee.Namespace, preemptee.Name, api.Releasing, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			preemptee.Job, ssn.UID)
	}

	// Update task in node.
	if node, found := ssn.NodeIndex[preemptee.NodeName]; found {
		node.UpdateTask(preemptee)
	}

	for _, eh := range ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: preemptor,
			})
		}

		if eh.EvictFunc != nil {
			eh.EvictFunc(&Event{
				Task: preemptee,
			})
		}
	}

	return nil
}

func (ssn *Session) AddEventHandler(eh *EventHandler) {
	ssn.eventHandlers = append(ssn.eventHandlers, eh)
}

func (ssn *Session) AddJobOrderFn(cf api.CompareFn) {
	ssn.jobOrderFns = append(ssn.jobOrderFns, cf)
}

func (ssn *Session) AddTaskOrderFn(cf api.CompareFn) {
	ssn.taskOrderFns = append(ssn.taskOrderFns, cf)
}

func (ssn *Session) AddPreemptableFn(cf api.LessFn) {
	ssn.preemptableFns = append(ssn.preemptableFns, cf)
}

func (ssn *Session) AddJobReadyFn(vf api.ValidateFn) {
	ssn.jobReadyFns = append(ssn.jobReadyFns, vf)
}

func (ssn *Session) JobReady(obj interface{}) bool {
	for _, jrf := range ssn.jobReadyFns {
		if !jrf(obj) {
			return false
		}
	}

	return true
}

func (ssn *Session) JobOrderFn(l, r interface{}) bool {
	for _, jof := range ssn.jobOrderFns {
		if j := jof(l, r); j != 0 {
			return j < 0
		}
	}

	// If no job order funcs, order job by UID.
	lv := l.(*api.JobInfo)
	rv := r.(*api.JobInfo)

	return lv.UID < rv.UID
}

func (ssn *Session) TaskCompareFns(l, r interface{}) int {
	for _, tof := range ssn.taskOrderFns {
		if j := tof(l, r); j != 0 {
			return j
		}
	}

	return 0
}

func (ssn *Session) TaskOrderFn(l, r interface{}) bool {
	if res := ssn.TaskCompareFns(l, r); res != 0 {
		return res < 0
	}

	// If no task order funcs, order task by UID.
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)

	return lv.UID < rv.UID
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
