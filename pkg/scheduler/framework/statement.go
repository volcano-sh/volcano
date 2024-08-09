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
	if err := s.updateTaskStatus(reclaimee, api.Releasing); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when evicting: %v",
			reclaimee.Namespace, reclaimee.Name, api.Releasing, s.ssn.UID, err)
	}

	err := s.updateTaskInNode(reclaimee)
	if err != nil {
		return err
	}
	s.handleEvent(reclaimee, false)
	s.addOperation(Evict, reclaimee, reason)

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
	if err := s.updateTaskStatus(reclaimee, api.Running); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when unevicting: %v",
			reclaimee.Namespace, reclaimee.Name, api.Running, s.ssn.UID, err)
	}

	err := s.updateTaskInNode(reclaimee)
	if err != nil {
		return err
	}
	s.handleEvent(reclaimee, true)

	return nil
}

// Pipeline the task for the node
func (s *Statement) Pipeline(task *api.TaskInfo, hostname string) error {
	if err := s.updateTaskStatus(task, api.Pipelined); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when pipeline: %v",
			task.Namespace, task.Name, api.Pipelined, s.ssn.UID, err)
	}

	task.NodeName = hostname

	err := s.addTaskToNode(task, hostname)
	if err != nil {
		klog.Errorf("Failed to addTask to Node <%s> in Session <%s> when pipeline.", hostname, s.ssn.UID)
	}
	s.handleEvent(task, true)
	s.addOperation(Pipeline, task, "")

	return nil
}

func (s *Statement) pipeline(task *api.TaskInfo) {
}

func (s *Statement) UnPipeline(task *api.TaskInfo) error {
	if err := s.updateTaskStatus(task, api.Pending); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when unpipeline: %v",
			task.Namespace, task.Name, api.Pending, s.ssn.UID, err)
	}

	err := s.removeTaskFromNode(task)
	if err != nil {
		klog.Errorf("Failed to remove Task <%v> on node <%v> when unpipeline: %s", task.Name, task.NodeName, err.Error())
	}
	s.handleEvent(task, false)
	task.NodeName = ""

	return nil
}

// Allocate the task to node
func (s *Statement) Allocate(task *api.TaskInfo, nodeInfo *api.NodeInfo) (err error) {
	podVolumes, err := s.ssn.cache.GetPodVolumes(task, nodeInfo.Node)
	if err != nil {
		return err
	}

	hostname := nodeInfo.Name
	if err := s.ssn.cache.AllocateVolumes(task, hostname, podVolumes); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.ssn.cache.RevertVolumes(task, podVolumes)
		}
	}()

	task.Pod.Spec.NodeName = hostname
	task.PodVolumes = podVolumes

	// Only update status in session
	if err := s.updateTaskStatus(task, api.Allocated); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when allocating: %v",
			task.Namespace, task.Name, api.Allocated, s.ssn.UID, err)
		return err
	}

	task.NodeName = hostname
	err = s.addTaskToNode(task, hostname)
	if err != nil {
		klog.Errorf("Failed to addTask to Node <%s> in Session <%s> when allocating.", hostname, s.ssn.UID)
		return err
	}

	// Callbacks
	s.handleEvent(task, true)

	// Update status in session
	klog.V(3).Info("Allocating operations ...")
	s.addOperation(Allocate, task, "")

	return nil
}

func (s *Statement) allocate(task *api.TaskInfo) error {
	if err := s.ssn.cache.AddBindTask(task); err != nil {
		return err
	}

	if err := s.updateTaskStatus(task, api.Binding); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when binding: %v",
			task.Namespace, task.Name, api.Binding, s.ssn.UID, err)
		return err
	}

	metrics.UpdateTaskScheduleDuration(metrics.Duration(task.Pod.CreationTimestamp.Time))
	return nil
}

// unallocate the pod for task
func (s *Statement) unallocate(task *api.TaskInfo) error {
	s.ssn.cache.RevertVolumes(task, task.PodVolumes)

	if err := s.updateTaskStatus(task, api.Pending); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v> when unallocating: %v",
			task.Namespace, task.Name, api.Pending, s.ssn.UID, err)
	}

	err := s.removeTaskFromNode(task)
	if err != nil {
		klog.Errorf("Failed to remove Task <%v> on node <%v> when unallocating: %s", task.Name, task.NodeName, err.Error())
	}

	s.handleEvent(task, false)
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

// Update task status in session
func (s *Statement) updateTaskStatus(task *api.TaskInfo, status api.TaskStatus) error {
	job, found := s.ssn.Jobs[task.Job]
	if !found {
		klog.Errorf("Failed to find Job <%s> in Session <%s> index.", task.Job, s.ssn.UID)
		return fmt.Errorf("failed to find Job <%s> in Session <%s> index", task.Job, s.ssn.UID)
	}
	if err := job.UpdateTaskStatus(task, status); err != nil {
		klog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
			task.Namespace, task.Name, status, s.ssn.UID, err)
		return err
	}
	return nil
}

// Handle event for task AllocateEvent/DeallocateEvent
func (s *Statement) handleEvent(task *api.TaskInfo, isAllocate bool) {
	for _, eh := range s.ssn.eventHandlers {
		if eh != nil {
			if isAllocate && eh.AllocateFunc != nil {
				eh.AllocateFunc(&Event{
					Task: task,
				})
			} else if !isAllocate && eh.DeallocateFunc != nil {
				eh.DeallocateFunc(&Event{
					Task: task,
				})
			}
		}
	}
}

// Update task in node.
func (s *Statement) updateTaskInNode(task *api.TaskInfo) error {
	if node, found := s.ssn.Nodes[task.NodeName]; found {
		err := node.UpdateTask(task)
		if err != nil {
			klog.Errorf("Failed to update task <%v/%v> in node %v: %v",
				task.Namespace, task.Name, task.NodeName, err.Error())
			return err
		}
	}
	return nil
}

// Add task in node.
func (s *Statement) addTaskToNode(task *api.TaskInfo, hostname string) error {
	node, found := s.ssn.Nodes[hostname]
	if !found {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index.", hostname, s.ssn.UID)
		return fmt.Errorf("failed to find node %s", hostname)
	}

	if err := node.AddTask(task); err != nil {
		klog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
			task.Namespace, task.Name, hostname, s.ssn.UID, err)
		return err
	}
	klog.V(3).Infof("Add Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
		task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	return nil
}

// Remove task in node.
func (s *Statement) removeTaskFromNode(task *api.TaskInfo) error {
	node, found := s.ssn.Nodes[task.NodeName]
	if !found {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index.", task.NodeName, s.ssn.UID)
		return fmt.Errorf("failed to find node %s", task.NodeName)
	}

	if err := node.RemoveTask(task); err != nil {
		klog.Errorf("Failed to remove task <%v/%v> from node <%v> in Session <%v>: %v",
			task.Namespace, task.Name, task.NodeName, s.ssn.UID, err)
		return err
	}

	klog.V(3).Infof("Remove Task <%v/%v> from Node <%v>: idle <%v>, used <%v>, releasing <%v>",
		task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)

	return nil
}

func (s *Statement) addOperation(name Operation, task *api.TaskInfo, reason string) {
	s.operations = append(s.operations, operation{
		name:   name,
		task:   task,
		reason: reason,
	})
}
