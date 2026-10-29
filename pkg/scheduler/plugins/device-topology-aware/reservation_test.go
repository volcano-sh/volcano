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
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

func TestMaterializeDeviceTopologyPlacementPlanSelectsDistinctAvailableDevices(t *testing.T) {
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	snapshot := newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "gpu-domain", deviceCount: 2, healthy: 2}}, now.Add(time.Hour), false)
	domain := snapshot.LocalDomains(node.Node.UID, "nvidia.com/gpu")[0]
	gangPlan := &deviceTopologyGangPlan{
		jobID:            job.UID,
		schedulingUnitID: string(job.UID),
		revision:         snapshot.Revision(),
		placements: []deviceTopologyGangPlacement{
			{taskID: firstTask.UID, nodeName: node.Name, nodeUID: node.Node.UID, containerName: "trainer", resourceName: "nvidia.com/gpu", quantity: 1, localDomainID: domain.ID},
			{taskID: secondTask.UID, nodeName: node.Name, nodeUID: node.Node.UID, containerName: "trainer", resourceName: "nvidia.com/gpu", quantity: 1, localDomainID: domain.ID},
		},
	}

	placementPlan, err := materializeDeviceTopologyPlacementPlan(snapshot, job, gangPlan)
	if err != nil {
		t.Fatalf("materializeDeviceTopologyPlacementPlan() error = %v", err)
	}
	if len(placementPlan.Placements) != 2 {
		t.Fatalf("placement count = %d, want 2", len(placementPlan.Placements))
	}
	firstAssignment := placementPlan.Placements[0].Assignments[0]
	secondAssignment := placementPlan.Placements[1].Assignments[0]
	if len(firstAssignment.DeviceIDs) != 1 || len(secondAssignment.DeviceIDs) != 1 {
		t.Fatalf("assignments = %#v, want one exact device for each Task", placementPlan.Placements)
	}
	if firstAssignment.DeviceIDs[0] == secondAssignment.DeviceIDs[0] {
		t.Fatalf("both Task assignments selected device %q", firstAssignment.DeviceIDs[0].String())
	}
	if firstAssignment.LocalDomainID != domain.ID || secondAssignment.LocalDomainID != domain.ID {
		t.Fatalf("assignment domains = (%#v, %#v), want %v", firstAssignment.LocalDomainID, secondAssignment.LocalDomainID, domain.ID)
	}
}

func TestMaterializeDeviceTopologyPlacementPlanRejectsDomainOvercommit(t *testing.T) {
	now := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	node := newCandidateTestNode("node-a")
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", firstTask, secondTask)
	snapshot := newCandidateTopologySnapshot(node, []candidateDomainSpec{{value: "gpu-domain", deviceCount: 1, healthy: 1}}, now.Add(time.Hour), false)
	domain := snapshot.LocalDomains(node.Node.UID, "nvidia.com/gpu")[0]
	gangPlan := &deviceTopologyGangPlan{
		jobID:            job.UID,
		schedulingUnitID: string(job.UID),
		revision:         snapshot.Revision(),
		placements: []deviceTopologyGangPlacement{
			{taskID: firstTask.UID, nodeName: node.Name, nodeUID: node.Node.UID, containerName: "trainer", resourceName: "nvidia.com/gpu", quantity: 1, localDomainID: domain.ID},
			{taskID: secondTask.UID, nodeName: node.Name, nodeUID: node.Node.UID, containerName: "trainer", resourceName: "nvidia.com/gpu", quantity: 1, localDomainID: domain.ID},
		},
	}

	if _, err := materializeDeviceTopologyPlacementPlan(snapshot, job, gangPlan); err == nil {
		t.Fatal("materializeDeviceTopologyPlacementPlan() error = nil, want reservation conflict")
	} else {
		assertGangPlanningReason(t, err, deviceTopologyReservationConflictReason)
	}
}

func TestReserveGangPlanMaterializesAndReservesThroughSessionCache(t *testing.T) {
	now := time.Now()
	node := newCandidateTestNode("node-a")
	node.Node.ResourceVersion = "1"
	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "gpu-domain", deviceCount: 1, healthy: 1}}, now.Add(time.Minute))

	providerRegistry, err := provider.NewRegistry([]provider.Registration{{
		ProviderID:        candidateTestProviderID,
		IdentityNamespace: candidateTestNamespace,
		ResourceNames:     []corev1.ResourceName{"nvidia.com/gpu"},
	}})
	if err != nil {
		t.Fatalf("provider.NewRegistry() error = %v", err)
	}
	allocationAdapter := &reservationForwardingTestAdapter{contract: adapter.DeviceIdentityContract{
		ProviderID:   candidateTestProviderID,
		ResourceName: "nvidia.com/gpu",
		Namespace:    candidateTestNamespace,
	}}
	adapterRegistry, err := adapter.NewRegistry([]adapter.AllocationAdapter{allocationAdapter})
	if err != nil {
		t.Fatalf("adapter.NewRegistry() error = %v", err)
	}

	schedulerCache := cache.NewDefaultMockSchedulerCache("device-topology-reservation-plugin-test")
	t.Cleanup(func() {
		cache.ShutdownMockSchedulerCache(schedulerCache)
	})
	cache.WithDeviceTopologyProviderRegistry(providerRegistry)(schedulerCache)
	cache.WithDeviceTopologyAdapterRegistry(adapterRegistry)(schedulerCache)
	if err := schedulerCache.AddOrUpdateNode(node.Node); err != nil {
		t.Fatalf("AddOrUpdateNode() error = %v", err)
	}
	if err := schedulerCache.ApplyProviderNodeUpdate(provider.ProviderNodeUpdate{
		ProviderID:          facts.ProviderID,
		NodeName:            facts.NodeName,
		NodeUID:             facts.NodeUID,
		NodeResourceVersion: facts.NodeResourceVersion,
		SourceGeneration:    facts.SourceGeneration,
		ObservedAt:          facts.ObservedAt,
		FreshUntil:          facts.FreshUntil,
		Operation:           provider.ReplaceFacts,
		Facts:               &facts,
	}); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
	}

	session := framework.OpenSession(schedulerCache, nil, nil)
	session.Jobs = map[api.JobID]*api.JobInfo{job.UID: job}
	t.Cleanup(func() {
		session.Jobs = nil
		framework.CloseSession(session)
	})
	domain := session.DeviceTopology.LocalDomains(node.Node.UID, "nvidia.com/gpu")[0]
	gangPlan := &deviceTopologyGangPlan{
		jobID:            job.UID,
		schedulingUnitID: string(job.UID),
		revision:         session.DeviceTopology.Revision(),
		placements: []deviceTopologyGangPlacement{{
			taskID:        task.UID,
			nodeName:      node.Name,
			nodeUID:       node.Node.UID,
			containerName: "trainer",
			resourceName:  "nvidia.com/gpu",
			quantity:      1,
			localDomainID: domain.ID,
		}},
	}

	reservation, err := (&deviceTopologyPlugin{}).reserveGangPlan(context.Background(), session, gangPlan)
	if err != nil {
		t.Fatalf("reserveGangPlan() error = %v", err)
	}
	if len(allocationAdapter.reservedPlans) != 1 {
		t.Fatalf("Reserve() calls = %d, want 1", len(allocationAdapter.reservedPlans))
	}
	if len(reservation.AdapterReservations) != 1 || reservation.AdapterReservations[0].Token != "reservation-token" {
		t.Fatalf("reserveGangPlan() reservation = %#v, want adapter reservation", reservation)
	}
	if got := allocationAdapter.reservedPlans[0]; len(got.Placements) != 1 || len(got.Placements[0].Assignments) != 1 || len(got.Placements[0].Assignments[0].DeviceIDs) != 1 {
		t.Fatalf("Reserve() plan = %#v, want one exact device assignment", got)
	}

	if err := schedulerCache.ReleaseDeviceTopologyReservation(context.Background(), reservation); err != nil {
		t.Fatalf("ReleaseDeviceTopologyReservation() error = %v", err)
	}
}

type reservationForwardingTestAdapter struct {
	contract      adapter.DeviceIdentityContract
	reservedPlans []api.TopologyPlacementPlan
}

func (testAdapter *reservationForwardingTestAdapter) Capabilities() adapter.AdapterCapabilities {
	return adapter.AdapterCapabilities{
		ExactDeviceIDs:        true,
		AtomicReservation:     true,
		ImmutableBindHandoff:  true,
		AllocationObservation: true,
		ReleaseObservation:    true,
		Recovery:              true,
	}
}

func (testAdapter *reservationForwardingTestAdapter) IdentityContract() adapter.DeviceIdentityContract {
	return testAdapter.contract
}

func (testAdapter *reservationForwardingTestAdapter) ValidateTopology(context.Context, api.NodeTopologyFacts) error {
	return nil
}

func (testAdapter *reservationForwardingTestAdapter) Reserve(_ context.Context, plan api.TopologyPlacementPlan) (adapter.AdapterReservation, error) {
	testAdapter.reservedPlans = append(testAdapter.reservedPlans, plan.Clone())
	return adapter.AdapterReservation{Token: "reservation-token", PlanDigest: "reservation-digest"}, nil
}

func (testAdapter *reservationForwardingTestAdapter) PrepareBind(context.Context, adapter.AdapterReservation, api.TopologyTaskPlacement) (adapter.AllocationHandoff, error) {
	return adapter.AllocationHandoff{}, nil
}

func (testAdapter *reservationForwardingTestAdapter) Confirm(context.Context, adapter.AdapterReservation) (adapter.AllocationObservation, error) {
	return adapter.AllocationObservation{}, nil
}

func (testAdapter *reservationForwardingTestAdapter) Release(context.Context, adapter.AdapterReservation) error {
	return nil
}

func (testAdapter *reservationForwardingTestAdapter) Recover(context.Context) ([]adapter.AdapterReservationState, error) {
	return nil, nil
}

func TestReserveGangPlanReservesTwoGangsInOneSession(t *testing.T) {
	now := time.Now()
	node := newCandidateTestNode("node-a")
	node.Node.ResourceVersion = "1"
	firstTask := newDeviceTopologyTask("first-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	secondTask := newDeviceTopologyTask("second-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	firstJob := api.NewJobInfo("first-job", firstTask)
	secondJob := api.NewJobInfo("second-job", secondTask)
	facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "gpu-domain", deviceCount: 2, healthy: 2}}, now.Add(time.Minute))

	providerRegistry, err := provider.NewRegistry([]provider.Registration{{
		ProviderID:        candidateTestProviderID,
		IdentityNamespace: candidateTestNamespace,
		ResourceNames:     []corev1.ResourceName{"nvidia.com/gpu"},
	}})
	if err != nil {
		t.Fatalf("provider.NewRegistry() error = %v", err)
	}
	allocationAdapter := &uniqueTokenTestAdapter{reservationForwardingTestAdapter: &reservationForwardingTestAdapter{contract: adapter.DeviceIdentityContract{
		ProviderID:   candidateTestProviderID,
		ResourceName: "nvidia.com/gpu",
		Namespace:    candidateTestNamespace,
	}}}
	adapterRegistry, err := adapter.NewRegistry([]adapter.AllocationAdapter{allocationAdapter})
	if err != nil {
		t.Fatalf("adapter.NewRegistry() error = %v", err)
	}

	schedulerCache := cache.NewDefaultMockSchedulerCache("device-topology-two-gang-reservation-test")
	t.Cleanup(func() {
		cache.ShutdownMockSchedulerCache(schedulerCache)
	})
	cache.WithDeviceTopologyProviderRegistry(providerRegistry)(schedulerCache)
	cache.WithDeviceTopologyAdapterRegistry(adapterRegistry)(schedulerCache)
	if err := schedulerCache.AddOrUpdateNode(node.Node); err != nil {
		t.Fatalf("AddOrUpdateNode() error = %v", err)
	}
	if err := schedulerCache.ApplyProviderNodeUpdate(provider.ProviderNodeUpdate{
		ProviderID:          facts.ProviderID,
		NodeName:            facts.NodeName,
		NodeUID:             facts.NodeUID,
		NodeResourceVersion: facts.NodeResourceVersion,
		SourceGeneration:    facts.SourceGeneration,
		ObservedAt:          facts.ObservedAt,
		FreshUntil:          facts.FreshUntil,
		Operation:           provider.ReplaceFacts,
		Facts:               &facts,
	}); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
	}

	session := framework.OpenSession(schedulerCache, nil, nil)
	session.Jobs = map[api.JobID]*api.JobInfo{firstJob.UID: firstJob, secondJob.UID: secondJob}
	t.Cleanup(func() {
		session.Jobs = nil
		framework.CloseSession(session)
	})
	domain := session.DeviceTopology.LocalDomains(node.Node.UID, "nvidia.com/gpu")[0]
	gangPlanFor := func(job *api.JobInfo, task *api.TaskInfo) *deviceTopologyGangPlan {
		// Both plans use the session snapshot, as planGang does.
		return &deviceTopologyGangPlan{
			jobID:            job.UID,
			schedulingUnitID: string(job.UID),
			revision:         session.DeviceTopology.Revision(),
			placements: []deviceTopologyGangPlacement{{
				taskID:        task.UID,
				nodeName:      node.Name,
				nodeUID:       node.Node.UID,
				containerName: "trainer",
				resourceName:  "nvidia.com/gpu",
				quantity:      1,
				localDomainID: domain.ID,
			}},
		}
	}

	plugin := &deviceTopologyPlugin{}
	firstReservation, err := plugin.reserveGangPlan(context.Background(), session, gangPlanFor(firstJob, firstTask))
	if err != nil {
		t.Fatalf("first reserveGangPlan() error = %v", err)
	}
	secondReservation, err := plugin.reserveGangPlan(context.Background(), session, gangPlanFor(secondJob, secondTask))
	if err != nil {
		t.Fatalf("second reserveGangPlan() in the same session error = %v, want the second free device reserved", err)
	}

	firstDevice := firstReservation.Plan.Placements[0].Assignments[0].DeviceIDs[0]
	secondDevice := secondReservation.Plan.Placements[0].Assignments[0].DeviceIDs[0]
	if firstDevice == secondDevice {
		t.Fatalf("both gangs reserved device %q", firstDevice.String())
	}
	for _, reservation := range []api.TopologyReservation{secondReservation, firstReservation} {
		if err := schedulerCache.ReleaseDeviceTopologyReservation(context.Background(), reservation); err != nil {
			t.Fatalf("ReleaseDeviceTopologyReservation() error = %v", err)
		}
	}
}

// uniqueTokenTestAdapter returns a different token for each Reserve call, so
// several live reservations can be released independently.
type uniqueTokenTestAdapter struct {
	*reservationForwardingTestAdapter
}

func (testAdapter *uniqueTokenTestAdapter) Reserve(_ context.Context, plan api.TopologyPlacementPlan) (adapter.AdapterReservation, error) {
	testAdapter.reservedPlans = append(testAdapter.reservedPlans, plan.Clone())
	return adapter.AdapterReservation{
		Token:      fmt.Sprintf("reservation-token-%d", len(testAdapter.reservedPlans)),
		PlanDigest: fmt.Sprintf("reservation-digest-%d", len(testAdapter.reservedPlans)),
	}, nil
}