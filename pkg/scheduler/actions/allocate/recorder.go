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
	jobDecisions    map[api.JobID]string
	subJobDecisions map[api.JobID]map[string]map[api.SubJobID]string

	subJobStatusSnapshot map[api.JobID]map[api.SubJobID]*SubJobStatus
}

type SubJobStatus struct {
	AllocatedHyperNode string
}

func NewRecorder() *Recorder {
	return &Recorder{
		jobDecisions:         make(map[api.JobID]string),
		subJobDecisions:      make(map[api.JobID]map[string]map[api.SubJobID]string),
		subJobStatusSnapshot: make(map[api.JobID]map[api.SubJobID]*SubJobStatus),
	}
}

func (d *Recorder) SaveJobDecision(job api.JobID, hyperNodeForJob string) {
	d.jobDecisions[job] = hyperNodeForJob
}

func (d *Recorder) SaveSubJobDecision(job api.JobID, hyperNodeForJob string, subJob api.SubJobID, hyperNodeForSubJob string) {
	if d.subJobDecisions[job] == nil {
		d.subJobDecisions[job] = make(map[string]map[api.SubJobID]string)
	}
	if d.subJobDecisions[job][hyperNodeForJob] == nil {
		d.subJobDecisions[job][hyperNodeForJob] = make(map[api.SubJobID]string)
	}
	d.subJobDecisions[job][hyperNodeForJob][subJob] = hyperNodeForSubJob
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

	for subJobID, hyperNode := range d.subJobDecisions[job.UID][hyperNodeForJob] {
		subJob, found := job.SubJobs[subJobID]
		if !found {
			klog.Errorf("subJob %s not found", subJobID)
			continue
		}
		allocatedHyperNode := hyperNodes.GetLCAHyperNode(subJob.AllocatedHyperNode, hyperNode)
		if subJob.AllocatedHyperNode != allocatedHyperNode {
			klog.V(3).InfoS("update allocated hyperNode for subJob", "subJob", subJob.UID,
				"old", subJob.AllocatedHyperNode, "new", allocatedHyperNode)
			subJob.AllocatedHyperNode = allocatedHyperNode
		}
	}
}

func (d *Recorder) SnapshotSubJobStatus(job *api.JobInfo, worksheet *JobWorksheet) {
	result := make(map[api.SubJobID]*SubJobStatus)
	for subJobID := range worksheet.subJobWorksheets {
		if subJob, found := job.SubJobs[subJobID]; found {
			result[subJobID] = &SubJobStatus{AllocatedHyperNode: subJob.AllocatedHyperNode}
		}
	}
	d.subJobStatusSnapshot[job.UID] = result
}

func (d *Recorder) RecoverSubJobStatus(job *api.JobInfo) {
	snapshot, ok := d.subJobStatusSnapshot[job.UID]
	if !ok {
		return
	}
	for subJobID, status := range snapshot {
		if subJob, found := job.SubJobs[subJobID]; found {
			subJob.AllocatedHyperNode = status.AllocatedHyperNode
		}
	}
}
