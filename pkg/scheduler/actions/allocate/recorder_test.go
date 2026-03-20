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
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestNewRecorder(t *testing.T) {
	recorder := NewRecorder()
	if recorder == nil {
		t.Errorf("NewRecorder should not return nil")
		return
	}
	if recorder.jobDecisions == nil {
		t.Errorf("recorder.jobDecisions should be initialized")
	}
	if recorder.subJobDecisions == nil {
		t.Errorf("recorder.subJobDecisions should be initialized")
	}
}

func TestSaveJobDecision(t *testing.T) {
	recorder := NewRecorder()
	jobID := api.JobID("job1")
	hyperNode := "node1"
	recorder.SaveJobDecision(jobID, hyperNode)

	if recorder.jobDecisions[jobID] != hyperNode {
		t.Errorf("SaveJobDecision should save the correct hyperNode for the job")
	}
}

func TestSaveSubJobDecision(t *testing.T) {
	recorder := NewRecorder()
	jobID := api.JobID("job1")
	hyperNodeForJob := "node1"
	subJobID := api.SubJobID("subJob1")
	hyperNodeForSubJob := "node2"
	recorder.SaveSubJobDecision(jobID, hyperNodeForJob, subJobID, hyperNodeForSubJob)

	if recorder.subJobDecisions[jobID][hyperNodeForJob][subJobID] != hyperNodeForSubJob {
		t.Errorf("SaveSubJobDecision should save the correct hyperNode for the subJob")
	}
}

func TestUpdateDecisionToJob(t *testing.T) {
	jobID := api.JobID("job1")
	recorder := &Recorder{
		jobDecisions: map[api.JobID]string{
			jobID: "node2",
		},
	}

	job := &api.JobInfo{
		UID:                jobID,
		AllocatedHyperNode: "node1",
	}

	hyperNodes := map[string]*api.HyperNodeInfo{
		"node1": {
			Name:      "node1",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor1",
			Children:  sets.New[string](),
		},
		"node2": {
			Name:      "node2",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor2",
			Children:  sets.New[string](),
		},
		"ancestor1": {
			Name:      "ancestor1",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "",
			Children:  sets.New[string]("node1"),
		},
		"ancestor2": {
			Name:      "ancestor2",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor1",
			Children:  sets.New[string]("node2"),
		},
	}

	recorder.UpdateDecisionToJob(job, hyperNodes)

	if job.AllocatedHyperNode != "ancestor1" {
		t.Errorf("Job allocated hyperNode should be updated to ancestor1")
	}
}

func TestUpdateDecisionToJob_SubJob(t *testing.T) {
	jobID := api.JobID("job1")
	hyperNodeForJob := "node2"
	subJobID := api.SubJobID("subJob1")
	hyperNodeForSubJob := "node2"
	recorder := &Recorder{
		jobDecisions: map[api.JobID]string{
			jobID: hyperNodeForJob,
		},
		subJobDecisions: map[api.JobID]map[string]map[api.SubJobID]string{
			jobID: {
				hyperNodeForJob: {
					subJobID: hyperNodeForSubJob,
				},
			},
		},
	}

	job := &api.JobInfo{
		UID:                jobID,
		AllocatedHyperNode: "node1",
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			subJobID: {
				UID:                subJobID,
				AllocatedHyperNode: "node1",
			},
		},
	}

	hyperNodes := map[string]*api.HyperNodeInfo{
		"node1": {
			Name:      "node1",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor1",
			Children:  sets.New[string](),
		},
		"node2": {
			Name:      "node2",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor2",
			Children:  sets.New[string](),
		},
		"ancestor1": {
			Name:      "ancestor1",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "",
			Children:  sets.New[string]("node1"),
		},
		"ancestor2": {
			Name:      "ancestor2",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor1",
			Children:  sets.New[string]("node2"),
		},
	}

	recorder.UpdateDecisionToJob(job, hyperNodes)

	if job.AllocatedHyperNode != "ancestor1" {
		t.Errorf("Job allocated hyperNode should be updated to ancestor1")
	}
	if job.SubJobs[subJobID].AllocatedHyperNode != "ancestor1" {
		t.Errorf("UpdateDecisionToJob should update the allocated hyperNode for the subJob")
	}
}

func TestUpdateDecisionToJob_SubJobNotFound(t *testing.T) {
	recorder := NewRecorder()
	jobID := api.JobID("job1")
	hyperNodeForJob := "node1"
	subJobID := api.SubJobID("subJob1")
	hyperNodeForSubJob := "node2"
	recorder.SaveSubJobDecision(jobID, hyperNodeForJob, subJobID, hyperNodeForSubJob)

	job := &api.JobInfo{
		UID:     jobID,
		SubJobs: map[api.SubJobID]*api.SubJobInfo{},
	}

	hyperNodes := api.HyperNodeInfoMap{
		"node1": &api.HyperNodeInfo{Name: "node1"},
		"node2": &api.HyperNodeInfo{Name: "node2"},
		"node3": &api.HyperNodeInfo{Name: "node3"},
	}

	recorder.UpdateDecisionToJob(job, hyperNodes)
	// Ensure no panic or error
}

func TestUpdateDecisionToJob_NoHyperNode(t *testing.T) {
	recorder := NewRecorder()
	jobID := api.JobID("job1")

	job := &api.JobInfo{
		UID:                jobID,
		AllocatedHyperNode: "node2",
	}

	hyperNodes := api.HyperNodeInfoMap{
		"node1": &api.HyperNodeInfo{Name: "node1"},
		"node2": &api.HyperNodeInfo{Name: "node2"},
		"node3": &api.HyperNodeInfo{Name: "node3"},
	}

	recorder.UpdateDecisionToJob(job, hyperNodes)

	if job.AllocatedHyperNode != "node2" {
		t.Errorf("UpdateDecisionToJob should not update the allocated hyperNode for the job without a recorder")
	}
}

func TestUpdateDecisionToJob_NoSubJob(t *testing.T) {
	recorder := NewRecorder()
	jobID := api.JobID("job1")
	hyperNodeForJob := "node1"
	recorder.SaveJobDecision(jobID, hyperNodeForJob)

	job := &api.JobInfo{
		UID: jobID,
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			api.SubJobID("subJob1"): {
				UID:                api.SubJobID("subJob1"),
				AllocatedHyperNode: "node2",
			},
		},
	}

	hyperNodes := map[string]*api.HyperNodeInfo{
		"node1": {
			Name:      "node1",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor1",
			Children:  sets.New[string](),
		},
		"node2": {
			Name:      "node2",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor2",
			Children:  sets.New[string](),
		},
		"ancestor1": {
			Name:      "ancestor1",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "",
			Children:  sets.New[string]("node1"),
		},
		"ancestor2": {
			Name:      "ancestor2",
			HyperNode: &topologyv1alpha1.HyperNode{},
			Parent:    "ancestor1",
			Children:  sets.New[string]("node2"),
		},
	}

	recorder.UpdateDecisionToJob(job, hyperNodes)

	if job.SubJobs["subJob1"].AllocatedHyperNode != "node2" {
		t.Errorf("UpdateDecisionToJob should not update the allocated hyperNode for the subJob without a recorder")
	}
}
