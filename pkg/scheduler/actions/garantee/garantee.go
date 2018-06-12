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

package garantee

import (
	"github.com/golang/glog"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/framework"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/util"
)

type garanteeAction struct {
	ssn *framework.Session
}

func New() *garanteeAction {
	return &garanteeAction{}
}

func (alloc *garanteeAction) Name() string {
	return "garantee"
}

func (alloc *garanteeAction) Initialize() {}

func (alloc *garanteeAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Garantee ...")
	defer glog.V(3).Infof("Leaving Garantee ...")

	jobs := ssn.Jobs

	for _, job := range jobs {
		if len(job.TaskStatusIndex[api.Pending]) == 0 {
			glog.V(3).Infof("No pending tasks in Job <%v>", job.UID)
			continue
		}

		tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
		for _, task := range job.TaskStatusIndex[api.Pending] {
			tasks.Push(task)
		}

		occupied := 0
		for status, tasks := range job.TaskStatusIndex {
			if api.OccupiedResources(status) {
				occupied = occupied + len(tasks)
			}
		}

		if tasks.Len() < job.MinAvailable-occupied {
			glog.V(3).Infof("Not enough pending tasks %v in QueueJob %v to start (min %v, occupied %v).",
				tasks.Len(), job.Name, job.MinAvailable, occupied)
			continue
		}

		binds := map[api.TaskID]string{}
		allocates := map[string]*api.Resource{}

		glog.V(3).Infof("Try to allocate resource to <%d> Tasks of Job <%s>",
			job.MinAvailable-occupied, job.UID)

		for ; occupied < job.MinAvailable; occupied++ {
			task := tasks.Pop().(*api.TaskInfo)

			if task == nil {
				break
			}

			assigned := false

			nodes := job.Candidates
			// If candidate list is nil, it means all nodes.
			if job.Candidates == nil {
				nodes = ssn.Nodes
			}

			for _, node := range nodes {
				currentIdle := node.Idle.Clone()

				if alloc, found := allocates[node.Name]; found {
					currentIdle.Sub(alloc)
				}

				if task.Resreq.LessEqual(currentIdle) {
					binds[task.UID] = node.Name
					if _, found := allocates[node.Name]; !found {
						allocates[node.Name] = api.EmptyResource()
					}
					allocates[node.Name].Add(task.Resreq)
					assigned = true
					break
				}
			}

			if !assigned {
				break
			}
		}

		// Got enough occupied, bind them all.
		if occupied >= job.MinAvailable {
			for taskID, host := range binds {
				task := job.Tasks[taskID]
				ssn.Bind(task, host)
			}
		} else {
			// If job can not get enough resource, forget it for following
			// actions.
			ssn.ForgetJob(job)
		}
	}
}

func (alloc *garanteeAction) UnInitialize() {}
