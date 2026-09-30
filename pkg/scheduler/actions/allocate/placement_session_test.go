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

package allocate

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	grouptopologyaffinity "volcano.sh/volcano/pkg/scheduler/plugins/group-topology-affinity"
	networktopologyaware "volcano.sh/volcano/pkg/scheduler/plugins/network-topology-aware"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// Exercise actual allocation, cache writeback and a fresh Session snapshot.
// Pipeline never changes the cached Pending task or its placement generation.
func TestPlacementReconciledAcrossSessions(t *testing.T) {
	for _, softTopology := range []bool{false, true} {
		for _, tc := range []struct {
			name        string
			pipeline    bool
			runningPeer bool
			bindFailure bool
		}{
			{name: "pipeline", pipeline: true},
			{name: "running-and-pipeline", pipeline: true, runningPeer: true},
			{name: "failed-bind", bindFailure: true},
			{name: "successful-bind"},
		} {
			t.Run(fmt.Sprintf("soft-topology=%t/%s", softTopology, tc.name), func(t *testing.T) {
				sc, tiers := newPlacementAllocationCache(t, 2, 1)
				t.Cleanup(func() { cache.ShutdownMockSchedulerCache(sc) })
				ready := &atomic.Bool{}
				ready.Store(true)
				sc.HyperNodesInfo = api.NewHyperNodesInfoWithCache(api.HyperNodeInfoMap{
					"domain-a": newPlacementTestHyperNode("domain-a", 1, ""),
					"domain-b": newPlacementTestHyperNode("domain-b", 1, ""),
				}, map[int]sets.Set[string]{1: sets.New("domain-a", "domain-b")}, map[string]sets.Set[string]{
					"domain-a": sets.New("node-0"),
					"domain-b": sets.New("node-1"),
				}, ready)

				if softTopology {
					sc.AddPodGroupV1beta1(util.BuildPodGroupWithNetWorkTopologies("peer", "ns", "", "q", 1, nil, schedulingv1beta1.PodGroupInqueue, "soft", 1))
					framework.RegisterPluginBuilder(networktopologyaware.PluginName, networktopologyaware.New)
					tiers[0].Plugins = append(tiers[0].Plugins, conf.PluginOption{
						Name: networktopologyaware.PluginName, EnabledHyperNodeGradient: ptr.To(true), EnabledHyperNodeOrder: ptr.To(true),
					})
				} else {
					// An ordinary Job is still tracked when another PodGroup selects it.
					consumer := util.BuildPodGroup("consumer", "ns", "q", 1, nil, schedulingv1beta1.PodGroupInqueue)
					consumer.Spec.TopologyAffinity = &schedulingv1beta1.TopologyAffinitySpec{
						PodGroupAntiAffinity: &schedulingv1beta1.PodGroupAntiAffinity{
							Required: []schedulingv1beta1.PodGroupAffinityTerm{{TopologyTier: ptr.To(int32(1)), PodGroupSelector: &metav1.LabelSelector{}}},
						},
					}
					sc.AddPodGroupV1beta1(consumer)
					framework.RegisterPluginBuilder(grouptopologyaffinity.PluginName, grouptopologyaffinity.New)
					tiers[0].Plugins = append(tiers[0].Plugins, conf.PluginOption{
						Name: grouptopologyaffinity.PluginName, EnabledHyperNodeGradient: ptr.To(true),
					})
				}
				if tc.runningPeer {
					sc.AddPod(util.BuildPod("ns", "running", "node-0", v1.PodRunning, api.BuildResourceList("1", "1Mi"), "peer", nil, nil))
				}

				ssn := framework.OpenSession(sc, tiers, nil)
				job := ssn.Jobs["ns/peer"]
				require.NotNil(t, job)
				require.Equal(t, !softTopology, ssn.PodGroupPlacementEnabled)
				require.False(t, job.AllocatedHyperNodeDirty)
				subJob := job.SubJobs[job.DefaultSubJobID()]
				task := job.Tasks["ns-task-0"]
				tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
				tasks.Push(task)
				if tc.pipeline {
					// Model resources released by preemption in this Session.
					ssn.Nodes["node-1"].Idle = api.EmptyResource()
					ssn.Nodes["node-1"].Releasing = ssn.Nodes["node-1"].Allocatable.Clone()
				}
				alloc := &Action{session: ssn, recorder: NewRecorder()}
				stmt := alloc.allocateResourcesForTasks(subJob, tasks, "domain-b")
				require.NotNil(t, stmt)
				require.Len(t, stmt.Operations(), 1)
				require.True(t, job.AllocatedHyperNodeDirty)
				require.True(t, ssn.DirtyJobs.Has(job.UID))
				if tc.pipeline {
					require.Equal(t, api.Pipelined, task.Status)
					// The allocate action leaves pipelined statements uncommitted.
				} else {
					if tc.bindFailure {
						// Cache changes after the snapshot cause AddBindTask to fail.
						delete(sc.Nodes, "node-1")
					}
					stmt.Commit()
				}
				if tc.pipeline || tc.bindFailure {
					require.Equal(t, api.Pending, sc.Jobs[job.UID].Tasks[task.UID].Status)
					require.Equal(t, job.TaskPlacementGeneration, sc.Jobs[job.UID].TaskPlacementGeneration)
				} else {
					require.Equal(t, api.Binding, sc.Jobs[job.UID].Tasks[task.UID].Status)
					require.Greater(t, sc.Jobs[job.UID].TaskPlacementGeneration, job.TaskPlacementGeneration)
				}
				framework.CloseSession(ssn)
				if tc.pipeline || tc.bindFailure {
					require.True(t, sc.Jobs[job.UID].AllocatedHyperNodeDirty)
				}

				next := framework.OpenSession(sc, tiers, nil)
				nextJob := next.Jobs[job.UID]
				wantDomain := ""
				if tc.runningPeer {
					wantDomain = "domain-a"
				} else if !tc.pipeline && !tc.bindFailure {
					wantDomain = "domain-b"
				}
				require.Equal(t, wantDomain, nextJob.AllocatedHyperNode)
				require.Equal(t, wantDomain, nextJob.SubJobs[subJob.UID].AllocatedHyperNode)
				require.False(t, nextJob.AllocatedHyperNodeDirty)
				occupied := next.HyperNodeIndex().OccupiedHyperNodes(nextJob, 1)
				if wantDomain == "" {
					require.Empty(t, occupied, "unbound reservations must not block anti-affinity")
				} else {
					require.Equal(t, sets.New(wantDomain), occupied, "retain only actual cache occupancy")
				}
				framework.CloseSession(next)
				require.False(t, sc.Jobs[job.UID].AllocatedHyperNodeDirty)
				require.Equal(t, wantDomain, sc.Jobs[job.UID].AllocatedHyperNode)

				unchanged := framework.OpenSession(sc, tiers, nil)
				defer framework.CloseSession(unchanged)
				require.False(t, unchanged.DirtyJobs.Has(job.UID), "unchanged placement should return to the fast path")
			})
		}
	}
}

func TestAllocateWithoutTopologyTracking(t *testing.T) {
	sc, tiers := newPlacementAllocationCache(t, 100, 20)
	t.Cleanup(func() { cache.ShutdownMockSchedulerCache(sc) })
	ssn := framework.OpenSession(sc, tiers, nil)
	defer framework.CloseSession(ssn)
	New().Execute(ssn)
	checkNoTopologyPlacement(t, sc, ssn, 20)
}

func TestOrdinaryPlacementTrackingAfterPolicyAdded(t *testing.T) {
	for _, preferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("preferred=%t", preferred), func(t *testing.T) {
			sc, tiers := newPlacementAllocationCache(t, 2, 1)
			t.Cleanup(func() { cache.ShutdownMockSchedulerCache(sc) })
			ready := &atomic.Bool{}
			ready.Store(true)
			sc.HyperNodesInfo = api.NewHyperNodesInfoWithCache(api.HyperNodeInfoMap{
				"domain-a": newPlacementTestHyperNode("domain-a", 1, ""),
				"domain-b": newPlacementTestHyperNode("domain-b", 1, ""),
			}, map[int]sets.Set[string]{1: sets.New("domain-a", "domain-b")}, map[string]sets.Set[string]{
				"domain-a": sets.New("node-0"),
				"domain-b": sets.New("node-1"),
			}, ready)
			framework.RegisterPluginBuilder(grouptopologyaffinity.PluginName, grouptopologyaffinity.New)
			framework.RegisterPluginBuilder(networktopologyaware.PluginName, networktopologyaware.New)
			tiers[0].Plugins = append(tiers[0].Plugins,
				conf.PluginOption{Name: grouptopologyaffinity.PluginName, EnabledHyperNodeGradient: ptr.To(true), EnabledHyperNodeOrder: ptr.To(true)},
				conf.PluginOption{Name: networktopologyaware.PluginName, EnabledHyperNodeGradient: ptr.To(true), EnabledHyperNodeOrder: ptr.To(true)})

			// A topology-aware readiness probe must not enable tracking for ordinary Jobs.
			sc.AddPodGroupV1beta1(util.BuildPodGroupWithNetWorkTopologies("probe", "ns", "", "q", 1, nil, schedulingv1beta1.PodGroupRunning, "hard", 1))
			sc.AddPod(util.BuildPod("ns", "probe-pod", "node-1", v1.PodRunning, api.BuildResourceList("1", "1Mi"), "probe", nil, nil))
			before := framework.OpenSession(sc, tiers, nil)
			New().Execute(before)
			peer := before.Jobs["ns/peer"]
			require.False(t, before.PodGroupPlacementEnabled)
			require.Equal(t, "domain-b", before.Jobs["ns/probe"].AllocatedHyperNode)
			require.Empty(t, peer.AllocatedHyperNode)
			require.Len(t, peer.TaskStatusIndex[api.Binding], 1)
			nodeName := peer.Tasks["ns-task-0"].NodeName
			framework.CloseSession(before)

			unchanged := framework.OpenSession(sc, tiers, nil)
			require.False(t, unchanged.PodGroupPlacementEnabled)
			require.Empty(t, unchanged.Jobs[peer.UID].AllocatedHyperNode)
			framework.CloseSession(unchanged)

			// The policy itself is enough to activate tracking, even before its
			// Pods exist. No new Pod event for the already-bound peer is needed.
			consumer := util.BuildPodGroup("consumer", "ns", "q", 1, nil, schedulingv1beta1.PodGroupPending)
			terms := []schedulingv1beta1.PodGroupAffinityTerm{{TopologyTier: ptr.To(int32(1)), PodGroupSelector: &metav1.LabelSelector{}}}
			antiAffinity := &schedulingv1beta1.PodGroupAntiAffinity{Required: terms}
			if preferred {
				terms[0].Weight = 100
				antiAffinity = &schedulingv1beta1.PodGroupAntiAffinity{Preferred: terms}
			}
			consumer.Spec.TopologyAffinity = &schedulingv1beta1.TopologyAffinitySpec{PodGroupAntiAffinity: antiAffinity}
			sc.AddPodGroupV1beta1(consumer)
			after := framework.OpenSession(sc, tiers, nil)
			wantDomain := map[string]string{"node-0": "domain-a", "node-1": "domain-b"}[nodeName]
			require.NotEmpty(t, wantDomain)
			require.True(t, after.PodGroupPlacementEnabled)
			require.Empty(t, after.Jobs["ns/consumer"].Tasks)
			require.Equal(t, wantDomain, after.Jobs[peer.UID].AllocatedHyperNode)
			require.Equal(t, wantDomain, after.Jobs[peer.UID].SubJobs[peer.DefaultSubJobID()].AllocatedHyperNode)
			require.Equal(t, sets.New(wantDomain), after.HyperNodeIndex().OccupiedHyperNodes(after.Jobs[peer.UID], 1))
			framework.CloseSession(after)
			require.Equal(t, wantDomain, sc.Jobs[peer.UID].AllocatedHyperNode)
		})
	}
}

// Measure the real allocate action, excluding fixture setup and Session creation.
// Both topology plugins are disabled and no HyperNode CRs exist; the framework
// still adds its synthetic root, which must not activate placement tracking.
func BenchmarkAllocateWithoutTopologyTracking(b *testing.B) {
	for _, size := range []struct{ nodes, tasks int }{{100, 100}, {1000, 100}, {1000, 1000}, {5000, 1000}} {
		b.Run(fmt.Sprintf("nodes=%d/tasks=%d", size.nodes, size.tasks), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// Release each iteration's cache immediately, even if an assertion
				// fails. b.Cleanup would retain every cache until the benchmark ends.
				func() {
					b.StopTimer()
					sc, tiers := newPlacementAllocationCache(b, size.nodes, size.tasks)
					defer cache.ShutdownMockSchedulerCache(sc)
					ssn := framework.OpenSession(sc, tiers, nil)
					defer framework.CloseSession(ssn)
					b.StartTimer()
					New().Execute(ssn)
					b.StopTimer()
					checkNoTopologyPlacement(b, sc, ssn, size.tasks)
				}()
			}
		})
	}
}

func checkNoTopologyPlacement(tb testing.TB, sc *cache.SchedulerCache, ssn *framework.Session, taskCount int) {
	tb.Helper()
	require.Len(tb, ssn.HyperNodes, 1)
	require.Contains(tb, ssn.HyperNodes, framework.ClusterTopHyperNode)
	require.False(tb, ssn.PodGroupPlacementEnabled)
	job := ssn.Jobs["ns/peer"]
	require.Len(tb, job.TaskStatusIndex[api.Binding], taskCount, "all tasks must actually be allocated")
	require.Len(tb, sc.BindFlowChannel, taskCount, "all bindings must reach the cache")
	require.Empty(tb, job.AllocatedHyperNode)
	require.False(tb, job.AllocatedHyperNodeDirty)
	require.False(tb, ssn.DirtyJobs.Has(job.UID), "ordinary allocation must not write back topology placement")
	for _, subJob := range job.SubJobs {
		require.Empty(tb, subJob.AllocatedHyperNode)
	}
	for _, task := range job.Tasks {
		require.Empty(tb, task.JobAllocatedHyperNode)
	}
}

func newPlacementAllocationCache(tb testing.TB, nodeCount, taskCount int) (*cache.SchedulerCache, []conf.Tier) {
	tb.Helper()
	sc := cache.NewCustomMockSchedulerCache("volcano", nil, nil, &util.FakeStatusUpdater{}, nil, &record.FakeRecorder{})
	sc.BindFlowChannel = make(chan *cache.BindContext, taskCount)
	sc.AddQueueV1beta1(util.BuildQueue("q", 1, nil))
	sc.AddPodGroupV1beta1(util.BuildPodGroup("peer", "ns", "q", int32(taskCount), nil, schedulingv1beta1.PodGroupInqueue))
	for i := 0; i < nodeCount; i++ {
		node := util.BuildNode(fmt.Sprintf("node-%d", i), api.BuildResourceList("2000", "4Gi", api.ScalarResource{Name: "pods", Value: "2000"}), nil)
		require.NoError(tb, sc.AddOrUpdateNode(node))
	}
	for i := 0; i < taskCount; i++ {
		sc.AddPod(util.BuildPod("ns", fmt.Sprintf("task-%d", i), "", v1.PodPending, api.BuildResourceList("1", "1Mi"), "peer", nil, nil))
	}
	framework.RegisterPluginBuilder(gang.PluginName, gang.New)
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name: gang.PluginName, EnabledJobReady: ptr.To(true), EnabledJobPipelined: ptr.To(true),
		EnabledSubJobReady: ptr.To(true), EnabledSubJobPipelined: ptr.To(true),
	}}}}
	return sc, tiers
}
