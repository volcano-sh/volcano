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
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// newTestNode returns a node with real allocatable CPU, so adding tasks to it reduces its idle.
func newTestNode(name string, milliCPU int64) *api.NodeInfo {
	capacity := v1.ResourceList{
		v1.ResourceCPU:  *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
		v1.ResourcePods: *resource.NewQuantity(110, resource.DecimalSI),
	}
	return api.NewNodeInfo(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     v1.NodeStatus{Capacity: capacity, Allocatable: capacity},
	})
}

// addVictims places running 1000m tasks of victimJobID on node.
func addVictims(t *testing.T, node *api.NodeInfo, victimJobID api.JobID, names ...string) []*api.TaskInfo {
	t.Helper()
	victims := make([]*api.TaskInfo, 0, len(names))
	for _, name := range names {
		v := makeTask(victimJobID, name)
		v.Status = api.Running
		v.NodeName = node.Name
		v.Preemptable = true
		assert.NoError(t, node.AddTask(v))
		victims = append(victims, v)
	}
	return victims
}

// fullNodeWithVictims builds a node packed by nVictims running tasks (1000m each) so its
// idle is zero, plus the victim JobInfo and the tasks (sorted by name for stable order).
func fullNodeWithVictims(t *testing.T, victimJobID api.JobID, nVictims int) (*api.NodeInfo, *api.JobInfo, []*api.TaskInfo) {
	t.Helper()
	node := newTestNode("n1", int64(1000*nVictims))
	names := make([]string, 0, nVictims)
	for i := 0; i < nVictims; i++ {
		names = append(names, "v"+string(rune('1'+i)))
	}
	victims := addVictims(t, node, victimJobID, names...)
	return node, api.NewJobInfo(victimJobID, victims...), victims
}

// makeTaskWithCPU returns a pending task requesting milliCPU.
func makeTaskWithCPU(jobID api.JobID, name string, milliCPU float64) *api.TaskInfo {
	task := makeTask(jobID, name)
	task.Resreq = (&api.Resource{MilliCPU: milliCPU}).Clone()
	task.InitResreq = task.Resreq.Clone()
	return task
}

// evictedNames returns the names of victims whose status the committed statement set to Releasing.
func evictedNames(victimJob *api.JobInfo) []string {
	names := []string{}
	for _, task := range victimJob.TaskStatusIndex[api.Releasing] {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	return names
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

// spreadVictimsCase builds one victim job whose 1000m tasks are spread over several nodes,
// returned in the order given, plus a pending job with one task of needMilliCPU.
func spreadVictimsCase(t *testing.T, needMilliCPU float64, nodeCPU map[string]int64, placement [][2]string) (*framework.Session, *api.JobInfo, *api.JobInfo, *api.HyperNodeInfo, []*api.TaskInfo) {
	t.Helper()
	jobID := api.JobID("ns/preemptor")
	job := api.NewJobInfo(jobID, makeTaskWithCPU(jobID, "p1", needMilliCPU))

	victimJobID := api.JobID("ns/victim")
	nodes := map[string]*api.NodeInfo{}
	nodeList := []*api.NodeInfo{}
	for name, cpu := range nodeCPU {
		nodes[name] = newTestNode(name, cpu)
		nodeList = append(nodeList, nodes[name])
	}
	victims := []*api.TaskInfo{}
	for _, p := range placement {
		victims = append(victims, addVictims(t, nodes[p[1]], victimJobID, p[0])...)
	}
	victimJob := api.NewJobInfo(victimJobID, victims...)

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job, victimJobID: victimJob},
		Nodes: nodes,
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, nodeList)
	return ssn, job, victimJob, hn, victims
}

// countSimulations wraps buildPlanFn to count calls; fail, if set, rejects matching victim sets.
func countSimulations(t *testing.T, fail func(victims []*api.TaskInfo) bool) *int {
	t.Helper()
	calls := 0
	orig := buildPlanFn
	buildPlanFn = func(ssn *framework.Session, queue *api.QueueInfo, job *api.JobInfo, hn *api.HyperNodeInfo, victims []*api.TaskInfo, reason string, enablePredCache bool) (*framework.Statement, map[api.SubJobID]string, bool) {
		calls++
		if fail != nil && fail(victims) {
			return nil, nil, false
		}
		return orig(ssn, queue, job, hn, victims, reason, enablePredCache)
	}
	t.Cleanup(func() { buildPlanFn = orig })
	return &calls
}

// TestSelectMinimalVictimsAndPlan_WidensThenDropsVictimsOffPlacedNode covers the multi-node case:
// the domain-wide resource gate is met before the job fits on any node, so the prefix widens
// until a node has room; victims on the other node are then dropped.
func TestSelectMinimalVictimsAndPlan_WidensThenDropsVictimsOffPlacedNode(t *testing.T) {
	ensureServerOptsForTest()
	// n1 and n2 have 2 CPU each; victims are ordered a(n1), b(n2), c(n1), d(n2). The 2000m
	// task needs a whole node: after a and b the total is 2000m but no node fits, after c n1 fits.
	ssn, job, victimJob, hn, victims := spreadVictimsCase(t, 2000,
		map[string]int64{"n1": 2000, "n2": 2000},
		[][2]string{{"a", "n1"}, {"b", "n2"}, {"c", "n1"}, {"d", "n2"}})
	bundle := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{bundle},
		api.EmptyResource(), &api.Resource{MilliCPU: 2000}, "test", true)

	assert.True(t, ok)
	assert.Equal(t, []string{"a", "c"}, evictedNames(victimJob), "b on n2 does not help the job placed on n1")
}

// TestSelectMinimalVictimsAndPlan_KeepsOriginalWhenPrunedSetDoesNotPlace verifies the pruned
// set is committed only if it still places the job.
func TestSelectMinimalVictimsAndPlan_KeepsOriginalWhenPrunedSetDoesNotPlace(t *testing.T) {
	ensureServerOptsForTest()
	ssn, job, victimJob, hn, victims := spreadVictimsCase(t, 2000,
		map[string]int64{"n1": 2000, "n2": 2000},
		[][2]string{{"a", "n1"}, {"b", "n2"}, {"c", "n1"}, {"d", "n2"}})
	bundle := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}
	// Pretend the pruned set [a c] does not place (e.g. a cross-node constraint).
	countSimulations(t, func(v []*api.TaskInfo) bool { return len(v) == 2 && v[0].Name == "a" && v[1].Name == "c" })

	stmt := framework.NewStatement(ssn)
	_, ok := SelectMinimalVictimsAndPlan(ssn, stmt, nil, job, hn, []*Bundle{bundle},
		api.EmptyResource(), &api.Resource{MilliCPU: 2000}, "test", true)

	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, evictedNames(victimJob))
}

// TestSelectMinimalVictimsAndPlan_SearchesBoundaryBundleInLogSimulations verifies the boundary
// bundle is searched with a bounded number of simulations rather than one per task, while the
// selected victims stay the minimal placing prefix.
func TestSelectMinimalVictimsAndPlan_SearchesBoundaryBundleInLogSimulations(t *testing.T) {
	ensureServerOptsForTest()
	// 10 nodes with 4 CPU each hold 40 one-CPU victims placed round-robin, so node n0 holds
	// v00, v10, v20 and v30. Resources cover 4000m after 4 victims, but a whole node is first
	// free after v30.
	nodeCPU := map[string]int64{}
	var placement [][2]string
	for n := 0; n < 10; n++ {
		nodeCPU[fmt.Sprintf("n%d", n)] = 4000
	}
	for v := 0; v < 40; v++ {
		placement = append(placement, [2]string{fmt.Sprintf("v%02d", v), fmt.Sprintf("n%d", v%10)})
	}

	tests := []struct {
		name           string
		needMilliCPU   float64
		wantOK         bool
		wantEvicted    []string
		maxSimulations int
	}{
		{
			name:           "job fits once a whole node is free",
			needMilliCPU:   4000,
			wantOK:         true,
			wantEvicted:    []string{"v00", "v10", "v20", "v30"},
			maxSimulations: 9,
		},
		{
			name:           "job fits on no node",
			needMilliCPU:   5000,
			maxSimulations: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn, job, victimJob, hn, victims := spreadVictimsCase(t, tt.needMilliCPU, nodeCPU, placement)
			bundle := &Bundle{Type: BundleSafe, Job: victimJob, Tasks: victims, LocalRes: sumTasks(victims), GlobalRes: api.EmptyResource()}
			calls := countSimulations(t, nil)

			_, ok := SelectMinimalVictimsAndPlan(ssn, framework.NewStatement(ssn), nil, job, hn, []*Bundle{bundle},
				api.EmptyResource(), &api.Resource{MilliCPU: tt.needMilliCPU}, "test", true)

			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantEvicted, evictedNames(victimJob))
			}
			assert.LessOrEqual(t, *calls, tt.maxSimulations)
		})
	}
}
