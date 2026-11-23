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
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	candidateTestProviderID = "candidate-test-provider"
	candidateTestNamespace  = "candidate.test"
)

type candidateDomainSpec struct {
	value       string
	deviceCount int
	healthy     int
}

func TestDeviceTopologyCandidateHardLocalDomain(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()

	tests := []struct {
		name       string
		domains    []candidateDomainSpec
		freshUntil time.Time
		noSnapshot bool
		wantReason string
	}{
		{
			name:       "fits one fresh healthy local domain",
			domains:    []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}},
			freshUntil: now.Add(time.Hour),
		},
		{
			name:       "does not aggregate devices across domains",
			domains:    []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}, {value: "domain-b", deviceCount: 1, healthy: 1}},
			freshUntil: now.Add(time.Hour),
			wantReason: deviceTopologyDomainFragmentedReason,
		},
		{
			name:       "rejects unhealthy devices",
			domains:    []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 0}},
			freshUntil: now.Add(time.Hour),
			wantReason: deviceTopologyDeviceUnhealthyReason,
		},
		{
			name:       "rejects stale facts",
			domains:    []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}},
			freshUntil: now.Add(-time.Hour),
			wantReason: deviceTopologyStaleReason,
		},
		{
			name:       "waits for topology data",
			noSnapshot: true,
			wantReason: deviceTopologyDataNotReadyReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
			job := api.NewJobInfo("job", task)
			job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)

			var snapshot *api.TopologySnapshot
			if !test.noSnapshot {
				snapshot = newCandidateTopologySnapshot(node, test.domains, test.freshUntil, false)
			}
			plugin, session := openCandidatePlugin(t, job, snapshot)

			err := plugin.predicate(session, task, node)
			if test.wantReason == "" {
				if err != nil {
					t.Fatalf("predicate() error = %v, want nil", err)
				}
				score, orderErr := plugin.nodeOrder(session, task, node)
				if orderErr != nil {
					t.Fatalf("nodeOrder() error = %v", orderErr)
				}
				if score != 0 {
					t.Fatalf("nodeOrder() score = %v, want 0 for a hard-only policy", score)
				}
				return
			}
			assertCandidateFitReason(t, err, test.wantReason)
		})
	}
}

func TestDeviceTopologyCandidateExcludesHeldLedgerDevices(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()
	node := newCandidateTestNode("node-a")
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	key := api.ProviderNodeKey{ProviderID: facts.ProviderID, NodeUID: facts.NodeUID}
	snapshot := api.NewTopologySnapshotWithAvailability(1,
		map[api.ProviderNodeKey]api.NodeTopologyFacts{key: facts},
		map[api.ProviderNodeKey]api.ProviderNodeSyncState{key: api.ProviderNodeSynced},
		map[api.DeviceID]api.DeviceAvailability{facts.Devices[0].ID: api.DeviceHeld},
	)
	plugin, session := openCandidatePlugin(t, job, snapshot)

	assertCandidateFitReason(t, plugin.predicate(session, task, node), deviceTopologyReservationConflictReason)
}

func TestDeviceTopologyCandidateWaitsForAllocationRecovery(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()
	node := newCandidateTestNode("node-a")
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
	key := api.ProviderNodeKey{ProviderID: facts.ProviderID, NodeUID: facts.NodeUID}
	snapshot := api.NewTopologySnapshotWithAllocationState(1,
		map[api.ProviderNodeKey]api.NodeTopologyFacts{key: facts},
		map[api.ProviderNodeKey]api.ProviderNodeSyncState{key: api.ProviderNodeSynced},
		nil,
		map[corev1.ResourceName]struct{}{"nvidia.com/gpu": {}},
	)
	plugin, session := openCandidatePlugin(t, job, snapshot)

	assertCandidateFitReason(t, plugin.predicate(session, task, node), deviceTopologyDataNotReadyReason)
}

func TestDeviceTopologyCandidateHardLocalDomainRequiresSyncedMatchingFacts(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()

	tests := []struct {
		name      string
		readiness api.ProviderNodeSyncState
		mutate    func(*api.NodeTopologyFacts)
		want      string
	}{
		{
			name:      "rejects facts before the provider has completed initial sync",
			readiness: api.ProviderNodePending,
			want:      deviceTopologyDataNotReadyReason,
		},
		{
			name:      "does not use topology facts for another resource",
			readiness: api.ProviderNodeSynced,
			mutate: func(facts *api.NodeTopologyFacts) {
				setCandidateDomainResource(facts, 0, "example.com/fpga")
			},
			want: deviceTopologyDataNotReadyReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
			job := api.NewJobInfo("job", task)
			job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
			facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
			if test.mutate != nil {
				test.mutate(&facts)
			}
			snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{facts}, map[types.UID]api.ProviderNodeSyncState{
				node.Node.UID: test.readiness,
			})
			plugin, session := openCandidatePlugin(t, job, snapshot)

			assertCandidateFitReason(t, plugin.predicate(session, task, node), test.want)
		})
	}
}

func TestDeviceTopologyCandidateSoftPreferencesDoNotReject(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()

	tests := []struct {
		name       string
		domains    []candidateDomainSpec
		withFabric bool
		wantScore  float64
	}{
		{
			name:       "matching local domain and fabric receive full preference",
			domains:    []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}},
			withFabric: true,
			wantScore:  1,
		},
		{
			name:      "missing fabric keeps the local preference only",
			domains:   []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}},
			wantScore: 0.5,
		},
		{
			name:      "fragmented local domains receive no preference",
			domains:   []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}, {value: "domain-b", deviceCount: 1, healthy: 1}},
			wantScore: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
			job := api.NewJobInfo("job", task)
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)
			policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.SoftDeviceTopologyMode}
			job.DeviceTopology = policy
			plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, test.domains, now.Add(time.Hour), test.withFabric))

			if err := plugin.predicate(session, task, node); err != nil {
				t.Fatalf("predicate() error = %v, want nil for soft policy", err)
			}
			score, err := plugin.nodeOrder(session, task, node)
			if err != nil {
				t.Fatalf("nodeOrder() error = %v", err)
			}
			if score != test.wantScore {
				t.Fatalf("nodeOrder() score = %v, want %v", score, test.wantScore)
			}
		})
	}
}

func TestDeviceTopologyCandidateSoftPolicyAllowsUnavailableTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()

	tests := []struct {
		name       string
		noSnapshot bool
		freshUntil time.Time
		readiness  api.ProviderNodeSyncState
	}{
		{
			name:       "missing snapshot",
			noSnapshot: true,
		},
		{
			name:       "pending provider facts",
			freshUntil: now.Add(time.Hour),
			readiness:  api.ProviderNodePending,
		},
		{
			name:       "stale provider facts",
			freshUntil: now.Add(-time.Hour),
			readiness:  api.ProviderNodeSynced,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
			job := api.NewJobInfo("job", task)
			job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)

			var snapshot *api.TopologySnapshot
			if !test.noSnapshot {
				facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, test.freshUntil)
				snapshot = newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{facts}, map[types.UID]api.ProviderNodeSyncState{
					node.Node.UID: test.readiness,
				})
			}
			plugin, session := openCandidatePlugin(t, job, snapshot)

			if err := plugin.predicate(session, task, node); err != nil {
				t.Fatalf("predicate() error = %v, want nil for soft policy", err)
			}
			score, err := plugin.nodeOrder(session, task, node)
			if err != nil {
				t.Fatalf("nodeOrder() error = %v", err)
			}
			if score != 0 {
				t.Fatalf("nodeOrder() score = %v, want 0", score)
			}
		})
	}
}

func TestDeviceTopologyCandidateEvaluatesMixedRequirementModes(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	newCandidate := func(t *testing.T, gpuHealthy, fpgaHealthy int) (*deviceTopologyPlugin, *framework.Session, *api.TaskInfo, *api.NodeInfo) {
		t.Helper()
		node := newCandidateTestNode("node-a")
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
				Mode:     scheduling.SoftDeviceTopologyMode,
			},
		}}
		facts := newCandidateTopologyFacts(node, []candidateDomainSpec{
			{value: "gpu-domain", deviceCount: 1, healthy: gpuHealthy},
			{value: "fpga-domain", deviceCount: 1, healthy: fpgaHealthy},
		}, time.Now().Add(time.Hour))
		setCandidateDomainResource(&facts, 1, "example.com/fpga")
		plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{facts}, nil))
		return plugin, session, task, node
	}

	t.Run("hard mismatch rejects even when the soft requirement matches", func(t *testing.T) {
		plugin, session, task, node := newCandidate(t, 0, 1)
		assertCandidateFitReason(t, plugin.predicate(session, task, node), deviceTopologyDeviceUnhealthyReason)
	})

	t.Run("soft mismatch does not reject a hard local match", func(t *testing.T) {
		plugin, session, task, node := newCandidate(t, 1, 0)
		if err := plugin.predicate(session, task, node); err != nil {
			t.Fatalf("predicate() error = %v, want nil", err)
		}
		score, err := plugin.nodeOrder(session, task, node)
		if err != nil {
			t.Fatalf("nodeOrder() error = %v", err)
		}
		if score != 0 {
			t.Fatalf("nodeOrder() score = %v, want 0 for the unmatched soft requirement", score)
		}
	})
}

func TestDeviceTopologyCandidateUsesTaskSpecificDeviceQuantity(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	node := newCandidateTestNode("node-a")
	oneDeviceTask := newDeviceTopologyTask("a-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	twoDeviceTask := newDeviceTopologyTask("z-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
	job := api.NewJobInfo("job", oneDeviceTask, twoDeviceTask)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, time.Now().Add(time.Hour), false))

	if err := plugin.predicate(session, oneDeviceTask, node); err != nil {
		t.Fatalf("predicate() error for one-device Task = %v, want nil", err)
	}
	assertCandidateFitReason(t, plugin.predicate(session, twoDeviceTask, node), deviceTopologyDomainFragmentedReason)
}

func TestDeviceTopologyCandidateHardFabric(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()

	tests := []struct {
		name       string
		withFabric bool
		wantReason string
	}{
		{name: "requires a declared fabric", wantReason: deviceTopologyFabricUnavailableReason},
		{name: "accepts a matching declared fabric", withFabric: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
			job := api.NewJobInfo("job", task)
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
			policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
			job.DeviceTopology = policy
			plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 2, healthy: 2}}, now.Add(time.Hour), test.withFabric))

			err := plugin.predicate(session, task, node)
			if test.wantReason == "" {
				if err != nil {
					t.Fatalf("predicate() error = %v, want nil", err)
				}
				return
			}
			assertCandidateFitReason(t, err, test.wantReason)
		})
	}
}

func TestDeviceTopologyCandidateHardFabricAcceptsCrossNodeFabric(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	freshUntil := time.Now().Add(time.Hour)
	factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, freshUntil)
	factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, freshUntil)
	factsA.Fabrics = []api.FabricDomain{newCandidateFabric(factsA, candidateFabricMember(factsA), candidateFabricMember(factsB))}
	snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{factsA, factsB}, nil)
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, snapshot)

	if err := plugin.predicate(session, task, nodeB); err != nil {
		t.Fatalf("predicate() error = %v, want nil for a valid cross-Node fabric", err)
	}
}

func TestDeviceTopologyCandidateHardFabricRejectsUnavailableOwner(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	now := time.Now()

	tests := []struct {
		name            string
		ownerFreshUntil time.Time
		ownerReadiness  api.ProviderNodeSyncState
		wantReason      string
	}{
		{
			name:            "pending owner",
			ownerFreshUntil: now.Add(time.Hour),
			ownerReadiness:  api.ProviderNodePending,
			wantReason:      deviceTopologyDataNotReadyReason,
		},
		{
			name:            "stale owner",
			ownerFreshUntil: now.Add(-time.Hour),
			ownerReadiness:  api.ProviderNodeSynced,
			wantReason:      deviceTopologyStaleReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ownerNode := newCandidateTestNode("owner-node")
			candidateNode := newCandidateTestNode("candidate-node")
			ownerFacts := newCandidateTopologyFacts(ownerNode, []candidateDomainSpec{{value: "owner-domain", deviceCount: 1, healthy: 1}}, test.ownerFreshUntil)
			candidateFacts := newCandidateTopologyFacts(candidateNode, []candidateDomainSpec{{value: "candidate-domain", deviceCount: 1, healthy: 1}}, now.Add(time.Hour))
			ownerFacts.Fabrics = []api.FabricDomain{newCandidateFabric(ownerFacts, candidateFabricMember(ownerFacts), candidateFabricMember(candidateFacts))}
			snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{ownerFacts, candidateFacts}, map[types.UID]api.ProviderNodeSyncState{
				ownerNode.Node.UID:     test.ownerReadiness,
				candidateNode.Node.UID: api.ProviderNodeSynced,
			})
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
			job := api.NewJobInfo("job", task)
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
			policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
			job.DeviceTopology = policy
			plugin, session := openCandidatePlugin(t, job, snapshot)

			assertCandidateFitReason(t, plugin.predicate(session, task, candidateNode), test.wantReason)
		})
	}
}

func TestDeviceTopologyCandidateHardFabricRejectsPendingMember(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	freshUntil := time.Now().Add(time.Hour)
	ownerNode := newCandidateTestNode("owner-node")
	candidateNode := newCandidateTestNode("candidate-node")
	pendingNode := newCandidateTestNode("pending-node")
	ownerFacts := newCandidateTopologyFacts(ownerNode, []candidateDomainSpec{{value: "owner-domain", deviceCount: 1, healthy: 1}}, freshUntil)
	candidateFacts := newCandidateTopologyFacts(candidateNode, []candidateDomainSpec{{value: "candidate-domain", deviceCount: 1, healthy: 1}}, freshUntil)
	pendingFacts := newCandidateTopologyFacts(pendingNode, []candidateDomainSpec{{value: "pending-domain", deviceCount: 1, healthy: 1}}, freshUntil)
	ownerFacts.Fabrics = []api.FabricDomain{newCandidateFabric(ownerFacts, candidateFabricMember(ownerFacts), candidateFabricMember(candidateFacts), candidateFabricMember(pendingFacts))}
	snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{ownerFacts, candidateFacts, pendingFacts}, map[types.UID]api.ProviderNodeSyncState{
		ownerNode.Node.UID:     api.ProviderNodeSynced,
		candidateNode.Node.UID: api.ProviderNodeSynced,
		pendingNode.Node.UID:   api.ProviderNodePending,
	})
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, snapshot)

	assertCandidateFitReason(t, plugin.predicate(session, task, candidateNode), deviceTopologyDataNotReadyReason)
}

func TestDeviceTopologyCandidateHardFabricRequiresCandidateDomainMembership(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	freshUntil := time.Now().Add(time.Hour)
	candidateNode := newCandidateTestNode("candidate-node")
	ownerNode := newCandidateTestNode("owner-node")
	candidateFacts := newCandidateTopologyFacts(candidateNode, []candidateDomainSpec{{value: "candidate-domain", deviceCount: 1, healthy: 1}}, freshUntil)
	ownerFacts := newCandidateTopologyFacts(ownerNode, []candidateDomainSpec{{value: "owner-domain", deviceCount: 1, healthy: 1}}, freshUntil)
	ownerFacts.Fabrics = []api.FabricDomain{newCandidateFabric(ownerFacts, candidateFabricMember(ownerFacts))}
	snapshot := newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{candidateFacts, ownerFacts}, nil)
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, snapshot)

	assertCandidateFitReason(t, plugin.predicate(session, task, candidateNode), deviceTopologyFabricUnavailableReason)
}

func TestDeviceTopologyCandidateHonorsIndependentRequirementAndFabricModes(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	tests := []struct {
		name            string
		requirementMode scheduling.DeviceTopologyMode
		fabricMode      scheduling.DeviceTopologyMode
		wantReason      string
		wantScore       float64
	}{
		{
			name:            "soft fabric does not reject a hard local match",
			requirementMode: scheduling.HardDeviceTopologyMode,
			fabricMode:      scheduling.SoftDeviceTopologyMode,
			wantScore:       0,
		},
		{
			name:            "hard fabric rejects after a soft local match",
			requirementMode: scheduling.SoftDeviceTopologyMode,
			fabricMode:      scheduling.HardDeviceTopologyMode,
			wantReason:      deviceTopologyFabricUnavailableReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
			job := api.NewJobInfo("job", task)
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", test.requirementMode)
			policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: test.fabricMode}
			job.DeviceTopology = policy
			plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, time.Now().Add(time.Hour), false))

			err := plugin.predicate(session, task, node)
			if test.wantReason != "" {
				assertCandidateFitReason(t, err, test.wantReason)
				return
			}
			if err != nil {
				t.Fatalf("predicate() error = %v, want nil", err)
			}
			score, err := plugin.nodeOrder(session, task, node)
			if err != nil {
				t.Fatalf("nodeOrder() error = %v", err)
			}
			if score != test.wantScore {
				t.Fatalf("nodeOrder() score = %v, want %v", score, test.wantScore)
			}
		})
	}
}

func TestDeviceTopologyCandidateHardFabricRejectsStaleMember(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	nodeA := newCandidateTestNode("node-a")
	nodeB := newCandidateTestNode("node-b")
	freshUntil := time.Now().Add(time.Hour)
	factsA := newCandidateTopologyFacts(nodeA, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, freshUntil)
	factsB := newCandidateTopologyFacts(nodeB, []candidateDomainSpec{{value: "domain-b", deviceCount: 1, healthy: 1}}, time.Now().Add(-time.Hour))
	factsA.Fabrics = []api.FabricDomain{{
		ID: api.FabricDomainID{
			ProviderID: candidateTestProviderID,
			Namespace:  candidateTestNamespace,
			Value:      "fabric-a",
		},
		OwnerProviderID:  candidateTestProviderID,
		OwnerNodeUID:     nodeA.Node.UID,
		SourceGeneration: factsA.SourceGeneration,
		Members: []api.FabricMember{
			{
				NodeName:         nodeA.Name,
				NodeUID:          nodeA.Node.UID,
				SourceGeneration: factsA.SourceGeneration,
				LocalDomainIDs:   []api.DeviceDomainID{factsA.LocalDomains[0].ID},
			},
			{
				NodeName:         nodeB.Name,
				NodeUID:          nodeB.Node.UID,
				SourceGeneration: factsB.SourceGeneration,
				LocalDomainIDs:   []api.DeviceDomainID{factsB.LocalDomains[0].ID},
			},
		},
	}}
	snapshot := api.NewTopologySnapshot(1,
		map[api.ProviderNodeKey]api.NodeTopologyFacts{
			{ProviderID: candidateTestProviderID, NodeUID: nodeA.Node.UID}: factsA,
			{ProviderID: candidateTestProviderID, NodeUID: nodeB.Node.UID}: factsB,
		},
		map[api.ProviderNodeKey]api.ProviderNodeSyncState{
			{ProviderID: candidateTestProviderID, NodeUID: nodeA.Node.UID}: api.ProviderNodeSynced,
			{ProviderID: candidateTestProviderID, NodeUID: nodeB.Node.UID}: api.ProviderNodeSynced,
		},
	)
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	job.DeviceTopology = policy
	plugin, session := openCandidatePlugin(t, job, snapshot)

	assertCandidateFitReason(t, plugin.predicate(session, task, nodeA), deviceTopologyStaleReason)
}

func TestDeviceTopologyCandidateSkipsTaskWithoutRequestedResource(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	node := newCandidateTestNode("node-a")
	gpuTask := newDeviceTopologyTask("gpu-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	cpuTask := newDeviceTopologyTask("cpu-worker", []corev1.Container{containerWithRequest("worker", corev1.ResourceCPU, "1")}, nil)
	job := api.NewJobInfo("job", gpuTask, cpuTask)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, nil)

	if err := plugin.predicate(session, cpuTask, node); err != nil {
		t.Fatalf("predicate() error = %v, want nil for a Task without a GPU request", err)
	}
	score, err := plugin.nodeOrder(session, cpuTask, node)
	if err != nil {
		t.Fatalf("nodeOrder() error = %v", err)
	}
	if score != 0 {
		t.Fatalf("nodeOrder() score = %v, want 0 for a Task without a GPU request", score)
	}
}

func TestDeviceTopologyCandidateUsesSubGroupPolicy(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	node := newCandidateTestNode("node-a")
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	subJob := job.SubJobs[job.TaskToSubJob[task.UID]]
	subJob.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)
	plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, time.Now().Add(time.Hour), false))

	if err := plugin.predicate(session, task, node); err != nil {
		t.Fatalf("predicate() error = %v, want nil", err)
	}
	score, err := plugin.nodeOrder(session, task, node)
	if err != nil {
		t.Fatalf("nodeOrder() error = %v", err)
	}
	if score != deviceTopologySoftPreferenceScore {
		t.Fatalf("nodeOrder() score = %v, want %v", score, deviceTopologySoftPreferenceScore)
	}
}

func TestDeviceTopologyCandidateSubGroupMappingSemantics(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	t.Run("parent policy applies without a subgroup mapping", func(t *testing.T) {
		node := newCandidateTestNode("node-a")
		task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
		job := api.NewJobInfo("job", task)
		job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
		delete(job.TaskToSubJob, task.UID)
		plugin, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, time.Now().Add(time.Hour), false))

		if err := plugin.predicate(session, task, node); err != nil {
			t.Fatalf("predicate() error = %v, want nil for a parent policy", err)
		}
	})

	t.Run("subgroup policy rejects an unmapped task", func(t *testing.T) {
		node := newCandidateTestNode("node-a")
		task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
		job := api.NewJobInfo("job", task)
		subJobID := job.TaskToSubJob[task.UID]
		job.SubJobs[subJobID].DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
		delete(job.TaskToSubJob, task.UID)
		plugin, session := openCandidatePlugin(t, job, nil)

		assertCandidateFitReasonContains(t, plugin.predicate(session, task, node), invalidDeviceTopologyPolicyReason)
		if _, err := plugin.nodeOrder(session, task, node); err == nil || !strings.Contains(err.Error(), invalidDeviceTopologyPolicyReason) {
			t.Fatalf("nodeOrder() error = %v, want %q", err, invalidDeviceTopologyPolicyReason)
		}
	})

	t.Run("task referencing a missing subgroup fails closed", func(t *testing.T) {
		node := newCandidateTestNode("node-a")
		task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
		job := api.NewJobInfo("job", task)
		job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
		delete(job.SubJobs, job.TaskToSubJob[task.UID])
		plugin, session := openCandidatePlugin(t, job, nil)

		assertCandidateFitReasonContains(t, plugin.predicate(session, task, node), invalidDeviceTopologyPolicyReason)
	})
}

func TestDeviceTopologyPluginRegistersCandidateCallbacks(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	enabled := true
	node := newCandidateTestNode("node-a")
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)
	_, session := openCandidatePlugin(t, job, newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "domain-a", deviceCount: 1, healthy: 1}}, time.Now().Add(time.Hour), false))
	session.Tiers = []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:             PluginName,
		EnabledPredicate: &enabled,
		EnabledNodeOrder: &enabled,
	}}}}

	if err := session.PredicateFn(task, node); err != nil {
		t.Fatalf("Session.PredicateFn() error = %v", err)
	}
	score, err := session.NodeOrderFn(task, node)
	if err != nil {
		t.Fatalf("Session.NodeOrderFn() error = %v", err)
	}
	if score != deviceTopologySoftPreferenceScore {
		t.Fatalf("Session.NodeOrderFn() score = %v, want %v", score, deviceTopologySoftPreferenceScore)
	}
}

// openCandidatePlugin creates a real framework Session so plugin callback
// registration uses the same initialized callback maps as production.
func openCandidatePlugin(t *testing.T, job *api.JobInfo, snapshot *api.TopologySnapshot) (*deviceTopologyPlugin, *framework.Session) {
	t.Helper()
	for _, task := range job.Tasks {
		task.Job = job.UID
	}
	session := newDeviceTopologyTestSession(t, map[api.JobID]*api.JobInfo{job.UID: job})
	session.DeviceTopology = snapshot
	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(session)
	return plugin, session
}

// newCandidateTestNode returns a NodeInfo with a stable Node UID suitable for
// matching immutable topology facts.
func newCandidateTestNode(name string) *api.NodeInfo {
	return &api.NodeInfo{
		Name: name,
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(name + "-uid"),
		}},
	}
}

// newCandidateTopologySnapshot creates complete facts for one test Node. A
// single optional fabric has the Node as its sole member, with all of its
// local domains.
func newCandidateTopologySnapshot(node *api.NodeInfo, domains []candidateDomainSpec, freshUntil time.Time, includeFabric bool) *api.TopologySnapshot {
	facts := newCandidateTopologyFacts(node, domains, freshUntil)
	if includeFabric && len(facts.LocalDomains) > 0 {
		facts.Fabrics = []api.FabricDomain{newCandidateFabric(facts, candidateFabricMember(facts))}
	}
	return newCandidateTopologySnapshotFromFacts([]api.NodeTopologyFacts{facts}, nil)
}

// newCandidateTopologySnapshotFromFacts creates one immutable snapshot with
// synced facts by default. Tests can mark individual Nodes pending explicitly.
func newCandidateTopologySnapshotFromFacts(facts []api.NodeTopologyFacts, readiness map[types.UID]api.ProviderNodeSyncState) *api.TopologySnapshot {
	factsByProviderNode := make(map[api.ProviderNodeKey]api.NodeTopologyFacts, len(facts))
	readinessByProviderNode := make(map[api.ProviderNodeKey]api.ProviderNodeSyncState, len(facts))
	for _, factSet := range facts {
		key := api.ProviderNodeKey{ProviderID: factSet.ProviderID, NodeUID: factSet.NodeUID}
		factsByProviderNode[key] = factSet
		state, exists := readiness[factSet.NodeUID]
		if !exists {
			state = api.ProviderNodeSynced
		}
		readinessByProviderNode[key] = state
	}
	return api.NewTopologySnapshot(1, factsByProviderNode, readinessByProviderNode)
}

// newCandidateFabric creates a provider-owned fabric from one owner fact set
// and the local domains published by each participating member.
func newCandidateFabric(owner api.NodeTopologyFacts, members ...api.FabricMember) api.FabricDomain {
	return api.FabricDomain{
		ID: api.FabricDomainID{
			ProviderID: candidateTestProviderID,
			Namespace:  candidateTestNamespace,
			Value:      "fabric-a",
		},
		OwnerProviderID:  owner.ProviderID,
		OwnerNodeUID:     owner.NodeUID,
		SourceGeneration: owner.SourceGeneration,
		Members:          members,
	}
}

// candidateFabricMember records every local domain contributed by one Node to
// a test fabric.
func candidateFabricMember(facts api.NodeTopologyFacts) api.FabricMember {
	domainIDs := make([]api.DeviceDomainID, 0, len(facts.LocalDomains))
	for _, domain := range facts.LocalDomains {
		domainIDs = append(domainIDs, domain.ID)
	}
	return api.FabricMember{
		NodeName:         facts.NodeName,
		NodeUID:          facts.NodeUID,
		SourceGeneration: facts.SourceGeneration,
		LocalDomainIDs:   domainIDs,
	}
}

// newCandidateTopologyFacts creates a complete provider fact set for one
// Node, including healthy and unhealthy device records for each local domain.
func newCandidateTopologyFacts(node *api.NodeInfo, domains []candidateDomainSpec, freshUntil time.Time) api.NodeTopologyFacts {
	facts := api.NodeTopologyFacts{
		ProviderID:          candidateTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.Node.UID,
		NodeResourceVersion: "1",
		SourceGeneration:    1,
		ObservedAt:          freshUntil.Add(-time.Minute),
		FreshUntil:          freshUntil,
	}
	for _, specification := range domains {
		domainID := api.DeviceDomainID{
			ProviderID: candidateTestProviderID,
			Namespace:  candidateTestNamespace,
			NodeUID:    node.Node.UID,
			Value:      specification.value,
		}
		domain := api.DeviceDomain{
			ID:           domainID,
			NodeName:     node.Name,
			NodeUID:      node.Node.UID,
			ResourceName: "nvidia.com/gpu",
		}
		for deviceIndex := 0; deviceIndex < specification.deviceCount; deviceIndex++ {
			deviceID := api.DeviceID{
				ProviderID: candidateTestProviderID,
				Namespace:  candidateTestNamespace,
				Value:      fmt.Sprintf("%s-device-%d", specification.value, deviceIndex),
			}
			health := api.DeviceUnhealthy
			if deviceIndex < specification.healthy {
				health = api.DeviceHealthy
			}
			facts.Devices = append(facts.Devices, api.TopologyDevice{
				ID:            deviceID,
				ResourceName:  "nvidia.com/gpu",
				NodeName:      node.Name,
				NodeUID:       node.Node.UID,
				LocalDomainID: domainID,
				Health:        health,
			})
			domain.DeviceIDs = append(domain.DeviceIDs, deviceID)
		}
		facts.LocalDomains = append(facts.LocalDomains, domain)
	}
	return facts
}

// setCandidateDomainResource changes one fixture domain and its devices to a
// different extended resource, preserving their physical identity.
func setCandidateDomainResource(facts *api.NodeTopologyFacts, domainIndex int, resourceName corev1.ResourceName) {
	domainID := facts.LocalDomains[domainIndex].ID
	facts.LocalDomains[domainIndex].ResourceName = resourceName
	for index := range facts.Devices {
		if facts.Devices[index].LocalDomainID == domainID {
			facts.Devices[index].ResourceName = resourceName
		}
	}
}

// assertCandidateFitReason checks the public structured reason returned from
// a device-topology predicate failure.
func assertCandidateFitReason(t *testing.T, err error, wantReason string) {
	t.Helper()
	fitError := candidateFitError(t, err)
	reasons := fitError.Reasons()
	if len(reasons) != 1 || reasons[0] != wantReason {
		t.Fatalf("predicate() reasons = %v, want [%q]", reasons, wantReason)
	}
}

// assertCandidateFitReasonContains checks a policy-resolution failure that
// retains its detailed message beneath a stable public reason prefix.
func assertCandidateFitReasonContains(t *testing.T, err error, wantReason string) {
	t.Helper()
	fitError := candidateFitError(t, err)
	reasons := fitError.Reasons()
	if len(reasons) != 1 || !strings.Contains(reasons[0], wantReason) {
		t.Fatalf("predicate() reasons = %v, want one reason containing %q", reasons, wantReason)
	}
}

// candidateFitError verifies the predicate's public scheduling error contract.
func candidateFitError(t *testing.T, err error) *api.FitError {
	t.Helper()
	if err == nil {
		t.Fatal("predicate() error = nil, want *api.FitError")
	}
	var fitError *api.FitError
	if !errors.As(err, &fitError) {
		t.Fatalf("predicate() error type = %T, want *api.FitError", err)
	}
	if len(fitError.Status) != 1 {
		t.Fatalf("predicate() status count = %d, want 1", len(fitError.Status))
	}
	status := fitError.Status[0]
	if status.Plugin != PluginName {
		t.Fatalf("predicate() status plugin = %q, want %q", status.Plugin, PluginName)
	}
	return fitError
}

// TestDeviceTopologyFitErrorCode checks for which reasons preempt and reclaim
// may evict Pods from the Node.
func TestDeviceTopologyFitErrorCode(t *testing.T) {
	task := &api.TaskInfo{Name: "task"}
	node := &api.NodeInfo{Name: "node"}
	tests := []struct {
		reason   string
		wantCode int
	}{
		{reason: deviceTopologyReservationConflictReason, wantCode: api.Unschedulable},
		{reason: deviceTopologyDomainFragmentedReason, wantCode: api.UnschedulableAndUnresolvable},
		{reason: deviceTopologyDataNotReadyReason, wantCode: api.UnschedulableAndUnresolvable},
		{reason: deviceTopologyStaleReason, wantCode: api.UnschedulableAndUnresolvable},
		{reason: deviceTopologyDeviceUnhealthyReason, wantCode: api.UnschedulableAndUnresolvable},
		{reason: deviceTopologyFabricUnavailableReason, wantCode: api.UnschedulableAndUnresolvable},
		{reason: invalidDeviceTopologyPolicyReason + ": policy error", wantCode: api.UnschedulableAndUnresolvable},
		{reason: unsupportedPodRequestPolicyReason + ": policy error", wantCode: api.UnschedulableAndUnresolvable},
	}
	for _, test := range tests {
		t.Run(test.reason, func(t *testing.T) {
			fitError := candidateFitError(t, newDeviceTopologyFitError(task, node, test.reason))
			if code := fitError.Status[0].Code; code != test.wantCode {
				t.Fatalf("status code = %v, want %v", code, test.wantCode)
			}
		})
	}
}
