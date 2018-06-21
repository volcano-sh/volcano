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

		start := 0
		for status, tasks := range job.TaskStatusIndex {
			// Also include succeeded task.
			if api.OccupiedResources(status) || status == api.Succeeded {
				start = start + len(tasks)
			}
		}

		if job.MinAvailable < start {
			glog.V(3).Infof("QueueJob %v already starts enough Tasks (min %v, start %v).",
				job.Name, job.MinAvailable, start)
			continue
		}

		if tasks.Len() < job.MinAvailable-start {
			glog.V(3).Infof("Not enough pending tasks %v in QueueJob %v to start (min %v, occupied %v).",
				tasks.Len(), job.Name, job.MinAvailable, start)
			continue
		}

		binds := map[api.TaskID]string{}
		allocates := map[string]*api.Resource{}

		glog.V(3).Infof("Try to allocate resource to <%d> Tasks of Job <%s:%s>",
			job.MinAvailable-start, job.UID, job.Name)

		for ; start < job.MinAvailable; start++ {
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

				glog.V(3).Infof("Considering Task <%v/%v> on node <%v>: <%v> vs. <%v>",
					task.Job, task.UID, node.Name, task.Resreq, currentIdle)

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
		if start >= job.MinAvailable {
			for taskID, host := range binds {
				task := job.Tasks[taskID]
				ssn.Bind(task, host)
				glog.V(3).Infof("Bind task <%v/%v> to host <%v>",
					task.Namespace, task.Name, host)
			}
		} else {
			// If job can not get enough resource, forget it for following
			// actions.
			ssn.ForgetJob(job)
		}
	}
}

func (alloc *garanteeAction) UnInitialize() {}
