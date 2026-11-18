/*
Copyright 2026 The Volcano Authors.

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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func pruneTestTask(jobID api.JobID, name, node string, status api.TaskStatus, milliCPU float64) *api.TaskInfo {
	res := (&api.Resource{MilliCPU: milliCPU}).Clone()
	return &api.TaskInfo{
		UID:         api.TaskID(name),
		Job:         jobID,
		Name:        name,
		Namespace:   "ns",
		Preemptable: true,
		Resreq:      res.Clone(),
		InitResreq:  res.Clone(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "ns",
				UID:       types.UID(name),
			},
		},
		NumaInfo: &api.TopologyInfo{
			ResMap: map[int]v1.ResourceList{},
		},
		TransactionContext: api.TransactionContext{
			NodeName: node,
			Status:   status,
		},
	}
}

func pruneTestNode(name string, idleMilliCPU float64) *api.NodeInfo {
	n := api.NewNodeInfo(nil)
	n.Name = name
	n.Idle = (&api.Resource{MilliCPU: idleMilliCPU}).Clone()
	n.Releasing = api.EmptyResource()
	n.Pipelined = api.EmptyResource()
	return n
}

// TestPruneRedundantVictims_DropsUnneededWholeBundles reproduces
// volcano-sh/volcano#6065: five whole-bundle victims are accepted because the
// domain total covers the request, but only the 8-CPU victim on node5 is
// needed to place the 8-CPU pending pod.
func TestPruneRedundantVictims_DropsUnneededWholeBundles(t *testing.T) {
	ssn := &framework.Session{}
	pendingJobID := api.JobID("ns/prod-new")
	pending := []*api.TaskInfo{pruneTestTask(pendingJobID, "prod-new", "", api.Pending, 8000)}

	nodes := []*api.NodeInfo{
		pruneTestNode("node1", 0),
		pruneTestNode("node2", 0),
		pruneTestNode("node3", 0),
		pruneTestNode("node4", 0),
		pruneTestNode("node5", 0),
	}

	bundles := make([]*Bundle, 0, 5)
	victimNames := []struct {
		job  string
		task string
		node string
		cpu  float64
	}{
		{"ns/spot-a", "spot-a", "node1", 6000},
		{"ns/spot-b", "spot-b", "node2", 6000},
		{"ns/spot-c", "spot-c", "node3", 6000},
		{"ns/spot-d", "spot-d", "node4", 6000},
		{"ns/spot-e", "spot-e", "node5", 8000},
	}
	for _, v := range victimNames {
		jobID := api.JobID(v.job)
		task := pruneTestTask(jobID, v.task, v.node, api.Running, v.cpu)
		job := api.NewJobInfo(jobID, task)
		bundles = append(bundles, &Bundle{
			Type:      BundleWhole,
			Job:       job,
			Tasks:     []*api.TaskInfo{task},
			LocalRes:  task.Resreq.Clone(),
			GlobalRes: task.Resreq.Clone(),
		})
	}

	pruned := PruneRedundantVictims(ssn, pending, nodes, bundles)
	assert.Len(t, pruned, 1)
	if len(pruned) == 1 {
		assert.Equal(t, api.TaskID("spot-e"), pruned[0].UID)
	}
}

// TestPruneRedundantVictims_KeepsAllWhenEachNeeded ensures pruning never
// removes a victim when every victim is required for placement.
func TestPruneRedundantVictims_KeepsAllWhenEachNeeded(t *testing.T) {
	ssn := &framework.Session{}
	pendingJobID := api.JobID("ns/prod-new")
	// Two pending pods of 8 CPU each need both victims.
	pending := []*api.TaskInfo{
		pruneTestTask(pendingJobID, "p1", "", api.Pending, 8000),
		pruneTestTask(pendingJobID, "p2", "", api.Pending, 8000),
	}
	nodes := []*api.NodeInfo{
		pruneTestNode("node1", 0),
		pruneTestNode("node2", 0),
	}
	bundles := []*Bundle{}
	for _, tc := range []struct {
		job, task, node string
	}{
		{"ns/spot-a", "spot-a", "node1"},
		{"ns/spot-b", "spot-b", "node2"},
	} {
		jobID := api.JobID(tc.job)
		task := pruneTestTask(jobID, tc.task, tc.node, api.Running, 8000)
		job := api.NewJobInfo(jobID, task)
		bundles = append(bundles, &Bundle{
			Type:      BundleWhole,
			Job:       job,
			Tasks:     []*api.TaskInfo{task},
			LocalRes:  task.Resreq.Clone(),
			GlobalRes: task.Resreq.Clone(),
		})
	}

	pruned := PruneRedundantVictims(ssn, pending, nodes, bundles)
	assert.Len(t, pruned, 2)
}

// TestPruneRedundantVictims_SafeBundleSplitsPerPod verifies a safe bundle
// contributes one unit per pod, so surplus replicas can be dropped
// individually without evicting the whole gang.
func TestPruneRedundantVictims_SafeBundleSplitsPerPod(t *testing.T) {
	ssn := &framework.Session{}
	pendingJobID := api.JobID("ns/prod-new")
	pending := []*api.TaskInfo{pruneTestTask(pendingJobID, "p1", "", api.Pending, 2000)}
	nodes := []*api.NodeInfo{pruneTestNode("node1", 0)}

	jobID := api.JobID("ns/elastic")
	t1 := pruneTestTask(jobID, "t1", "node1", api.Running, 2000)
	t2 := pruneTestTask(jobID, "t2", "node1", api.Running, 2000)
	job := api.NewJobInfo(jobID, t1, t2)
	otherJobID := api.JobID("ns/other")
	o1 := pruneTestTask(otherJobID, "o1", "node1", api.Running, 2000)
	otherJob := api.NewJobInfo(otherJobID, o1)
	bundles := []*Bundle{
		{
			Type:      BundleSafe,
			Job:       job,
			Tasks:     []*api.TaskInfo{t1, t2},
			LocalRes:  (&api.Resource{MilliCPU: 4000}).Clone(),
			GlobalRes: api.EmptyResource(),
		},
		{
			Type:      BundleWhole,
			Job:       otherJob,
			Tasks:     []*api.TaskInfo{o1},
			LocalRes:  (&api.Resource{MilliCPU: 2000}).Clone(),
			GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
		},
	}

	pruned := PruneRedundantVictims(ssn, pending, nodes, bundles)
	// One 2000m pod is enough; at least one unit must be pruned.
	assert.Less(t, len(pruned), 3)
	assert.GreaterOrEqual(t, len(pruned), 1)
}

// TestPruneRedundantVictims_RespectsPredicateViability ensures victims on
// nodes the pending pod cannot run on are not counted as usable capacity.
func TestPruneRedundantVictims_RespectsPredicateViability(t *testing.T) {
	trueVal := true
	ssn := &framework.Session{
		Tiers: []conf.Tier{
			{Plugins: []conf.PluginOption{{Name: "test-pred", EnabledPredicate: &trueVal}}},
		},
	}
	// Only node1 (odd name) is viable via PredicateForPreemptAction.
	ssn.AddPredicateFn("test-pred", func(task *api.TaskInfo, node *api.NodeInfo) error {
		if node.Name == "node1" {
			return nil
		}
		return api.NewFitErrWithStatus(task, node, &api.Status{Code: api.UnschedulableAndUnresolvable, Reason: "wrong accelerator type"})
	})

	pendingJobID := api.JobID("ns/prod-new")
	pending := []*api.TaskInfo{pruneTestTask(pendingJobID, "p1", "", api.Pending, 8000)}
	nodes := []*api.NodeInfo{
		pruneTestNode("node1", 0),
		pruneTestNode("node2", 0),
	}
	bundles := []*Bundle{}
	for _, tc := range []struct {
		job, task, node string
	}{
		{"ns/spot-a", "spot-a", "node1"},
		{"ns/spot-b", "spot-b", "node2"},
	} {
		jobID := api.JobID(tc.job)
		task := pruneTestTask(jobID, tc.task, tc.node, api.Running, 8000)
		job := api.NewJobInfo(jobID, task)
		bundles = append(bundles, &Bundle{
			Type:      BundleWhole,
			Job:       job,
			Tasks:     []*api.TaskInfo{task},
			LocalRes:  task.Resreq.Clone(),
			GlobalRes: task.Resreq.Clone(),
		})
	}

	pruned := PruneRedundantVictims(ssn, pending, nodes, bundles)
	// node2 is not viable, so spot-b must be dropped and spot-a kept.
	assert.Len(t, pruned, 1)
	if len(pruned) == 1 {
		assert.Equal(t, api.TaskID("spot-a"), pruned[0].UID)
	}
}

// TestPruneRedundantVictims_SingleVictimUnchanged ensures no pruning work
// happens for trivial single-victim sets.
func TestPruneRedundantVictims_SingleVictimUnchanged(t *testing.T) {
	ssn := &framework.Session{}
	pending := []*api.TaskInfo{pruneTestTask(api.JobID("ns/j"), "p", "", api.Pending, 1000)}
	nodes := []*api.NodeInfo{pruneTestNode("n1", 0)}
	jobID := api.JobID("ns/v")
	v := pruneTestTask(jobID, "v", "n1", api.Running, 1000)
	bundles := []*Bundle{{Type: BundleWhole, Job: api.NewJobInfo(jobID, v), Tasks: []*api.TaskInfo{v}}}
	pruned := PruneRedundantVictims(ssn, pending, nodes, bundles)
	assert.Len(t, pruned, 1)
}
