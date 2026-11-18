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
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

// pendingTestNode is one Node of a pending-allocation test and its GPU
// domains.
type pendingTestNode struct {
	name    string
	domains []candidateDomainSpec
	fabrics []api.FabricDomain
}

// TestDeviceTopologyPluginPlacesGangAcrossNodes allocates a gang of two
// two-GPU Pods where each Node has only one free two-GPU domain. After the
// first Pod is allocated, the second must not be placed on the same Node.
func TestDeviceTopologyPluginPlacesGangAcrossNodes(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	for _, withFabric := range []bool{false, true} {
		name := "hard local domains"
		if withFabric {
			name = "hard local domains in one fabric"
		}
		t.Run(name, func(t *testing.T) {
			nodeA := pendingTestNode{name: "node-a", domains: []candidateDomainSpec{{value: "a-0", deviceCount: 1, healthy: 1}, {value: "a-1", deviceCount: 1, healthy: 1}, {value: "a-2", deviceCount: 2, healthy: 2}}}
			nodeB := pendingTestNode{name: "node-b", domains: []candidateDomainSpec{{value: "b-0", deviceCount: 2, healthy: 2}}}
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
			if withFabric {
				nodeA.fabrics = []api.FabricDomain{pendingTestFabric("fabric-1", "node-a", map[string]string{"node-a": "a-2", "node-b": "b-0"})}
				policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
			}
			session, allocationAdapter, tasks := newPendingTestSession(t, []pendingTestNode{nodeA, nodeB}, policy, 2)
			plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
			plugin.OnSessionOpen(session)
			statement := framework.NewStatement(session)

			if err := plugin.predicate(session, tasks[0], session.Nodes["node-a"]); err != nil {
				t.Fatalf("predicate(task 0, node-a) error = %v", err)
			}
			if err := statement.Allocate(tasks[0], session.Nodes["node-a"]); err != nil {
				t.Fatalf("Allocate(task 0, node-a) error = %v", err)
			}

			// node-a has 4 GPUs, but its only two-GPU domain is pending for
			// task 0, so task 1 must go to node-b.
			assertCandidateFitReason(t, plugin.predicate(session, tasks[1], session.Nodes["node-a"]), deviceTopologyDomainFragmentedReason)
			if err := plugin.predicate(session, tasks[1], session.Nodes["node-b"]); err != nil {
				t.Fatalf("predicate(task 1, node-b) error = %v", err)
			}
			if err := statement.Allocate(tasks[1], session.Nodes["node-b"]); err != nil {
				t.Fatalf("Allocate(task 1, node-b) error = %v", err)
			}

			preparation, err := plugin.PrepareBindBatch(context.Background(), batchWaveBindContexts(tasks, []int{0, 1}))
			if err != nil {
				t.Fatalf("PrepareBindBatch() error = %v, want a gang plan across both Nodes", err)
			}
			if len(allocationAdapter.reservedPlans) != 1 || len(allocationAdapter.reservedPlans[0].Placements) != 2 {
				t.Fatalf("Reserve() plans = %#v, want one plan with two placements", allocationAdapter.reservedPlans)
			}
			if withFabric && allocationAdapter.reservedPlans[0].FabricID.Value != "fabric-1" {
				t.Fatalf("Reserve() fabric = %#v, want fabric-1", allocationAdapter.reservedPlans[0].FabricID)
			}
			if err := preparation.Commit(context.Background()); err != nil {
				t.Fatalf("Commit() error = %v", err)
			}
			if len(plugin.pending.tasks) != 0 || len(plugin.pending.byDomain) != 0 || len(plugin.pending.fabrics) != 0 {
				t.Fatalf("pending allocations after commit = %#v, want none", plugin.pending)
			}
		})
	}
}

// TestDeviceTopologyPluginPinsGangToOneFabric checks that the later Pods of a
// hard fabric gang use the fabric its first Pod pinned, even when another
// fabric has room on the same Node.
func TestDeviceTopologyPluginPinsGangToOneFabric(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	nodeA := pendingTestNode{name: "node-a", domains: []candidateDomainSpec{{value: "a-2", deviceCount: 2, healthy: 2}, {value: "a-3", deviceCount: 2, healthy: 2}}}
	nodeA.fabrics = []api.FabricDomain{
		pendingTestFabric("fabric-1", "node-a", map[string]string{"node-a": "a-2", "node-b": "b-0"}),
		pendingTestFabric("fabric-2", "node-a", map[string]string{"node-a": "a-3", "node-c": "c-0"}),
	}
	nodeB := pendingTestNode{name: "node-b", domains: []candidateDomainSpec{{value: "b-0", deviceCount: 2, healthy: 2}}}
	nodeC := pendingTestNode{name: "node-c", domains: []candidateDomainSpec{{value: "c-0", deviceCount: 2, healthy: 2}}}
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
	session, _, tasks := newPendingTestSession(t, []pendingTestNode{nodeA, nodeB, nodeC}, policy, 2)
	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(session)
	statement := framework.NewStatement(session)

	if err := statement.Allocate(tasks[0], session.Nodes["node-a"]); err != nil {
		t.Fatalf("Allocate(task 0, node-a) error = %v", err)
	}
	if pinned := plugin.pending.fabrics[plugin.pending.tasks[tasks[0].UID].policy]; pinned == nil || pinned.id.Value != "fabric-1" {
		t.Fatalf("pinned fabric = %#v, want fabric-1", pinned)
	}

	// a-3 on node-a and c-0 on node-c are free, but only in fabric-2.
	assertCandidateFitReason(t, plugin.predicate(session, tasks[1], session.Nodes["node-a"]), deviceTopologyFabricUnavailableReason)
	assertCandidateFitReason(t, plugin.predicate(session, tasks[1], session.Nodes["node-c"]), deviceTopologyFabricUnavailableReason)
	if err := plugin.predicate(session, tasks[1], session.Nodes["node-b"]); err != nil {
		t.Fatalf("predicate(task 1, node-b) error = %v", err)
	}

	// Undoing the allocation releases the pin.
	statement.Discard()
	if len(plugin.pending.tasks) != 0 || len(plugin.pending.byDomain) != 0 || len(plugin.pending.fabrics) != 0 {
		t.Fatalf("pending allocations after Discard() = %#v, want none", plugin.pending)
	}
}

// pendingTestFabric returns a fabric owned by owner over one local domain on
// each member Node. Every test Node publishes source generation 1.
func pendingTestFabric(value, owner string, domainByNode map[string]string) api.FabricDomain {
	fabric := api.FabricDomain{
		ID:               api.FabricDomainID{ProviderID: candidateTestProviderID, Namespace: candidateTestNamespace, Value: value},
		OwnerProviderID:  candidateTestProviderID,
		OwnerNodeUID:     types.UID(owner + "-uid"),
		SourceGeneration: 1,
	}
	for _, nodeName := range []string{"node-a", "node-b", "node-c"} {
		domain, exists := domainByNode[nodeName]
		if !exists {
			continue
		}
		nodeUID := types.UID(nodeName + "-uid")
		fabric.Members = append(fabric.Members, api.FabricMember{
			NodeName:         nodeName,
			NodeUID:          nodeUID,
			SourceGeneration: 1,
			LocalDomainIDs:   []api.DeviceDomainID{{ProviderID: candidateTestProviderID, Namespace: candidateTestNamespace, NodeUID: nodeUID, Value: domain}},
		})
	}
	return fabric
}

// newPendingTestSession opens a session over Nodes with trusted GPU facts and
// one Job whose taskCount Tasks each request two GPUs.
func newPendingTestSession(t *testing.T, nodes []pendingTestNode, policy *scheduling.DeviceTopologySpec, taskCount int) (*framework.Session, *uniqueTokenTestAdapter, []*api.TaskInfo) {
	t.Helper()
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

	schedulerCache := cache.NewDefaultMockSchedulerCache("device-topology-pending-test")
	t.Cleanup(func() {
		cache.ShutdownMockSchedulerCache(schedulerCache)
	})
	cache.WithDeviceTopologyProviderRegistry(providerRegistry)(schedulerCache)
	cache.WithDeviceTopologyAdapterRegistry(adapterRegistry)(schedulerCache)
	freshUntil := time.Now().Add(time.Minute)
	for _, testNode := range nodes {
		gpus := 0
		for _, domain := range testNode.domains {
			gpus += domain.deviceCount
		}
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: testNode.name, UID: types.UID(testNode.name + "-uid"), ResourceVersion: "1"},
			Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
				"nvidia.com/gpu":      *resource.NewQuantity(int64(gpus), resource.DecimalSI),
			}},
		}
		if err := schedulerCache.AddOrUpdateNode(node); err != nil {
			t.Fatalf("AddOrUpdateNode(%s) error = %v", testNode.name, err)
		}
		facts := newCandidateTopologyFacts(&api.NodeInfo{Name: node.Name, Node: node}, testNode.domains, freshUntil)
		facts.Fabrics = testNode.fabrics
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
			t.Fatalf("ApplyProviderNodeUpdate(%s) error = %v", testNode.name, err)
		}
	}

	tasks := make([]*api.TaskInfo, 0, taskCount)
	for index := 0; index < taskCount; index++ {
		tasks = append(tasks, newDeviceTopologyTask("worker-"+string(rune('0'+index)), []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil))
	}
	job := api.NewJobInfo("job", tasks...)
	job.DeviceTopology = policy
	for _, task := range tasks {
		task.Job = job.UID
	}

	session := framework.OpenSession(schedulerCache, nil, nil)
	session.Jobs = map[api.JobID]*api.JobInfo{job.UID: job}
	t.Cleanup(func() {
		session.Jobs = nil
		framework.CloseSession(session)
	})
	return session, allocationAdapter, tasks
}
