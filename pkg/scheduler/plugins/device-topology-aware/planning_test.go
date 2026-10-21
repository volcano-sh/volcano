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

package devicetopologyaware

import (
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestDeviceTopologyGangPlannerPlansCompleteHardUnit(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}}, now.Add(time.Hour), false))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{secondTask, firstTask},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			firstTask.UID:  {node},
			secondTask.UID: {node},
		},
		at: now,
	})
	if err != nil {
		t.Fatalf("planGang() error = %v", err)
	}
	if plan == nil {
		t.Fatal("planGang() = nil, want a complete plan")
	}
	if plan.jobID != job.UID || plan.schedulingUnitID != string(job.UID) || plan.revision != 1 {
		t.Fatalf("plan identity = (%q, %q, %d), want (%q, %q, 1)", plan.jobID, plan.schedulingUnitID, plan.revision, job.UID, job.UID)
	}
	if plan.fabricID != (api.FabricDomainID{}) {
		t.Fatalf("plan fabric = %#v, want no fabric", plan.fabricID)
	}
	if len(plan.placements) != 2 {
		t.Fatalf("plan placement count = %d, want 2", len(plan.placements))
	}
	if plan.placements[0].taskID != secondTask.UID || plan.placements[1].taskID != firstTask.UID {
		t.Fatalf("plan Task order = [%q, %q], want [%q, %q]", plan.placements[0].taskID, plan.placements[1].taskID, secondTask.UID, firstTask.UID)
	}
	for _, placement := range plan.placements {
		if placement.nodeName != node.Name || placement.nodeUID != node.Node.UID || placement.localDomainID.Value != "domain-a" || placement.quantity != 1 {
			t.Fatalf("placement = %#v, want one device from node-a/domain-a", placement)
		}
	}
}

func TestDeviceTopologyGangPlannerRejectsAggregateDomainOvercommit(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}}, now.Add(time.Hour), false))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{firstTask, secondTask},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			firstTask.UID:  {node},
			secondTask.UID: {node},
		},
		at: now,
	})
	if plan != nil {
		t.Fatalf("planGang() plan = %#v, want nil", plan)
	}
	assertGangPlanningReason(t, err, deviceTopologyDomainFragmentedReason)
}

func TestDeviceTopologyGangPlannerSelectsOneSharedFabric(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	factsA.Fabrics = []api.FabricDomain{newCandidateFabric(factsA, candidateFabricMember(factsA), candidateFabricMember(factsB))}

	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{factsA, factsB}, nil))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{firstTask, secondTask},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			firstTask.UID:  {nodeA},
			secondTask.UID: {nodeB},
		},
		at: now,
	})
	if err != nil {
		t.Fatalf("planGang() error = %v", err)
	}
	if plan == nil {
		t.Fatal("planGang() = nil, want a fabric plan")
	}
	if plan.fabricID != factsA.Fabrics[0].ID {
		t.Fatalf("plan fabric = %#v, want %#v", plan.fabricID, factsA.Fabrics[0].ID)
	}
	if len(plan.placements) != 2 {
		t.Fatalf("plan placement count = %d, want 2", len(plan.placements))
	}
	for _, placement := range plan.placements {
		if !fabricContainsDomain(factsA.Fabrics[0], placement.localDomainID) {
			t.Fatalf("placement domain %#v is not a member of the selected fabric", placement.localDomainID)
		}
	}
}

func TestDeviceTopologyGangPlannerPlansHardFabricWithSoftLocalRequirement(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	facts.Fabrics = []api.FabricDomain{newCandidateFabric(facts, candidateFabricMember(facts))}
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{facts}, nil))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{task},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			task.UID: {node},
		},
		at: now,
	})
	if err != nil {
		t.Fatalf("planGang() error = %v", err)
	}
	if plan == nil || plan.fabricID != facts.Fabrics[0].ID || len(plan.placements) != 1 {
		t.Fatalf("planGang() = %#v, want one placement in the hard fabric", plan)
	}
}

func TestDeviceTopologyGangPlannerReportsRelevantFabricFreshness(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		freshUntil time.Time
		readiness  api.ProviderNodeSyncState
		reason     string
	}{
		{
			name:       "pending member",
			freshUntil: time.Date(2026, time.October, 20, 1, 0, 0, 0, time.UTC),
			readiness:  api.ProviderNodePending,
			reason:     deviceTopologyDataNotReadyReason,
		},
		{
			name:       "stale member",
			freshUntil: time.Date(2026, time.October, 19, 23, 0, 0, 0, time.UTC),
			readiness:  api.ProviderNodeSynced,
			reason:     deviceTopologyStaleReason,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
			now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
			nodeA := newCandidateTestNode("node-a")
			nodeB := newCandidateTestNode("node-b")
			factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
			factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, testCase.freshUntil)
			factsA.Fabrics = []api.FabricDomain{newCandidateFabric(factsA, candidateFabricMember(factsA), candidateFabricMember(factsB))}
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
			job := api.NewJobInfo("job", task)
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
			policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
			job.DeviceTopology = policy
			plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{factsA, factsB}, map[types.UID]api.ProviderNodeSyncState{
				nodeB.Node.UID: testCase.readiness,
			}))

			plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
				job:   job,
				tasks: []*api.TaskInfo{task},
				candidateNodes: map[api.TaskID][]*api.NodeInfo{
					task.UID: {nodeA},
				},
				at: now,
			})
			if plan != nil {
				t.Fatalf("planGang() plan = %#v, want nil", plan)
			}
			assertGangPlanningReason(t, err, testCase.reason)
		})
	}
}

func TestDeviceTopologyGangPlannerIgnoresIrrelevantStaleFabric(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, now.Add(-time.Hour))
	factsB.Fabrics = []api.FabricDomain{newCandidateFabric(factsB, candidateFabricMember(factsB))}
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{factsA, factsB}, nil))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{task},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			task.UID: {nodeA},
		},
		at: now,
	})
	if plan != nil {
		t.Fatalf("planGang() plan = %#v, want nil", plan)
	}
	assertGangPlanningReason(t, err, deviceTopologyFabricUnavailableReason)
}

func TestDeviceTopologyGangPlannerRejectsSplitFabrics(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	factsA.Fabrics = []api.FabricDomain{newCandidateFabric(factsA, candidateFabricMember(factsA))}
	fabricB := newCandidateFabric(factsB, candidateFabricMember(factsB))
	fabricB.ID.Value = "fabric-b"
	factsB.Fabrics = []api.FabricDomain{fabricB}

	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{factsA, factsB}, nil))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{firstTask, secondTask},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			firstTask.UID:  {nodeA},
			secondTask.UID: {nodeB},
		},
		at: now,
	})
	if plan != nil {
		t.Fatalf("planGang() plan = %#v, want nil", plan)
	}
	assertGangPlanningReason(t, err, deviceTopologyFabricUnavailableReason)
}

func TestDeviceTopologyGangPlannerHonorsAssignedNodes(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{
		newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour)),
		newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, now.Add(time.Hour)),
	}, nil)
	plugin, session := openCandidatePlugin(t, job, snapshot)

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{task},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			task.UID: {nodeA, nodeB},
		},
		assignedNodes: map[api.TaskID]*api.NodeInfo{
			task.UID: nodeB,
		},
		at: now,
	})
	if err != nil {
		t.Fatalf("planGang() error = %v", err)
	}
	if plan == nil || len(plan.placements) != 1 {
		t.Fatalf("planGang() = %#v, want one placement", plan)
	}
	if plan.placements[0].nodeName != nodeB.Name || plan.placements[0].localDomainID.NodeUID != nodeB.Node.UID {
		t.Fatalf("assigned placement = %#v, want node-b", plan.placements[0])
	}
}

func TestDeviceTopologyGangPlannerKeepsHardRequirementsOnOneNode(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "gpu-domain", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "fpga-domain", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	setCandidateDomainResource(&factsB, 0, "example.com/fpga")

	container := containerWithRequest("trainer", "nvidia.com/gpu", "1")
	container.Resources.Requests["example.com/fpga"] = resource.MustParse("1")
	task := newDeviceTopologyTask("worker", []corev1.Container{container}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = &scheduling.DeviceTopologySpec{Requirements: []scheduling.DeviceTopologyRequirement{
		{
			Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
			Mode:     scheduling.HardDeviceTopologyMode,
		},
		{
			Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: "example.com/fpga"},
			Mode:     scheduling.HardDeviceTopologyMode,
		},
	}}
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{factsA, factsB}, nil))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{task},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			task.UID: {nodeA, nodeB},
		},
		at: now,
	})
	if plan != nil {
		t.Fatalf("planGang() plan = %#v, want nil", plan)
	}
	assertGangPlanningReason(t, err, deviceTopologyDomainFragmentedReason)
}

func TestDeviceTopologyGangPlannerUsesExactPendingSubGroupUnit(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	subJob := job.SubJobs[job.DefaultSubJobID()]
	subJob.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour), false))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:    job,
		subJob: subJob,
		tasks:  []*api.TaskInfo{firstTask},
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			firstTask.UID: {node},
		},
		at: now,
	})
	if err != nil {
		t.Fatalf("planGang() error = %v", err)
	}
	if plan == nil || plan.schedulingUnitID != string(subJob.UID) || len(plan.placements) != 1 || plan.placements[0].taskID != firstTask.UID {
		t.Fatalf("planGang() = %#v, want one placement for the exact pending SubJob Task", plan)
	}
}

func TestDeviceTopologyGangPlannerSkipsSoftOnlyPolicy(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, nil)

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:   job,
		tasks: []*api.TaskInfo{task},
	})
	if err != nil || plan != nil {
		t.Fatalf("planGang() = (%#v, %v), want (nil, nil) for soft-only policy", plan, err)
	}
}

func TestDeviceTopologyGangPlannerReportsSearchBudgetExhaustion(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour), false))

	plan, err := plugin.planGang(session, deviceTopologyGangPlanContext{
		job:             job,
		tasks:           []*api.TaskInfo{task},
		maxSearchStates: 1,
		candidateNodes: map[api.TaskID][]*api.NodeInfo{
			task.UID: {node},
		},
		at: now,
	})
	if plan != nil {
		t.Fatalf("planGang() plan = %#v, want nil", plan)
	}
	assertGangPlanningReason(t, err, deviceTopologyPlanningBudgetExceededReason)
}

func TestDeviceTopologyGangPlanSolverBacktracksAcrossDomains(t *testing.T) {
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	facts := newCandidateTopologyFacts(node, []candidateDomainSpec{
		{value: "domain-a", deviceCount: 1, healthy: 1},
		{value: "domain-b", deviceCount: 1, healthy: 1},
	}, now.Add(time.Hour))
	snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{facts}, nil)
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	request := func(task *api.TaskInfo) resolvedDeviceTopologyTaskRequest {
		return resolvedDeviceTopologyTaskRequest{
			taskID:        task.UID,
			containerName: "trainer",
			resourceName:  "nvidia.com/gpu",
			quantity:      1,
		}
	}
	domains := snapshot.LocalDomains(node.Node.UID, "nvidia.com/gpu")
	tasks := []deviceTopologyPlanningTask{
		{
			task:  firstTask,
			order: 0,
			candidateChoices: []deviceTopologyTaskNodeChoice{{
				node: node,
				requestChoices: []deviceTopologyRequestDomainChoice{{
					request: request(firstTask),
					domains: domains,
				}},
			}},
		},
		{
			task:  secondTask,
			order: 1,
			candidateChoices: []deviceTopologyTaskNodeChoice{{
				node: node,
				requestChoices: []deviceTopologyRequestDomainChoice{{
					request: request(secondTask),
					domains: domains[:1],
				}},
			}},
		},
	}

	solver := newDeviceTopologyGangPlanSolver(snapshot, tasks, defaultDeviceTopologyGangPlanMaxSearchStates)
	if !solver.solve() {
		t.Fatalf("solver.solve() = false, want true; budgetExceeded = %t", solver.budgetExceeded)
	}
	if len(solver.placements) != 2 {
		t.Fatalf("solver placement count = %d, want 2", len(solver.placements))
	}
	if solver.placements[0].localDomainID.Value != "domain-b" || solver.placements[1].localDomainID.Value != "domain-a" {
		t.Fatalf("solver placements = %#v, want first Task on domain-b after backtracking and second on domain-a", solver.placements)
	}
}

func assertGangPlanningReason(t *testing.T, err error, wantReason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("planGang() error = nil, want reason %q", wantReason)
	}
	var planningError *policyResolutionError
	if !errors.As(err, &planningError) {
		t.Fatalf("planGang() error type = %T, want *policyResolutionError", err)
	}
	if planningError.reason != wantReason {
		t.Fatalf("planGang() reason = %q, want %q", planningError.reason, wantReason)
	}
}
