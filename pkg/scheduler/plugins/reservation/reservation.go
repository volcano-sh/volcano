/*
Copyright 2020 The Kubernetes Authors.

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

package reservation

import (
	"time"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// PluginName defines the name of the plugin
const PluginName = "reservation"

type reservationPlugin struct {
	pluginArguments framework.Arguments
}

// New returns the plugin instance
func New(arguments framework.Arguments) framework.Plugin {
	return &reservationPlugin{pluginArguments: arguments}
}

func (rp *reservationPlugin) Name() string {
	return PluginName
}

func (rp *reservationPlugin) OnSessionOpen(ssn *framework.Session) {
	// select the job which has the highest priority and waits for the longest duration
	targetJobFn := func(jobs []*api.JobInfo) *api.JobInfo {
		if len(jobs) == 0 {
			return nil
		}
		priority := rp.getHighestPriority(jobs)
		highestPriorityJobs := rp.getHighestPriorityJobs(priority, jobs)
		return rp.getTargetJob(highestPriorityJobs)
	}
	ssn.AddTargetJobFn(rp.Name(), targetJobFn)

	reservedNodesFn := func() {
		node := rp.getUnlockedNodesWithMaxIdle(ssn)
		if node != nil {
			util.Reservation.LockedNodes[node.Name] = node
			klog.V(3).Infof("locked node: %s", node.Name)
		}
	}
	ssn.AddReservedNodesFn(rp.Name(), reservedNodesFn)
}

func (rp *reservationPlugin) OnSessionClose(ssn *framework.Session) {}

func (rp *reservationPlugin) getHighestPriority(jobs []*api.JobInfo) int32 {
	if len(jobs) == 0 {
		return -1
	}
	highestPriority := jobs[0].Priority
	for _, job := range jobs {
		if job.Priority > highestPriority {
			highestPriority = job.Priority
		}
	}
	return highestPriority
}

func (rp *reservationPlugin) getHighestPriorityJobs(priority int32, jobs []*api.JobInfo) []*api.JobInfo {
	var highestPriorityJobs []*api.JobInfo
	if len(jobs) == 0 {
		return highestPriorityJobs
	}
	for _, job := range jobs {
		if job.Priority == priority {
			highestPriorityJobs = append(highestPriorityJobs, job)
		}
	}
	return highestPriorityJobs
}

func (rp *reservationPlugin) getTargetJob(jobs []*api.JobInfo) *api.JobInfo {
	if len(jobs) == 0 {
		return nil
	}
	maxWaitDuration := rp.getJobWaitingTime(jobs[0])
	targetJob := jobs[0]
	for _, job := range jobs {
		waitDuration := rp.getJobWaitingTime(job)
		if waitDuration > maxWaitDuration {
			maxWaitDuration = waitDuration
			targetJob = job
		}
	}
	klog.V(3).Infof("Target job ID: %s, Name: %s", targetJob.UID, targetJob.Name)
	return targetJob
}

func (rp *reservationPlugin) getJobWaitingTime(job *api.JobInfo) time.Duration {
	if job == nil {
		return -1
	}
	now := time.Now()
	return now.Sub(job.ScheduleStartTimestamp.Time)
}

// select the node which has not been locked and idle resource is the most as the locked node
func (rp *reservationPlugin) getUnlockedNodesWithMaxIdle(ssn *framework.Session) *api.NodeInfo {
	var maxIdleNode *api.NodeInfo
	for _, node := range ssn.Nodes {
		hasLocked := false
		for _, lockedNode := range util.Reservation.LockedNodes {
			if node.Node.UID == lockedNode.Node.UID {
				hasLocked = true
				break
			}
		}
		if !hasLocked && (maxIdleNode == nil || maxIdleNode.Idle.LessEqual(node.Idle, api.Zero)) {
			maxIdleNode = node
		}
	}
	if maxIdleNode != nil {
		klog.V(3).Infof("Max idle node: %s:", maxIdleNode.Name)
	} else {
		klog.V(3).Info("Max idle node: nil")
	}

	return maxIdleNode
}
