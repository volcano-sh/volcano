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
	"time"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// pendingDeviceUse is the local domain that one requirement of an allocated,
// not yet committed Task is expected to use.
type pendingDeviceUse struct {
	domainID api.DeviceDomainID
	quantity int64
}

// pendingTaskUse records the expected devices of one allocated Task and the
// policy unit it belongs to.
type pendingTaskUse struct {
	policy *resolvedDeviceTopologyPolicy
	uses   []pendingDeviceUse
}

// pendingFabric pins a policy unit with a fabric rule to the fabric its first
// allocated Task uses, while any of its Tasks is pending. For a soft rule, the
// pin only affects scoring.
type pendingFabric struct {
	id    api.FabricDomainID
	tasks int
}

// pendingAllocations tracks devices that Tasks allocated in this session but
// not yet committed are expected to use. The predicate subtracts them, so the
// later Tasks of a gang are not placed on devices an earlier Task needs. The
// topology snapshot already shows committed reservations, so committed Tasks
// are forgotten.
type pendingAllocations struct {
	tasks    map[api.TaskID]pendingTaskUse
	byDomain map[api.DeviceDomainID]int64
	fabrics  map[*resolvedDeviceTopologyPolicy]*pendingFabric
}

func newPendingAllocations() pendingAllocations {
	return pendingAllocations{
		tasks:    make(map[api.TaskID]pendingTaskUse),
		byDomain: make(map[api.DeviceDomainID]int64),
		fabrics:  make(map[*resolvedDeviceTopologyPolicy]*pendingFabric),
	}
}

func (pending *pendingAllocations) clear() {
	clear(pending.tasks)
	clear(pending.byDomain)
	clear(pending.fabrics)
}

// eventHandler records an allocated or pipelined Task, and forgets a Task when
// its allocation is undone. An unevicted running Task also raises an allocate
// event; its devices are already held, so it is ignored.
func (plugin *deviceTopologyPlugin) eventHandler() *framework.EventHandler {
	return &framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			if event.Task != nil && (event.Task.Status == api.Allocated || event.Task.Status == api.Pipelined) {
				plugin.recordPendingAllocation(event.Task)
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			if event.Task != nil {
				plugin.forgetPendingAllocation(event.Task.UID)
			}
		},
	}
}

// recordPendingAllocation chooses, for each requirement of the Task that fits
// on its Node, the fitting local domain with the fewest remaining devices, so
// larger domains stay free for later Tasks. For a fabric rule, hard or soft,
// the first Task pins the policy unit to one fabric.
func (plugin *deviceTopologyPlugin) recordPendingAllocation(task *api.TaskInfo) {
	session := plugin.session
	if session == nil {
		return
	}
	if plugin.pending.tasks == nil {
		plugin.pending = newPendingAllocations()
	}
	node, exists := session.Nodes[task.NodeName]
	if !exists {
		return
	}
	policy, evaluations, fabric, err := plugin.evaluateTaskCandidate(session, task, node, time.Now())
	if err != nil || policy == nil {
		return
	}
	plugin.forgetPendingAllocation(task.UID)

	snapshot := session.DeviceTopology
	use := pendingTaskUse{policy: policy}
	for _, evaluation := range evaluations {
		if evaluation.local.reason != "" {
			continue
		}
		domains := evaluation.local.fittingDomains
		if policy.hasFabric && fabric.reason == "" {
			fabricID, pinned := plugin.pendingFabricFor(policy, snapshot, domains)
			if !pinned {
				continue
			}
			domains = domainsInFabric(snapshot, fabricID, domains)
		}
		domainID, found := plugin.smallestFittingDomain(snapshot, domains, evaluation.request.quantity)
		if !found {
			continue
		}
		use.uses = append(use.uses, pendingDeviceUse{domainID: domainID, quantity: evaluation.request.quantity})
		plugin.pending.byDomain[domainID] += evaluation.request.quantity
	}
	if len(use.uses) == 0 {
		return
	}
	plugin.pending.tasks[task.UID] = use
	if pinned, exists := plugin.pending.fabrics[policy]; exists {
		pinned.tasks++
	}
}

// forgetPendingAllocation removes a Task's expected devices. It is safe to
// call for a Task that was never recorded.
func (plugin *deviceTopologyPlugin) forgetPendingAllocation(taskID api.TaskID) {
	use, exists := plugin.pending.tasks[taskID]
	if !exists {
		return
	}
	delete(plugin.pending.tasks, taskID)
	for _, deviceUse := range use.uses {
		plugin.pending.byDomain[deviceUse.domainID] -= deviceUse.quantity
		if plugin.pending.byDomain[deviceUse.domainID] <= 0 {
			delete(plugin.pending.byDomain, deviceUse.domainID)
		}
	}
	if pinned, exists := plugin.pending.fabrics[use.policy]; exists {
		pinned.tasks--
		if pinned.tasks <= 0 {
			delete(plugin.pending.fabrics, use.policy)
		}
	}
}

// pendingFabricFor returns the fabric a policy unit is pinned to, or pins it
// to the first valid fabric, in stable ID order, that holds one of the
// candidate domains.
func (plugin *deviceTopologyPlugin) pendingFabricFor(policy *resolvedDeviceTopologyPolicy, snapshot *api.TopologySnapshot, domains []api.DeviceDomain) (api.FabricDomainID, bool) {
	if pinned, exists := plugin.pending.fabrics[policy]; exists {
		return pinned.id, true
	}
	candidateDomains := make(map[api.DeviceDomainID]struct{}, len(domains))
	for _, domain := range domains {
		candidateDomains[domain.ID] = struct{}{}
	}
	for _, fabric := range snapshot.Fabrics() {
		if fabricIncludesDomain(fabric, candidateDomains) {
			plugin.pending.fabrics[policy] = &pendingFabric{id: fabric.ID}
			return fabric.ID, true
		}
	}
	return api.FabricDomainID{}, false
}

// domainsInFabric keeps only the domains that are members of one fabric.
func domainsInFabric(snapshot *api.TopologySnapshot, fabricID api.FabricDomainID, domains []api.DeviceDomain) []api.DeviceDomain {
	fabric, exists := snapshot.Fabric(fabricID)
	if !exists {
		return nil
	}
	members := make(map[api.DeviceDomainID]struct{})
	for _, member := range fabric.Members {
		for _, domainID := range member.LocalDomainIDs {
			members[domainID] = struct{}{}
		}
	}
	result := make([]api.DeviceDomain, 0, len(domains))
	for _, domain := range domains {
		if _, exists := members[domain.ID]; exists {
			result = append(result, domain)
		}
	}
	return result
}

// smallestFittingDomain returns the domain with the fewest remaining devices
// that still has quantity of them. Domains are already in stable ID order.
func (plugin *deviceTopologyPlugin) smallestFittingDomain(snapshot *api.TopologySnapshot, domains []api.DeviceDomain, quantity int64) (api.DeviceDomainID, bool) {
	var chosen api.DeviceDomainID
	chosenRemaining := int64(-1)
	for _, domain := range domains {
		remaining := int64(len(snapshot.AvailableDeviceIDs(domain.ID))) - plugin.pending.byDomain[domain.ID]
		if remaining < quantity {
			continue
		}
		if chosenRemaining < 0 || remaining < chosenRemaining {
			chosen, chosenRemaining = domain.ID, remaining
		}
	}
	return chosen, chosenRemaining >= 0
}
