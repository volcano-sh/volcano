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

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/cache"
)

type Session struct {
	ID types.UID

	cache cache.Cache

	Jobs      []*api.JobInfo
	JobIndex  map[api.JobID]*api.JobInfo
	Nodes     []*api.NodeInfo
	NodeIndex map[string]*api.NodeInfo
	Backlog   []*api.JobInfo

	plugins       []Plugin
	eventHandlers []*EventHandler
	jobOrderFns   []api.CompareFn
	taskOrderFns  []api.CompareFn
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		ID:        uuid.NewUUID(),
		cache:     cache,
		JobIndex:  map[api.JobID]*api.JobInfo{},
		NodeIndex: map[string]*api.NodeInfo{},
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
}

func (ssn *Session) Bind(task *api.TaskInfo, hostname string) error {
	if err := ssn.cache.Bind(task, hostname); err != nil {
		return err
	}

	// Update status in session
	if job, found := ssn.JobIndex[task.Job]; found {
		job.UpdateTaskStatus(task, api.Binding)
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.ID)
	}

	if node, found := ssn.NodeIndex[hostname]; found {
		node.AddTask(task)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, ssn.ID)
	}

	// Callbacks
	for _, eh := range ssn.eventHandlers {
		eh.BindFunc(&Event{
			Task: task,
		})
	}

	return nil
}

func (ssn *Session) Evict(task *api.TaskInfo) error {
	return fmt.Errorf("not supported")
}

func (ssn *Session) ForgetJob(job *api.JobInfo) error {
	for i, j := range ssn.Jobs {
		if j.UID == job.UID {
			ssn.Backlog = append(ssn.Backlog, j)
			ssn.Jobs[i] = ssn.Jobs[len(ssn.Jobs)-1]
			ssn.Jobs = ssn.Jobs[:len(ssn.Jobs)-1]
			// NOTES: did not update JobIndex, the index is used for both
			// backlog & jobs.
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

func (ssn *Session) TaskOrderFn(l, r interface{}) bool {
	for _, tof := range ssn.taskOrderFns {
		if j := tof(l, r); j != 0 {
			return j < 0
		}
	}

	// If no task order funcs, order task by UID.
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)

	return lv.UID < rv.UID
}
