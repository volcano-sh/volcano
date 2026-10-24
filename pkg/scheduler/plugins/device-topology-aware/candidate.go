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

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	deviceTopologyDataNotReadyReason        = "DeviceTopologyDataNotReady"
	deviceTopologyDomainFragmentedReason    = "DeviceTopologyDomainFragmented"
	deviceTopologyFabricUnavailableReason   = "DeviceTopologyFabricUnavailable"
	deviceTopologyDeviceUnhealthyReason     = "DeviceTopologyDeviceUnhealthy"
	deviceTopologyReservationConflictReason = "DeviceTopologyReservationConflict"
	deviceTopologyStaleReason               = "DeviceTopologyStale"
	deviceTopologySoftPreferenceScore       = 1.0
)

// topologyCandidateEvaluation records static snapshot feasibility for one
// candidate Node. It never selects or reserves a concrete device ID.
type topologyCandidateEvaluation struct {
	fittingDomains []api.DeviceDomain
	reason         string
}

// requirementCandidateEvaluation records one selected Task requirement and
// the local-domain result for the candidate Node.
type requirementCandidateEvaluation struct {
	requirement resolvedDeviceTopologyRequirement
	local       topologyCandidateEvaluation
}

// selectedDeviceTopologyRequirement pairs one policy requirement with the
// exact request made by the Task being evaluated.
type selectedDeviceTopologyRequirement struct {
	requirement resolvedDeviceTopologyRequirement
	request     resolvedDeviceTopologyTaskRequest
}

// predicate rejects a candidate only when a selected hard requirement cannot
// use the immutable session topology snapshot. Exact-ID reservation and
// allocation-adapter enforcement checks are added later.
func (plugin *deviceTopologyPlugin) predicate(session *framework.Session, task *api.TaskInfo, node *api.NodeInfo) error {
	policy, requirements, fabric, err := plugin.evaluateTaskCandidate(session, task, node, time.Now())
	if err != nil {
		return newDeviceTopologyFitError(task, node, err.Error())
	}
	if policy == nil {
		return nil
	}

	for _, evaluation := range requirements {
		if evaluation.requirement.mode == scheduling.HardDeviceTopologyMode && evaluation.local.reason != "" {
			return newDeviceTopologyFitError(task, node, evaluation.local.reason)
		}
	}
	if policy.hasFabric && policy.fabricMode == scheduling.HardDeviceTopologyMode && fabric.reason != "" {
		return newDeviceTopologyFitError(task, node, fabric.reason)
	}
	return nil
}

// nodeOrder returns a small deterministic tie-break score for satisfied soft
// device-topology preferences. It never makes an otherwise ineligible Node
// eligible and deliberately does not encode binpack or spread policy.
func (plugin *deviceTopologyPlugin) nodeOrder(session *framework.Session, task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
	policy, requirements, fabric, err := plugin.evaluateTaskCandidate(session, task, node, time.Now())
	if err != nil {
		return 0, err
	}
	if policy == nil {
		return 0, nil
	}

	preferenceCount := 0
	matchedPreferences := 0
	for _, evaluation := range requirements {
		if evaluation.requirement.mode != scheduling.SoftDeviceTopologyMode {
			continue
		}
		preferenceCount++
		if evaluation.local.reason == "" {
			matchedPreferences++
		}
	}
	if policy.hasFabric && policy.fabricMode == scheduling.SoftDeviceTopologyMode {
		preferenceCount++
		if fabric.reason == "" {
			matchedPreferences++
		}
	}
	if preferenceCount == 0 {
		return 0, nil
	}

	return deviceTopologySoftPreferenceScore * float64(matchedPreferences) / float64(preferenceCount), nil
}

// evaluateTaskCandidate resolves the active policy for one Task and evaluates
// its selected requirements against one immutable topology snapshot.
func (plugin *deviceTopologyPlugin) evaluateTaskCandidate(session *framework.Session, task *api.TaskInfo, node *api.NodeInfo, at time.Time) (*resolvedDeviceTopologyPolicy, []requirementCandidateEvaluation, topologyCandidateEvaluation, error) {
	policy, requirements, err := plugin.requirementsForTask(session, task)
	if err != nil || policy == nil {
		return policy, nil, topologyCandidateEvaluation{}, err
	}

	evaluations := make([]requirementCandidateEvaluation, 0, len(requirements))
	for _, requirement := range requirements {
		evaluations = append(evaluations, requirementCandidateEvaluation{
			requirement: requirement.requirement,
			local:       evaluateLocalDomain(session.DeviceTopology, node, requirement.request, at),
		})
	}

	fabric := topologyCandidateEvaluation{}
	if policy.hasFabric {
		if len(evaluations) != 1 {
			return nil, nil, topologyCandidateEvaluation{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "fabric policy for Job %q resolved %d requirements", policy.jobID, len(evaluations))
		}
		if evaluations[0].local.reason != "" {
			fabric.reason = evaluations[0].local.reason
		} else {
			fabric = evaluateFabric(session.DeviceTopology, evaluations[0].local.fittingDomains, at)
		}
	}

	return policy, evaluations, fabric, nil
}

// requirementsForTask returns only policy requirements that selected the
// Task's concrete device request. Tasks without a matching request remain in
// normal Volcano scheduling and receive no topology filter or score.
func (plugin *deviceTopologyPlugin) requirementsForTask(session *framework.Session, task *api.TaskInfo) (*resolvedDeviceTopologyPolicy, []selectedDeviceTopologyRequirement, error) {
	if session == nil || task == nil {
		return nil, nil, nil
	}
	job, exists := session.Jobs[task.Job]
	if !exists || job == nil {
		return nil, nil, nil
	}

	subJob, err := topologySubJobForTask(job, task)
	if err != nil {
		return nil, nil, err
	}
	policy, err := plugin.policyFor(job, subJob)
	if err != nil || policy == nil {
		return policy, nil, err
	}

	result := make([]selectedDeviceTopologyRequirement, 0, len(policy.requirements))
	for _, requirement := range policy.requirements {
		for _, request := range requirement.tasks {
			if request.taskID == task.UID {
				result = append(result, selectedDeviceTopologyRequirement{requirement: requirement, request: request})
				break
			}
		}
	}
	if len(result) == 0 {
		return nil, nil, nil
	}
	return policy, result, nil
}

// topologySubJobForTask returns the subgroup that owns a Task. A missing
// mapping is a fail-closed policy error only when the Job has a subgroup
// device-topology policy; parent policies do not require a subgroup mapping.
func topologySubJobForTask(job *api.JobInfo, task *api.TaskInfo) (*api.SubJobInfo, error) {
	if job == nil || task == nil {
		return nil, nil
	}
	subJobID, mapped := job.TaskToSubJob[task.UID]
	if !mapped {
		if hasSubGroupDeviceTopologyPolicy(job) {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Task %q has no SubJob mapping in Job %q", task.UID, job.UID)
		}
		return nil, nil
	}
	subJob, exists := job.SubJobs[subJobID]
	if !exists || subJob == nil {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Task %q references missing SubJob %q in Job %q", task.UID, subJobID, job.UID)
	}
	return subJob, nil
}

// hasSubGroupDeviceTopologyPolicy reports whether any existing subgroup owns
// an independent device-topology policy.
func hasSubGroupDeviceTopologyPolicy(job *api.JobInfo) bool {
	for _, subJob := range job.SubJobs {
		if subJob != nil && subJob.DeviceTopology != nil {
			return true
		}
	}
	return false
}

// evaluateLocalDomain finds available devices from one fresh local domain that
// can satisfy the Task's whole-device request. Provider health determines the
// diagnostic reason, while the scheduler-owned availability ledger prevents a
// held or allocated device from being selected again.
func evaluateLocalDomain(snapshot *api.TopologySnapshot, node *api.NodeInfo, request resolvedDeviceTopologyTaskRequest, at time.Time) topologyCandidateEvaluation {
	if snapshot == nil || node == nil || node.Node == nil || node.Node.UID == "" {
		return topologyCandidateEvaluation{reason: deviceTopologyDataNotReadyReason}
	}

	domains := snapshot.LocalDomains(node.Node.UID, request.resourceName)
	if len(domains) == 0 {
		return topologyCandidateEvaluation{reason: deviceTopologyDataNotReadyReason}
	}

	result := topologyCandidateEvaluation{fittingDomains: make([]api.DeviceDomain, 0, len(domains))}
	hasSyncedFacts := false
	hasFreshFacts := false
	hasHealthyDevice := false
	hasSufficientHealthyDomain := false
	for _, domain := range domains {
		if snapshot.ProviderNodeReadiness(domain.ID.ProviderID, node.Node.UID) != api.ProviderNodeSynced {
			continue
		}
		hasSyncedFacts = true
		if !snapshot.IsProviderNodeFreshAt(domain.ID.ProviderID, node.Node.UID, at) {
			continue
		}
		hasFreshFacts = true
		healthyDeviceIDs := snapshot.HealthyDeviceIDs(domain.ID)
		if len(healthyDeviceIDs) > 0 {
			hasHealthyDevice = true
		}
		if int64(len(healthyDeviceIDs)) >= request.quantity {
			hasSufficientHealthyDomain = true
		}
		if int64(len(snapshot.AvailableDeviceIDs(domain.ID))) >= request.quantity {
			result.fittingDomains = append(result.fittingDomains, domain)
		}
	}
	if len(result.fittingDomains) > 0 {
		return result
	}
	if !hasSyncedFacts {
		result.reason = deviceTopologyDataNotReadyReason
		return result
	}
	if !hasFreshFacts {
		result.reason = deviceTopologyStaleReason
		return result
	}
	if !hasHealthyDevice {
		result.reason = deviceTopologyDeviceUnhealthyReason
		return result
	}
	if hasSufficientHealthyDomain {
		result.reason = deviceTopologyReservationConflictReason
		return result
	}
	result.reason = deviceTopologyDomainFragmentedReason
	return result
}

// evaluateFabric checks whether any fitting local domain belongs to one valid,
// current fabric. Gang planning later selects one fabric for the complete
// policy unit; this per-Task check only verifies candidate membership.
func evaluateFabric(snapshot *api.TopologySnapshot, domains []api.DeviceDomain, at time.Time) topologyCandidateEvaluation {
	if snapshot == nil {
		return topologyCandidateEvaluation{reason: deviceTopologyDataNotReadyReason}
	}

	candidateDomains := make(map[api.DeviceDomainID]struct{}, len(domains))
	for _, domain := range domains {
		candidateDomains[domain.ID] = struct{}{}
	}
	hasPendingMember := false
	hasStaleMember := false
	for _, fabric := range snapshot.Fabrics() {
		if !fabricIncludesDomain(fabric, candidateDomains) {
			continue
		}
		if reason := fabricFreshnessReason(snapshot, fabric, at); reason == "" {
			return topologyCandidateEvaluation{}
		} else if reason == deviceTopologyDataNotReadyReason {
			hasPendingMember = true
		} else {
			hasStaleMember = true
		}
	}
	if hasPendingMember {
		return topologyCandidateEvaluation{reason: deviceTopologyDataNotReadyReason}
	}
	if hasStaleMember {
		return topologyCandidateEvaluation{reason: deviceTopologyStaleReason}
	}
	return topologyCandidateEvaluation{reason: deviceTopologyFabricUnavailableReason}
}

// fabricIncludesDomain reports whether a fabric's declared membership contains
// any currently fitting local domain on the candidate Node.
func fabricIncludesDomain(fabric api.FabricDomain, domains map[api.DeviceDomainID]struct{}) bool {
	for _, member := range fabric.Members {
		for _, domainID := range member.LocalDomainIDs {
			if _, exists := domains[domainID]; exists {
				return true
			}
		}
	}
	return false
}

// fabricFreshnessReason verifies that the fabric owner and every declared
// member still have synced, fresh facts in the same session snapshot.
func fabricFreshnessReason(snapshot *api.TopologySnapshot, fabric api.FabricDomain, at time.Time) string {
	if snapshot.ProviderNodeReadiness(fabric.OwnerProviderID, fabric.OwnerNodeUID) != api.ProviderNodeSynced {
		return deviceTopologyDataNotReadyReason
	}
	if !snapshot.IsProviderNodeFreshAt(fabric.OwnerProviderID, fabric.OwnerNodeUID, at) {
		return deviceTopologyStaleReason
	}
	for _, member := range fabric.Members {
		if snapshot.ProviderNodeReadiness(fabric.ID.ProviderID, member.NodeUID) != api.ProviderNodeSynced {
			return deviceTopologyDataNotReadyReason
		}
		if !snapshot.IsProviderNodeFreshAt(fabric.ID.ProviderID, member.NodeUID, at) {
			return deviceTopologyStaleReason
		}
	}
	return ""
}

// newDeviceTopologyFitError creates a structured unschedulable result for a
// hard policy failure without conflating it with ordinary resource fit. Only a
// reservation conflict can be solved by evicting the Pods that hold the
// devices. Every other reason is UnschedulableAndUnresolvable, so preempt and
// reclaim do not evict Pods from the Node for nothing.
func newDeviceTopologyFitError(task *api.TaskInfo, node *api.NodeInfo, reason string) error {
	code := api.UnschedulableAndUnresolvable
	if reason == deviceTopologyReservationConflictReason {
		code = api.Unschedulable
	}
	return api.NewFitErrWithStatus(task, node, &api.Status{
		Code:   code,
		Plugin: PluginName,
		Reason: reason,
	})
}
