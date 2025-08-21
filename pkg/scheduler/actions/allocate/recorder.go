/*
Copyright 2025 The Volcano Authors.

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
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type Recorder struct {
	jobDecisions      map[api.JobID]string
	podBunchDecisions map[api.JobID]map[string]map[api.BunchID]string

	podBunchStatusSnapshot map[api.JobID]map[api.BunchID]*PodBunchStatus
}

type PodBunchStatus struct {
	AllocatedHyperNode string
}

func NewRecorder() *Recorder {
	return &Recorder{
		jobDecisions:           make(map[api.JobID]string),
		podBunchDecisions:      make(map[api.JobID]map[string]map[api.BunchID]string),
		podBunchStatusSnapshot: make(map[api.JobID]map[api.BunchID]*PodBunchStatus),
	}
}

func (d *Recorder) SaveJobDecision(job api.JobID, hyperNodeForJob string) {
	d.jobDecisions[job] = hyperNodeForJob
}

func (d *Recorder) SavePodBunchDecision(job api.JobID, hyperNodeForJob string, podBunch api.BunchID, hyperNodeForPodBunch string) {
	if d.podBunchDecisions[job] == nil {
		d.podBunchDecisions[job] = make(map[string]map[api.BunchID]string)
	}
	if d.podBunchDecisions[job][hyperNodeForJob] == nil {
		d.podBunchDecisions[job][hyperNodeForJob] = make(map[api.BunchID]string)
	}
	d.podBunchDecisions[job][hyperNodeForJob][podBunch] = hyperNodeForPodBunch
}

func (d *Recorder) UpdateDecisionToJob(job *api.JobInfo, hyperNodes api.HyperNodeInfoMap) {
	hyperNodeForJob := d.jobDecisions[job.UID]
	if hyperNodeForJob == "" {
		return
	}

	jobAllocatedHyperNode := hyperNodes.GetLCAHyperNode(job.AllocatedHyperNode, hyperNodeForJob)
	if job.AllocatedHyperNode != jobAllocatedHyperNode {
		klog.V(3).InfoS("update allocated hyperNode for job", "job", job.UID,
			"old", job.AllocatedHyperNode, "new", jobAllocatedHyperNode)
		job.AllocatedHyperNode = jobAllocatedHyperNode
	}

	for bunchId, hyperNode := range d.podBunchDecisions[job.UID][hyperNodeForJob] {
		podBunch, found := job.PodBunches[bunchId]
		if !found {
			klog.Errorf("podBunch %s not found", bunchId)
			continue
		}
		allocatedHyperNode := hyperNodes.GetLCAHyperNode(podBunch.AllocatedHyperNode, hyperNode)
		if podBunch.AllocatedHyperNode != allocatedHyperNode {
			klog.V(3).InfoS("update allocated hyperNode for podBunch", "podBunch", podBunch.UID,
				"old", podBunch.AllocatedHyperNode, "new", allocatedHyperNode)
			podBunch.AllocatedHyperNode = allocatedHyperNode
		}
	}
}

func (d *Recorder) SnapshotPodBunchStatus(job *api.JobInfo, worksheet *JobWorksheet) {
	result := make(map[api.BunchID]*PodBunchStatus)
	for bunchID := range worksheet.podBunchWorksheets {
		if podBunch, found := job.PodBunches[bunchID]; found {
			result[bunchID] = &PodBunchStatus{AllocatedHyperNode: podBunch.AllocatedHyperNode}
		}
	}
	d.podBunchStatusSnapshot[job.UID] = result
}

func (d *Recorder) RecoverPodBunchStatus(job *api.JobInfo) {
	snapshot, ok := d.podBunchStatusSnapshot[job.UID]
	if !ok {
		return
	}
	for bunchID, status := range snapshot {
		if podBunch, found := job.PodBunches[bunchID]; found {
			podBunch.AllocatedHyperNode = status.AllocatedHyperNode
		}
	}
}
