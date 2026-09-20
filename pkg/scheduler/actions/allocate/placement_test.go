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
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestCaptureRestoreHyperNodePlacement(t *testing.T) {
	job := &api.JobInfo{
		UID:                "job-1",
		AllocatedHyperNode: "",
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			"sub-1": {
				UID:                "sub-1",
				AllocatedHyperNode: "",
			},
		},
	}
	subJob := job.SubJobs["sub-1"]
	placement := captureHyperNodePlacement(job, subJob)

	job.AllocatedHyperNode = "sn-a"
	subJob.AllocatedHyperNode = "sn-a"
	restoreHyperNodePlacement(job, subJob, placement)

	if job.AllocatedHyperNode != "" {
		t.Fatalf("job AllocatedHyperNode = %q, want empty", job.AllocatedHyperNode)
	}
	if subJob.AllocatedHyperNode != "" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want empty", subJob.AllocatedHyperNode)
	}
}

func TestUpdateJobAllocatedHyperNodeFromSubJob(t *testing.T) {
	hn := api.HyperNodeInfoMap{
		"root": newPlacementTestHyperNode("root", 3, ""),
		"sn-a": newPlacementTestHyperNode("sn-a", 2, "root"),
		"sn-b": newPlacementTestHyperNode("sn-b", 2, "root"),
	}
	ssn := &framework.Session{
		HyperNodes:                hn,
		HyperNodesReadyToSchedule: true,
		DirtyJobs:                 sets.New[api.JobID](),
	}
	job := &api.JobInfo{UID: "job-1", AllocatedHyperNode: "sn-a"}
	subJob := &api.SubJobInfo{UID: "sub-1"}

	updateJobAllocatedHyperNodeFromSubJob(ssn, job, subJob, "sn-b")
	if job.AllocatedHyperNode != "root" {
		t.Fatalf("job AllocatedHyperNode = %q, want root", job.AllocatedHyperNode)
	}
}

func TestFilterGradientsByMinResourceTierStats(t *testing.T) {
	nodeInfo := api.NewNodeInfo(util.BuildNode(
		"node-a",
		api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "110"}}...),
		nil,
	))

	ssn := &framework.Session{
		Nodes: map[string]*api.NodeInfo{"node-a": nodeInfo},
		RealNodesSet: map[string]sets.Set[string]{
			"sn-a": sets.New("node-a"),
			"sn-b": sets.New("node-a"),
		},
		HyperNodes: api.HyperNodeInfoMap{
			"sn-a": newPlacementTestHyperNode("sn-a", 2, "root"),
			"sn-b": newPlacementTestHyperNode("sn-b", 2, "root"),
		},
		HyperNodeTierNameMap: api.HyperNodeTierNameMap{"supernode": 2},
	}

	gradients := [][]*api.HyperNodeInfo{
		{ssn.HyperNodes["sn-a"], ssn.HyperNodes["sn-b"]},
	}
	minResource := &api.Resource{MilliCPU: 20000, Memory: 40 * 1024 * 1024 * 1024}

	filtered, stats := FilterGradientsByMinResource(ssn, gradients, minResource, "")
	if len(filtered) != 0 {
		t.Fatalf("expected empty filtered gradients, got %#v", filtered)
	}
	if stats == nil {
		t.Fatal("expected resource filter stats")
	}
	if stats.ExcludedByTier[2] != 2 {
		t.Fatalf("expected 2 resource exclusions at supernode tier, got %#v", stats.ExcludedByTier)
	}
	if stats.FinalByTier[2] != 0 {
		t.Fatalf("expected 0 final hyperNodes, got %#v", stats.FinalByTier)
	}
}

func newPlacementTestHyperNode(name string, tier int, parent string) *api.HyperNodeInfo {
	hn := &topologyv1alpha1.HyperNode{}
	hn.Name = name
	hn.Spec.Tier = tier
	hni := api.NewHyperNodeInfo(hn)
	hni.Parent = parent
	return hni
}
