/*
Copyright 2017 The Kubernetes Authors.

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

package allocate

import (
	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/util"
)

type allocateAction struct {
	ssn *framework.Session
}

func New() *allocateAction {
	return &allocateAction{}
}

func (alloc *allocateAction) Name() string {
	return "allocate"
}

func (alloc *allocateAction) Initialize() {}

func (alloc *allocateAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Allocate ...")
	defer glog.V(3).Infof("Leaving Allocate ...")

	jobs := util.NewPriorityQueue(ssn.JobOrderFn)

	for _, job := range ssn.Jobs {
		jobs.Push(job)
	}

	glog.V(3).Infof("Try to allocate resource to %d Jobs", jobs.Len())

	pendingTasks := map[api.JobID]*util.PriorityQueue{}

	for {
		if jobs.Empty() {
			break
		}

		job := jobs.Pop().(*api.JobInfo)

		if _, found := pendingTasks[job.UID]; !found {
			tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				tasks.Push(task)
			}
			pendingTasks[job.UID] = tasks
		}
		tasks := pendingTasks[job.UID]

		glog.V(3).Infof("Try to allocate resource to %d tasks of Job <%v/%v>",
			tasks.Len(), job.Namespace, job.Name)

		for !tasks.Empty() {
			task := tasks.Pop().(*api.TaskInfo)

			assigned := false

			// If candidates is nil, it means all nodes.
			// If candidates is empty, it means none.
			nodes := job.Candidates
			if nodes == nil {
				nodes = ssn.Nodes
			}

			glog.V(3).Infof("There are <%d> nodes for Job <%v/%v>",
				len(nodes), job.Namespace, job.Name)

			for _, node := range nodes {
				glog.V(3).Infof("Considering Task <%v/%v> on node <%v>: <%v> vs. <%v>",
					task.Namespace, task.Name, node.Name, task.Resreq, node.Idle)
				// Allocate idle resource to the task.
				if task.Resreq.LessEqual(node.Idle) {
					glog.V(3).Infof("Binding Task <%v/%v> to node <%v>",
						task.Namespace, task.Name, node.Name)
					if err := ssn.Allocate(task, node.Name); err != nil {
						glog.Errorf("Failed to bind Task %v on %v in Session %v",
							task.UID, node.Name, ssn.UID)
						continue
					}
					assigned = true
					break
				}

				// Allocate releasing resource to the task if any.
				if task.Resreq.LessEqual(node.Releasing) {
					glog.V(3).Infof("Pipelining Task <%v/%v> to node <%v> for <%v> on <%v>",
						task.Namespace, task.Name, node.Name, task.Resreq, node.Releasing)
					if err := ssn.Pipeline(task, node.Name); err != nil {
						glog.Errorf("Failed to pipeline Task %v on %v in Session %v",
							task.UID, node.Name, ssn.UID)
						continue
					}

					assigned = true
					break
				}
			}

			if assigned {
				jobs.Push(job)
			}

			// Handle one pending task in each loop.
			break
		}
	}
}

func (alloc *allocateAction) UnInitialize() {}
