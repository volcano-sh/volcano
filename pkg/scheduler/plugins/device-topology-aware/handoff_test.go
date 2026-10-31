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

func TestDeviceTopologyPluginPreparesPartialHardBatch(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	tests := []struct {
		name     string
		statuses []api.TaskStatus
		batch    []int
	}{
		{
			name:     "minAvailable wave leaves later pending Tasks for a later plan",
			statuses: []api.TaskStatus{api.Allocated, api.Allocated, api.Pending, api.Pending},
			batch:    []int{0, 1},
		},
		{
			name:     "replacement Task joins running members",
			statuses: []api.TaskStatus{api.Running, api.Running, api.Running, api.Allocated},
			batch:    []int{3},
		},
		{
			name:     "completed members are ignored",
			statuses: []api.TaskStatus{api.Succeeded, api.Failed, api.Allocated, api.Allocated},
			batch:    []int{2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			node.Node.ResourceVersion = "1"
			job, tasks := newBatchWaveTestJob(node, tt.statuses, testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode))
			facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "gpu-domain", deviceCount: len(tasks), healthy: len(tasks)}}, time.Now().Add(time.Minute))
			session, allocationAdapter := newBatchBindTestSession(t, node, facts, job)
			plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
			plugin.OnSessionOpen(session)
			bindContexts := batchWaveBindContexts(tasks, tt.batch)

			preparation, err := plugin.PrepareBindBatch(context.Background(), bindContexts)
			if err != nil {
				t.Fatalf("PrepareBindBatch() error = %v", err)
			}
			if preparation == nil {
				t.Fatal("PrepareBindBatch() preparation = nil, want hard topology preparation")
			}
			t.Cleanup(func() {
				if err := preparation.Rollback(context.Background()); err != nil {
					t.Errorf("prepared bind Rollback() error = %v", err)
				}
			})
			if len(allocationAdapter.reservedPlans) != 1 || len(allocationAdapter.reservedPlans[0].Placements) != len(tt.batch) {
				t.Fatalf("Reserve() plans = %#v, want one plan with %d placements", allocationAdapter.reservedPlans, len(tt.batch))
			}
			for _, bindContext := range bindContexts {
				extension, exists := bindContext.Extensions[PluginName].(cache.DeviceTopologyBindContextExtension)
				if !exists || len(extension.Handoffs) != 1 {
					t.Fatalf("bind context for Task %q extension = %#v, want one handoff", bindContext.TaskInfo.UID, bindContext.Extensions[PluginName])
				}
			}
		})
	}
}

func TestDeviceTopologyPluginHardFabricBatchWaves(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	tests := []struct {
		name       string
		statuses   []api.TaskStatus
		batch      []int
		wantReason string
	}{
		{
			name:     "first wave plans one fabric",
			statuses: []api.TaskStatus{api.Allocated, api.Allocated, api.Pending},
			batch:    []int{0, 1},
		},
		{
			name:       "later wave is rejected while earlier members hold a fabric",
			statuses:   []api.TaskStatus{api.Running, api.Allocated, api.Pending},
			batch:      []int{1},
			wantReason: deviceTopologyFabricUnavailableReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newCandidateTestNode("node-a")
			node.Node.ResourceVersion = "1"
			policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
			policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode}
			job, tasks := newBatchWaveTestJob(node, tt.statuses, policy)
			facts := newCandidateTopologyFacts(node, []candidateDomainSpec{{value: "gpu-domain", deviceCount: len(tasks), healthy: len(tasks)}}, time.Now().Add(time.Minute))
			facts.Fabrics = []api.FabricDomain{newCandidateFabric(facts, candidateFabricMember(facts))}
			session, allocationAdapter := newBatchBindTestSession(t, node, facts, job)
			plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
			plugin.OnSessionOpen(session)
			bindContexts := batchWaveBindContexts(tasks, tt.batch)

			preparation, err := plugin.PrepareBindBatch(context.Background(), bindContexts)
			if tt.wantReason != "" {
				assertGangPlanningReason(t, err, tt.wantReason)
				if len(allocationAdapter.reservedPlans) != 0 {
					t.Fatalf("Reserve() calls = %d, want 0", len(allocationAdapter.reservedPlans))
				}
				for _, bindContext := range bindContexts {
					if _, exists := bindContext.Extensions[PluginName]; exists {
						t.Fatalf("bind context for Task %q has extension %#v, want none", bindContext.TaskInfo.UID, bindContext.Extensions[PluginName])
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("PrepareBindBatch() error = %v", err)
			}
			if preparation == nil {
				t.Fatal("PrepareBindBatch() preparation = nil, want hard topology preparation")
			}
			t.Cleanup(func() {
				if err := preparation.Rollback(context.Background()); err != nil {
					t.Errorf("prepared bind Rollback() error = %v", err)
				}
			})
			if len(allocationAdapter.reservedPlans) != 1 || allocationAdapter.reservedPlans[0].FabricID != facts.Fabrics[0].ID {
				t.Fatalf("Reserve() plans = %#v, want one plan on fabric %#v", allocationAdapter.reservedPlans, facts.Fabrics[0].ID)
			}
		})
	}
}

// newBatchWaveTestJob creates one Job with a single-GPU Task for each status.
// Every non-pending Task is placed on node.
func newBatchWaveTestJob(node *api.NodeInfo, statuses []api.TaskStatus, policy *scheduling.DeviceTopologySpec) (*api.JobInfo, []*api.TaskInfo) {
	tasks := make([]*api.TaskInfo, 0, len(statuses))
	for index := range statuses {
		tasks = append(tasks, newDeviceTopologyTask(fmt.Sprintf("worker-%d", index), []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil))
	}
	job := api.NewJobInfo("job", tasks...)
	job.DeviceTopology = policy
	for index, task := range tasks {
		task.Job = job.UID
		if statuses[index] != api.Pending {
			task.NodeName = node.Name
		}
		job.UpdateTaskStatus(task, statuses[index])
	}
	return job, tasks
}

// batchWaveBindContexts returns bind contexts for the selected Task indexes.
func batchWaveBindContexts(tasks []*api.TaskInfo, batch []int) []*cache.BindContext {
	bindContexts := make([]*cache.BindContext, 0, len(batch))
	for _, index := range batch {
		bindContexts = append(bindContexts, &cache.BindContext{TaskInfo: tasks[index], Extensions: make(map[string]cache.BindContextExtension)})
	}
	return bindContexts
}

// newBatchBindTestSession opens a session whose cache holds trusted facts for
// one Node and forwards reservations to a unique-token test adapter.
func newBatchBindTestSession(t *testing.T, node *api.NodeInfo, facts api.NodeTopologyFacts, jobs ...*api.JobInfo) (*framework.Session, *uniqueTokenTestAdapter) {
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

	schedulerCache := cache.NewDefaultMockSchedulerCache("device-topology-batch-wave-test")
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
	session.Jobs = make(map[api.JobID]*api.JobInfo, len(jobs))
	for _, job := range jobs {
		session.Jobs[job.UID] = job
	}
	t.Cleanup(func() {
		session.Jobs = nil
		framework.CloseSession(session)
	})
	return session, allocationAdapter
}
