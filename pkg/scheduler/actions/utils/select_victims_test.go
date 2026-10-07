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

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// fullNodeWithVictims builds a node packed by nVictims running tasks (1000m each) so its
// idle is zero, plus the victim JobInfo and the tasks (sorted by name for stable order).
func fullNodeWithVictims(t *testing.T, victimJobID api.JobID, nVictims int) (*api.NodeInfo, *api.JobInfo, []*api.TaskInfo) {
	t.Helper()
	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: float64(1000 * nVictims)}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()

	victims := make([]*api.TaskInfo, 0, nVictims)
	for i := 0; i < nVictims; i++ {
		v := makeTask(victimJobID, "v"+string(rune('1'+i)))
		v.Status = api.Running
		v.NodeName = node.Name
		v.Preemptable = true
		assert.NoError(t, node.AddTask(v))
		victims = append(victims, v)
	}
	return node, api.NewJobInfo(victimJobID, victims...), victims
}

// TestSelectMinimalVictimsAndPlan_TrimsToMinimalPrefix is the regression test for
// volcano-sh/volcano#5984 / #5994: a preemptor/reclaimer that needs only one victim pod
// must not cause the whole SAFE bundle to be evicted.
func TestSelectMinimalVictimsAndPlan_TrimsToMinimalPrefix(t *testing.T) {
	ensureServerOptsForTest()

	jobID := api.JobID("ns/needs-one")
	job := api.NewJobInfo(jobID, makeTask(jobID, "t1")) // one pending task, 1000m

	victimJobID := api.JobID("ns/victim")
	node, victimJob, victims := fullNodeWithVictims(t, victimJobID, 3) // 3-pod SAFE bundle

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job, victimJobID: victimJob},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})

	bundle := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{bundle},
		api.EmptyResource(), &api.Resource{MilliCPU: 1000}, "test", true)

	assert.True(t, ok, "expected a nomination plan to be committed")
	// 1 evict + 1 pipeline == 2 operations. The bug would evict all 3 victims (4 operations).
	assert.Equal(t, 2, len(stmt.Operations()), "only the single sufficient victim should be evicted")
}

// TestSelectMinimalVictimsAndPlan_ConsumesEntireSafeBundleWhenNeeded verifies the trim does
// not under-select: when the demand needs every SAFE task, all of them are evicted.
func TestSelectMinimalVictimsAndPlan_ConsumesEntireSafeBundleWhenNeeded(t *testing.T) {
	ensureServerOptsForTest()

	jobID := api.JobID("ns/needs-three")
	job := api.NewJobInfo(jobID, makeTask(jobID, "t1"), makeTask(jobID, "t2"), makeTask(jobID, "t3"))

	victimJobID := api.JobID("ns/victim")
	node, victimJob, victims := fullNodeWithVictims(t, victimJobID, 3)

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job, victimJobID: victimJob},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})

	bundle := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{bundle},
		api.EmptyResource(), &api.Resource{MilliCPU: 3000}, "test", true)

	assert.True(t, ok)
	// 3 evict + 3 pipeline == 6 operations.
	assert.Equal(t, 6, len(stmt.Operations()), "all victims should be evicted when the demand requires them")
}

// TestSelectMinimalVictimsAndPlan_WholeBundleConsumedIntact verifies a WHOLE bundle (tasks
// at the gang minimum) is all-or-nothing: it is never trimmed to a partial prefix, since
// evicting only some of them would break the victim's gang without fully clearing the job.
func TestSelectMinimalVictimsAndPlan_WholeBundleConsumedIntact(t *testing.T) {
	ensureServerOptsForTest()

	jobID := api.JobID("ns/needs-one")
	job := api.NewJobInfo(jobID, makeTask(jobID, "t1")) // one pending task, 1000m

	victimJobID := api.JobID("ns/victim")
	node, victimJob, victims := fullNodeWithVictims(t, victimJobID, 3)

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job, victimJobID: victimJob},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})

	// A WHOLE bundle: even though 1000m (one task) covers the demand by resources, the
	// bundle must be consumed intact.
	bundle := &Bundle{Type: BundleWhole, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{bundle},
		api.EmptyResource(), &api.Resource{MilliCPU: 1000}, "test", true)

	assert.True(t, ok)
	// All 3 victims evicted (3 evict + 1 pipeline == 4). A partial-prefix bug would give 2.
	assert.Equal(t, 4, len(stmt.Operations()), "a WHOLE bundle must be consumed intact, not trimmed to a prefix")
}

// TestSelectMinimalVictimsAndPlan_SafeThenWholeConsumesWholeIntact covers a mixed run: a SAFE
// bundle that is insufficient on its own, followed by a WHOLE bundle. The SAFE bundle is taken
// whole and the WHOLE bundle, being all-or-nothing, is also consumed in full rather than
// trimmed to the single task that would nominally cover the remaining demand.
func TestSelectMinimalVictimsAndPlan_SafeThenWholeConsumesWholeIntact(t *testing.T) {
	ensureServerOptsForTest()

	jobID := api.JobID("ns/needs-three")
	job := api.NewJobInfo(jobID, makeTask(jobID, "t1"), makeTask(jobID, "t2"), makeTask(jobID, "t3")) // 3000m demand

	victimJobID := api.JobID("ns/victim")
	node, victimJob, victims := fullNodeWithVictims(t, victimJobID, 5) // 5 x 1000m on the node

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job, victimJobID: victimJob},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})

	safe := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims[:2], LocalRes: sumTasks(victims[:2]), GlobalRes: api.EmptyResource()}
	whole := &Bundle{Type: BundleWhole, Job: victimJob, Tasks: victims[2:], LocalRes: sumTasks(victims[2:]), GlobalRes: api.EmptyResource()}

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{safe, whole},
		api.EmptyResource(), &api.Resource{MilliCPU: 3000}, "test", true)

	assert.True(t, ok)
	// SAFE (2 tasks = 2000m) alone < 3000m, so the WHOLE bundle is entered; consumed intact it
	// evicts all 3 of its tasks. Total 5 evict + 3 pipeline == 8. Trimming the WHOLE bundle to
	// one task would give 3 evict + 3 pipeline == 6.
	assert.Equal(t, 8, len(stmt.Operations()), "WHOLE bundle must be consumed intact even after a SAFE bundle")
}

// TestSelectMinimalVictimsAndPlan_InfeasibleReturnsFalse verifies no plan is committed when
// even the whole bundle cannot satisfy the demand.
func TestSelectMinimalVictimsAndPlan_InfeasibleReturnsFalse(t *testing.T) {
	ensureServerOptsForTest()

	jobID := api.JobID("ns/needs-too-much")
	job := api.NewJobInfo(jobID, makeTask(jobID, "t1"))

	victimJobID := api.JobID("ns/victim")
	node, victimJob, victims := fullNodeWithVictims(t, victimJobID, 1)

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job, victimJobID: victimJob},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})

	bundle := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{bundle},
		api.EmptyResource(), &api.Resource{MilliCPU: 5000}, "test", true)

	assert.False(t, ok)
	assert.Equal(t, 0, len(stmt.Operations()))
}
