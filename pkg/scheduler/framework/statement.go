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
	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

type Statement struct {
	operations []operation
	ssn        *Session
}

type operation struct {
	name string
	args []interface{}
}

func (s *Statement) Evict(reclaimee *api.TaskInfo, reason string) error {
	// Update status in session
	job, found := s.ssn.JobIndex[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Releasing); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, s.ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			reclaimee.Job, s.ssn.UID)
	}

	// Update task in node.
	if node, found := s.ssn.NodeIndex[reclaimee.NodeName]; found {
		node.UpdateTask(reclaimee)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: reclaimee,
			})
		}
	}

	s.operations = append(s.operations, operation{
		name: "evict",
		args: []interface{}{reclaimee, reason},
	})

	return nil
}

func (s *Statement) evict(reclaimee *api.TaskInfo, reason string) error {
	if err := s.ssn.cache.Evict(reclaimee, reason); err != nil {
		if e := s.unevict(reclaimee, reason); err != nil {
			glog.Errorf("Faled to unevict task <%v/%v>: %v.",
				reclaimee.Namespace, reclaimee.Name, e)
		}
		return err
	}

	return nil
}

func (s *Statement) unevict(reclaimee *api.TaskInfo, reason string) error {
	// Update status in session
	job, found := s.ssn.JobIndex[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Running); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, s.ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			reclaimee.Job, s.ssn.UID)
	}

	// Update task in node.
	if node, found := s.ssn.NodeIndex[reclaimee.NodeName]; found {
		node.AddTask(reclaimee)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: reclaimee,
			})
		}
	}

	return nil
}

func (s *Statement) Pipeline(task *api.TaskInfo, hostname string) error {
	// Only update status in session
	job, found := s.ssn.JobIndex[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pipelined); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, s.ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, s.ssn.UID)
	}

	task.NodeName = hostname

	if node, found := s.ssn.NodeIndex[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to pipeline task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.ssn.UID, err)
		}
		glog.V(3).Infof("After pipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, s.ssn.UID)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	s.operations = append(s.operations, operation{
		name: "pipeline",
		args: []interface{}{task, hostname},
	})

	return nil
}

func (s *Statement) pipeline(task *api.TaskInfo) {
}

func (s *Statement) unpipeline(task *api.TaskInfo) error {
	// Only update status in session
	job, found := s.ssn.JobIndex[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pending); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, s.ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, s.ssn.UID)
	}

	hostname := task.NodeName

	if node, found := s.ssn.NodeIndex[hostname]; found {
		if err := node.RemoveTask(task); err != nil {
			glog.Errorf("Failed to pipeline task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.ssn.UID, err)
		}
		glog.V(3).Infof("After pipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, s.ssn.UID)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: task,
			})
		}
	}

	return nil
}

func (s *Statement) Discard() {
	glog.V(3).Info("Discarding operations ...")
	for i := len(s.operations) - 1; i >= 0; i-- {
		op := s.operations[i]
		switch op.name {
		case "evict":
			s.unevict(op.args[0].(*api.TaskInfo), op.args[1].(string))
		case "pipeline":
			s.unpipeline(op.args[0].(*api.TaskInfo))
		}
	}
}

func (s *Statement) Commit() {
	glog.V(3).Info("Committing operations ...")
	for _, op := range s.operations {
		switch op.name {
		case "evict":
			s.evict(op.args[0].(*api.TaskInfo), op.args[1].(string))
		case "pipeline":
			s.pipeline(op.args[0].(*api.TaskInfo))
		}
	}
}
