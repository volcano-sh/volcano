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

package cache

import (
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type podState struct {
	pod *v1.Pod
	// Used by assumedPod to determinate expiration.
	deadline *time.Time
	// Used to block cache from expiring assumedPod if binding still runs
	bindingFinished bool
}

type TaskInfo struct {
	UID       types.UID
	Owner     types.UID
	Name      string
	Namespace string

	NodeName string
	Status   v1.PodPhase
	Priority int32

	Pod *v1.Pod

	Resreq *Resource
}

func NewTaskInfo(pod *v1.Pod) *TaskInfo {
	req := EmptyResource()

	for _, c := range pod.Spec.Containers {
		req.Add(NewResource(c.Resources.Requests))
	}

	pi := &TaskInfo{
		UID:       pod.UID,
		Owner:     getPodOwner(pod),
		Name:      pod.Name,
		Namespace: pod.Namespace,
		NodeName:  pod.Spec.NodeName,
		Status:    pod.Status.Phase,
		Priority:  1,

		Pod:    pod,
		Resreq: req,
	}

	if pod.Spec.Priority != nil {
		pi.Priority = *pod.Spec.Priority
	}

	return pi
}

func (pi *TaskInfo) Clone() *TaskInfo {
	return &TaskInfo{
		UID:       pi.UID,
		Owner:     pi.Owner,
		Name:      pi.Name,
		Namespace: pi.Namespace,
		NodeName:  pi.NodeName,
		Status:    pi.Status,
		Priority:  pi.Priority,
		Pod:       pi.Pod,
		Resreq:    pi.Resreq.Clone(),
	}
}

type JobInfo struct {
	metav1.ObjectMeta

	PdbName      string
	MinAvailable int

	Allocated    *Resource
	TotalRequest *Resource

	Running  []*TaskInfo
	Pending  []*TaskInfo // The pending pod without NodeName
	Assigned []*TaskInfo // The pending pod with NodeName
	Others   []*TaskInfo

	NodeSelector map[string]string
}

func NewJobInfo(uid types.UID) *JobInfo {
	return &JobInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name: string(uid),
			UID:  uid,
		},
		PdbName:      "",
		MinAvailable: 0,
		Allocated:    EmptyResource(),
		TotalRequest: EmptyResource(),
		Running:      make([]*TaskInfo, 0),
		Pending:      make([]*TaskInfo, 0),
		Assigned:     make([]*TaskInfo, 0),
		Others:       make([]*TaskInfo, 0),
		NodeSelector: make(map[string]string),
	}
}

func (ps *JobInfo) AddTaskInfo(pi *TaskInfo) {
	switch pi.Status {
	case v1.PodRunning:
		ps.Running = append(ps.Running, pi)
		ps.Allocated.Add(pi.Resreq)
		ps.TotalRequest.Add(pi.Resreq)
	case v1.PodPending:
		// treat pending pod with NodeName as allocated
		if len(pi.Pod.Spec.NodeName) != 0 {
			ps.Allocated.Add(pi.Resreq)
			ps.Assigned = append(ps.Assigned, pi)
		} else {
			ps.Pending = append(ps.Pending, pi)
		}
		ps.TotalRequest.Add(pi.Resreq)
	default:
		ps.Others = append(ps.Others, pi)
	}

	// Update PodSet Labels
	// assume all pods in the same PodSet have same labels
	if len(ps.Labels) == 0 && len(pi.Pod.Labels) != 0 {
		ps.Labels = pi.Pod.Labels
	}

	// Update PodSet NodeSelector
	// assume all pods in the same PodSet have same NodeSelector
	if len(ps.NodeSelector) == 0 && len(pi.Pod.Spec.NodeSelector) != 0 {
		for k, v := range pi.Pod.Spec.NodeSelector {
			ps.NodeSelector[k] = v
		}
	}
}

func (ps *JobInfo) DeleteTaskInfo(pi *TaskInfo) {
	for index, piRunning := range ps.Running {
		if piRunning.Name == pi.Name {
			ps.Allocated.Sub(piRunning.Resreq)
			ps.TotalRequest.Sub(piRunning.Resreq)
			ps.Running = append(ps.Running[:index], ps.Running[index+1:]...)
			return
		}
	}

	for index, piPending := range ps.Pending {
		if piPending.Name == pi.Name {
			if len(piPending.Pod.Spec.NodeName) != 0 {
				ps.Allocated.Sub(piPending.Resreq)
			}
			ps.TotalRequest.Sub(piPending.Resreq)
			ps.Pending = append(ps.Pending[:index], ps.Pending[index+1:]...)
			return
		}
	}

	for index, piAssigned := range ps.Assigned {
		if piAssigned.Name == pi.Name {
			if len(piAssigned.Pod.Spec.NodeName) != 0 {
				ps.Allocated.Sub(piAssigned.Resreq)
			}
			ps.TotalRequest.Sub(piAssigned.Resreq)
			ps.Assigned = append(ps.Assigned[:index], ps.Assigned[index+1:]...)
			return
		}
	}
}

func (ps *JobInfo) Clone() *JobInfo {
	info := &JobInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name: ps.Name,
			UID:  ps.UID,
		},
		PdbName:      ps.PdbName,
		MinAvailable: ps.MinAvailable,
		Allocated:    ps.Allocated.Clone(),
		TotalRequest: ps.TotalRequest.Clone(),
		Running:      make([]*TaskInfo, 0),
		Pending:      make([]*TaskInfo, 0),
		Assigned:     make([]*TaskInfo, 0),
		Others:       make([]*TaskInfo, 0),
		NodeSelector: ps.NodeSelector,
	}

	for _, pod := range ps.Running {
		info.Running = append(info.Running, pod.Clone())
	}

	for _, pod := range ps.Pending {
		info.Pending = append(info.Pending, pod.Clone())
	}

	for _, pod := range ps.Assigned {
		info.Assigned = append(info.Assigned, pod.Clone())
	}

	for _, pod := range ps.Others {
		info.Others = append(info.Others, pod.Clone())
	}

	return info
}
