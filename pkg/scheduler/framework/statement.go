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

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// Operation type
type Operation int8

const (
	// Evict op
	Evict = iota
	// Pipeline op
	Pipeline
	// Allocate op
	Allocate
)

type operation struct {
	name   Operation
	task   *api.TaskInfo
	reason string
}

// Statement structure
type Statement struct {
	operations []operation
	ssn        *Session
}

// NewStatement returns new statement object
func NewStatement(ssn *Session) *Statement {
	return &Statement{
		ssn: ssn,
	}
}

// Evict the pod
func (s *Statement) Evict(reclaimee *api.TaskInfo, reason string) error {
	// Update status in session
	if job, found := s.ssn.Jobs[reclaimee.Job]; found {
		if err := job.UpdateTaskStatus(reclaimee, api.Releasing); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when evicting in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, s.ssn.UID, err)
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when evicting.",
			reclaimee.Job, s.ssn.UID)
	}

	// Update task in node.
	if node, found := s.ssn.Nodes[reclaimee.NodeName]; found {
		err := node.UpdateTask(reclaimee)
		if err != nil {
			klog.Errorf("Failed to update task <%v/%v> in node %v for: %s",
				reclaimee.Namespace, reclaimee.Name, reclaimee.NodeName, err.Error())
			return err
		}
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: reclaimee,
			})
		}
	}

	s.operations = append(s.operations, operation{
		name:   Evict,
		task:   reclaimee,
		reason: reason,
	})

	return nil
}

func (s *Statement) evict(reclaimee *api.TaskInfo, reason string) error {
	if err := s.ssn.cache.Evict(reclaimee, reason); err != nil {
		if e := s.unevict(reclaimee); e != nil {
			klog.Errorf("Faled to unevict task <%v/%v>: %v.", reclaimee.Namespace, reclaimee.Name, e)
		}
		return err
	}

	return nil
}

func (s *Statement) unevict(reclaimee *api.TaskInfo) error {
	// Update status in session
	job, found := s.ssn.Jobs[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Running); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when unevicting in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Running, s.ssn.UID, err)
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when unevicting.",
			reclaimee.Job, s.ssn.UID)
	}

	// Update task in node.
	if node, found := s.ssn.Nodes[reclaimee.NodeName]; found {
		err := node.UpdateTask(reclaimee)
		if err != nil {
			klog.Errorf("Failed to update task <%v/%v> in node %v for: %s",
				reclaimee.Namespace, reclaimee.Name, reclaimee.NodeName, err.Error())
			return err
		}
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

// Pipeline the task for the node
func (s *Statement) Pipeline(task *api.TaskInfo, hostname string, evictionOccurred bool) error {
	errInfos := make([]error, 0)
	job, found := s.ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pipelined); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when pipeline in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, s.ssn.UID, err)
			errInfos = append(errInfos, err)
		}
	} else {
		err := fmt.Errorf("Failed to find Job <%s> in Session <%s> index when pipeline.",
			task.Job, s.ssn.UID)
		klog.Errorf("%v", err)
		errInfos = append(errInfos, err)
	}

	task.NodeName = hostname
	task.EvictionOccurred = evictionOccurred

	if node, found := s.ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			klog.Errorf("Failed to add task <%v/%v> to node <%v> when pipeline in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.ssn.UID, err)
			errInfos = append(errInfos, err)
		}
		klog.V(3).Infof("After pipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		err := fmt.Errorf("Failed to find Node <%s> in Session <%s> index when pipeline.",
			hostname, s.ssn.UID)
		klog.Errorf("%v", err)
		errInfos = append(errInfos, err)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eventInfo := &Event{
				Task: task,
			}
			eh.AllocateFunc(eventInfo)
			if eventInfo.Err != nil {
				klog.Errorf("Failed to exec allocate callback functions for task <%v/%v> to node <%v> when pipeline in Session <%v>: %v",
					task.Namespace, task.Name, hostname, s.ssn.UID, eventInfo.Err)
				errInfos = append(errInfos, eventInfo.Err)
			}
		}
	}

	if len(errInfos) != 0 {
		return fmt.Errorf("Task(%s/%s) pipeline to node(%s) error and errInfos num is %d, UnPipeline will be called later to roll back the resources and status of the task.",
			task.Namespace, task.Name, hostname, len(errInfos))
	} else {
		s.operations = append(s.operations, operation{
			name: Pipeline,
			task: task,
		})
	}

	return nil
}

func (s *Statement) pipeline(task *api.TaskInfo) {
}

func (s *Statement) UnPipeline(task *api.TaskInfo) error {
	job, found := s.ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pending); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when unpipeline in Session <%v>: %v",
				task.Namespace, task.Name, api.Pending, s.ssn.UID, err)
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when unpipeline.", task.Job, s.ssn.UID)
	}

	if node, found := s.ssn.Nodes[task.NodeName]; found {
		if err := node.RemoveTask(task); err != nil {
			klog.Errorf("Failed to remove task <%v/%v> to node <%v> when unpipeline in Session <%v>: %v",
				task.Namespace, task.Name, task.NodeName, s.ssn.UID, err)
		}
		klog.V(3).Infof("After unpipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index when unpipeline.",
			task.NodeName, s.ssn.UID)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eventInfo := &Event{
				Task: task,
			}
			eh.DeallocateFunc(eventInfo)
			if eventInfo.Err != nil {
				klog.Errorf("Failed to exec deallocate callback functions for task <%v/%v> to node <%v> when pipeline in Session <%v>: %v",
					task.Namespace, task.Name, task.NodeName, s.ssn.UID, eventInfo.Err)
			}
		}
	}
	task.NodeName = ""

	return nil
}

// Allocate the task to node
func (s *Statement) Allocate(task *api.TaskInfo, nodeInfo *api.NodeInfo) (err error) {
	errInfos := make([]error, 0)
	hostname := nodeInfo.Name

	podVolumes, err := s.ssn.cache.GetPodVolumes(task, nodeInfo.Node)
	if err != nil {
		klog.Errorf("Failed to get pod volume for task %v/%v on node %v when allocating in Session <%v>: %v",
			task.Namespace, task.Name, hostname, s.ssn.UID, err)
		errInfos = append(errInfos, err)
	}

	if err := s.ssn.cache.AllocateVolumes(task, hostname, podVolumes); err != nil {
		klog.Errorf("Failed to allocate volume for task %v/%v on node %v when allocating in Session <%v>: %v",
			task.Namespace, task.Name, hostname, s.ssn.UID, err)
		errInfos = append(errInfos, err)
	}

	task.Pod.Spec.NodeName = hostname
	task.PodVolumes = podVolumes

	// Only update status in session
	job, found := s.ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when allocating in Session <%v>: %v",
				task.Namespace, task.Name, api.Allocated, s.ssn.UID, err)
			errInfos = append(errInfos, err)
		}
	} else {
		err := fmt.Errorf("Failed to find Job <%s> in Session <%s> index when allocating.",
			task.Job, s.ssn.UID)
		klog.Errorf("%v", err)
		errInfos = append(errInfos, err)
	}

	task.NodeName = hostname
	if node, found := s.ssn.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			klog.Errorf("Failed to add task <%v/%v> to node <%v> when allocating in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.ssn.UID, err)
			errInfos = append(errInfos, err)
		}
		klog.V(3).Infof("After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		err := fmt.Errorf("Failed to find Node <%s> in Session <%s> index when allocating.",
			hostname, s.ssn.UID)
		klog.Errorf("%v", err)
		errInfos = append(errInfos, err)
	}

	// Callbacks
	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eventInfo := &Event{
				Task: task,
			}
			eh.AllocateFunc(eventInfo)
			if eventInfo.Err != nil {
				klog.Errorf("Failed to exec allocate callback functions for task <%v/%v> to node <%v> when allocating in Session <%v>: %v",
					task.Namespace, task.Name, hostname, s.ssn.UID, eventInfo.Err)
				errInfos = append(errInfos, eventInfo.Err)
			}
		}
	}

	if len(errInfos) != 0 {
		return fmt.Errorf("Task %s/%s allocate to node %s error and errInfos num is %d, UnAllocate will be called later to roll back the resources and status of the task.",
			task.Namespace, task.Name, hostname, len(errInfos))
	} else {
		// Update status in session
		klog.V(3).Info("Allocating operations ...")
		s.operations = append(s.operations, operation{
			name: Allocate,
			task: task,
		})
	}

	return nil
}

// UnAllocate the pod for task
func (s *Statement) UnAllocate(task *api.TaskInfo) error {
	return s.unallocate(task)
}

func (s *Statement) allocate(task *api.TaskInfo) error {
	if err := s.ssn.cache.AddBindTask(task); err != nil {
		return err
	}

	if job, found := s.ssn.Jobs[task.Job]; found {
		if err := job.UpdateTaskStatus(task, api.Binding); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when binding in Session <%v>: %v",
				task.Namespace, task.Name, api.Binding, s.ssn.UID, err)
			return err
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			task.Job, s.ssn.UID)
		return fmt.Errorf("failed to find job %s", task.Job)
	}

	metrics.UpdateTaskScheduleDuration(metrics.Duration(task.Pod.CreationTimestamp.Time))
	return nil
}

// unallocate the pod for task
func (s *Statement) unallocate(task *api.TaskInfo) error {
	s.ssn.cache.RevertVolumes(task, task.PodVolumes)

	// Update status in session
	job, found := s.ssn.Jobs[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pending); err != nil {
			klog.Errorf("Failed to update task <%v/%v> status to %v when unallocating in Session <%v>: %v",
				task.Namespace, task.Name, api.Pending, s.ssn.UID, err)
		}
	} else {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index when unallocating.",
			task.Job, s.ssn.UID)
	}

	if node, found := s.ssn.Nodes[task.NodeName]; found {
		klog.V(3).Infof("Remove Task <%v> on node <%v>", task.Name, task.NodeName)
		err := node.RemoveTask(task)
		if err != nil {
			klog.Errorf("Failed to remove Task <%v> on node <%v> when unallocating: %s", task.Name, task.NodeName, err.Error())
		}
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: task,
			})
		}
	}
	task.NodeName = ""

	return nil
}

// Discard operation for evict, pipeline and allocate
func (s *Statement) Discard() {
	klog.V(3).Info("Discarding operations ...")
	for i := len(s.operations) - 1; i >= 0; i-- {
		op := s.operations[i]
		op.task.GenerateLastTxContext()
		switch op.name {
		case Evict:
			err := s.unevict(op.task)
			if err != nil {
				klog.Errorf("Failed to unevict task: %s", err.Error())
			}
		case Pipeline:
			err := s.UnPipeline(op.task)
			if err != nil {
				klog.Errorf("Failed to unpipeline task: %s", err.Error())
			}
		case Allocate:
			err := s.unallocate(op.task)
			if err != nil {
				klog.Errorf("Failed to unallocate task: %s", err.Error())
			}
		}
	}
}

// Commit operation for evict and pipeline
func (s *Statement) Commit() {
	klog.V(3).Info("Committing operations ...")
	for _, op := range s.operations {
		op.task.ClearLastTxContext()
		switch op.name {
		case Evict:
			err := s.evict(op.task, op.reason)
			if err != nil {
				klog.Errorf("Failed to evict task: %s", err.Error())
			}
		case Pipeline:
			s.pipeline(op.task)
		case Allocate:
			err := s.allocate(op.task)
			if err != nil {
				if e := s.unallocate(op.task); e != nil {
					klog.Errorf("Failed to unallocate task <%v/%v>: %v.", op.task.Namespace, op.task.Name, e)
				}
				klog.Errorf("Failed to allocate task <%v/%v>: %v.", op.task.Namespace, op.task.Name, err)
			}
		}
	}
}

func (s *Statement) GetEvictTasks() (tasks []*api.TaskInfo) {
	for _, op := range s.operations {
		if op.name == Evict {
			tasks = append(tasks, op.task)
		}
	}
	return
}
