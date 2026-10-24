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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	defaultDeviceTopologyGangPlanMaxSearchStates = 4096
	deviceTopologyPlanningBudgetExceededReason   = "DeviceTopologyPlanningBudgetExceeded"
)

// deviceTopologyGangPlanContext contains the read-only inputs for one pure
// device-topology plan. Candidate Nodes must already have passed normal
// scheduler eligibility checks. Assigned Nodes, when supplied, constrain a
// final plan to the Nodes selected by normal allocation.
type deviceTopologyGangPlanContext struct {
	job             *api.JobInfo
	subJob          *api.SubJobInfo
	tasks           []*api.TaskInfo
	candidateNodes  map[api.TaskID][]*api.NodeInfo
	assignedNodes   map[api.TaskID]*api.NodeInfo
	at              time.Time
	maxSearchStates int
}

// deviceTopologyGangPlan records a complete static domain plan for the exact
// device-requesting Tasks in one gang admission unit. It deliberately carries
// no reservation, adapter token, or concrete device IDs; the availability
// ledger and allocation handoff add those in later phases.
type deviceTopologyGangPlan struct {
	jobID            api.JobID
	schedulingUnitID string
	revision         api.TopologyRevision
	fabricID         api.FabricDomainID
	placements       []deviceTopologyGangPlacement
}

// deviceTopologyGangPlacement records the Node-local domain selected for one
// Task container resource request in a static gang plan.
type deviceTopologyGangPlacement struct {
	taskID        api.TaskID
	nodeName      string
	nodeUID       types.UID
	containerName string
	resourceName  corev1.ResourceName
	quantity      int64
	localDomainID api.DeviceDomainID
}

// deviceTopologyPlanningTask groups every hard request that one Task must
// satisfy on the same Node. A Task may have more than one hard resource
// requirement even though each individual requirement selects one container.
type deviceTopologyPlanningTask struct {
	task                   *api.TaskInfo
	order                  int
	candidateChoices       []deviceTopologyTaskNodeChoice
	candidateDomainCount   int
	largestRequestQuantity int64
}

// deviceTopologyTaskNodeChoice is one candidate Node and the fitting domains
// available there for every hard request of a Task.
type deviceTopologyTaskNodeChoice struct {
	node           *api.NodeInfo
	requestChoices []deviceTopologyRequestDomainChoice
}

// deviceTopologyRequestDomainChoice records the local domains that can
// satisfy one Task resource request on one candidate Node.
type deviceTopologyRequestDomainChoice struct {
	request resolvedDeviceTopologyTaskRequest
	domains []api.DeviceDomain
}

// deviceTopologyGangPlanSolver searches static domain assignments without
// mutating the session, cache, candidate Nodes, or Task state.
type deviceTopologyGangPlanSolver struct {
	tasks             []deviceTopologyPlanningTask
	remainingCapacity map[api.DeviceDomainID]int64
	placements        []deviceTopologyGangPlacement
	maxSearchStates   int
	searchStates      int
	budgetExceeded    bool
}

// deviceTopologyFabricPlanningCandidate pairs one fresh, gang-compatible
// fabric with the Task choices constrained to that fabric.
type deviceTopologyFabricPlanningCandidate struct {
	fabric api.FabricDomain
	tasks  []deviceTopologyPlanningTask
}

// planGang builds a complete static plan for the exact pending Tasks in one
// existing PodGroup or SubJob admission unit. It is intentionally not
// registered as a framework callback yet: allocate integration, reservation,
// and exact-ID enforcement are separate later changes.
func (plugin *deviceTopologyPlugin) planGang(session *framework.Session, context deviceTopologyGangPlanContext) (*deviceTopologyGangPlan, error) {
	if session == nil {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning requires a scheduler session")
	}
	if context.job == nil {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning requires an owning Job")
	}
	if context.subJob != nil && context.subJob.Job != context.job.UID {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "SubJob %q does not belong to Job %q", context.subJob.UID, context.job.UID)
	}

	policy, err := plugin.policyFor(context.job, context.subJob)
	if err != nil || policy == nil || len(context.tasks) == 0 {
		return nil, err
	}

	at := context.at
	if at.IsZero() {
		at = time.Now()
	}
	planningTasks, err := buildDeviceTopologyPlanningTasks(session.DeviceTopology, policy, context, at)
	if err != nil || len(planningTasks) == 0 {
		return nil, err
	}

	maxSearchStates := context.maxSearchStates
	if maxSearchStates <= 0 {
		maxSearchStates = defaultDeviceTopologyGangPlanMaxSearchStates
	}

	if policy.hasFabric && policy.fabricMode == scheduling.HardDeviceTopologyMode {
		return planDeviceTopologyGangWithFabric(session.DeviceTopology, policy, planningTasks, maxSearchStates, at)
	}
	return planDeviceTopologyGangWithoutFabric(session.DeviceTopology, policy, planningTasks, maxSearchStates)
}

// buildDeviceTopologyPlanningTasks resolves only the hard local-domain work
// for the current plan unit. Soft-only requests remain in the score path and
// never make a gang plan mandatory.
func buildDeviceTopologyPlanningTasks(snapshot *api.TopologySnapshot, policy *resolvedDeviceTopologyPolicy, context deviceTopologyGangPlanContext, at time.Time) ([]deviceTopologyPlanningTask, error) {
	policyTaskIDs := make(map[api.TaskID]struct{}, len(policy.taskIDs))
	for _, taskID := range policy.taskIDs {
		policyTaskIDs[taskID] = struct{}{}
	}

	result := make([]deviceTopologyPlanningTask, 0, len(context.tasks))
	seenTasks := make(map[api.TaskID]struct{}, len(context.tasks))
	useAssignedNodes := len(context.assignedNodes) > 0
	for order, task := range context.tasks {
		if task == nil {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning context contains a nil Task")
		}
		if _, exists := seenTasks[task.UID]; exists {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning context repeats Task %q", task.UID)
		}
		seenTasks[task.UID] = struct{}{}
		if _, exists := policyTaskIDs[task.UID]; !exists {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Task %q does not belong to device topology policy unit %q", task.UID, policy.schedulingUnitID)
		}

		requests := planningRequestsForTask(policy, task.UID)
		if len(requests) == 0 {
			continue
		}
		candidateNodes, err := planningCandidateNodes(task, context, useAssignedNodes)
		if err != nil {
			return nil, err
		}
		planningTask, err := buildDeviceTopologyPlanningTask(snapshot, task, order, requests, candidateNodes, at)
		if err != nil {
			return nil, err
		}
		result = append(result, planningTask)
	}

	sort.SliceStable(result, func(left, right int) bool {
		if result[left].candidateDomainCount != result[right].candidateDomainCount {
			return result[left].candidateDomainCount < result[right].candidateDomainCount
		}
		if result[left].largestRequestQuantity != result[right].largestRequestQuantity {
			return result[left].largestRequestQuantity > result[right].largestRequestQuantity
		}
		return result[left].order < result[right].order
	})
	return result, nil
}

// planningRequestsForTask returns every request that requires gang planning.
// A hard fabric makes its sole resource requirement mandatory even when that
// requirement's local-domain mode is soft.
func planningRequestsForTask(policy *resolvedDeviceTopologyPolicy, taskID api.TaskID) []resolvedDeviceTopologyTaskRequest {
	result := make([]resolvedDeviceTopologyTaskRequest, 0, len(policy.requirements))
	for _, requirement := range policy.requirements {
		isHardRequirement := requirement.mode == scheduling.HardDeviceTopologyMode
		isHardFabricRequirement := policy.hasFabric && policy.fabricMode == scheduling.HardDeviceTopologyMode
		if !isHardRequirement && !isHardFabricRequirement {
			continue
		}
		for _, request := range requirement.tasks {
			if request.taskID == taskID {
				result = append(result, request)
				break
			}
		}
	}
	return result
}

// planningCandidateNodes returns deterministic, distinct candidate Nodes for
// a Task. Finalization requires one explicit assigned Node for every planned
// Task, while provisional planning uses the normal candidate set.
func planningCandidateNodes(task *api.TaskInfo, context deviceTopologyGangPlanContext, useAssignedNodes bool) ([]*api.NodeInfo, error) {
	var candidates []*api.NodeInfo
	if useAssignedNodes {
		assignedNode, exists := context.assignedNodes[task.UID]
		if !exists || assignedNode == nil {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning context has no assigned Node for Task %q", task.UID)
		}
		candidates = []*api.NodeInfo{assignedNode}
	} else {
		var exists bool
		candidates, exists = context.candidateNodes[task.UID]
		if !exists || len(candidates) == 0 {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning context has no candidate Nodes for Task %q", task.UID)
		}
	}

	uniqueNodes := make(map[types.UID]*api.NodeInfo, len(candidates))
	for _, node := range candidates {
		if node == nil || node.Node == nil || node.Node.UID == "" || planningNodeName(node) == "" {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "gang planning context has an invalid candidate Node for Task %q", task.UID)
		}
		if _, exists := uniqueNodes[node.Node.UID]; !exists {
			uniqueNodes[node.Node.UID] = node
		}
	}
	result := make([]*api.NodeInfo, 0, len(uniqueNodes))
	for _, node := range uniqueNodes {
		result = append(result, node)
	}
	sort.Slice(result, func(left, right int) bool {
		leftName := planningNodeName(result[left])
		rightName := planningNodeName(result[right])
		if leftName != rightName {
			return leftName < rightName
		}
		return string(result[left].Node.UID) < string(result[right].Node.UID)
	})
	return result, nil
}

// buildDeviceTopologyPlanningTask finds candidate Nodes where every hard
// request of one Task has a fitting local domain. It deliberately keeps all
// fitting domains so the bounded solver can backtrack when a greedy choice
// cannot complete the gang.
func buildDeviceTopologyPlanningTask(snapshot *api.TopologySnapshot, task *api.TaskInfo, order int, requests []resolvedDeviceTopologyTaskRequest, candidateNodes []*api.NodeInfo, at time.Time) (deviceTopologyPlanningTask, error) {
	result := deviceTopologyPlanningTask{
		task:  task,
		order: order,
	}
	requirementFits := make([]bool, len(requests))
	failureReasons := make([]map[string]struct{}, len(requests))
	for index := range failureReasons {
		failureReasons[index] = make(map[string]struct{})
	}

	for _, node := range candidateNodes {
		choice := deviceTopologyTaskNodeChoice{
			node:           node,
			requestChoices: make([]deviceTopologyRequestDomainChoice, 0, len(requests)),
		}
		fitsAllRequests := true
		for requestIndex, request := range requests {
			evaluation := evaluateLocalDomain(snapshot, node, request, at)
			if evaluation.reason != "" {
				failureReasons[requestIndex][evaluation.reason] = struct{}{}
				fitsAllRequests = false
				continue
			}
			requirementFits[requestIndex] = true
			choice.requestChoices = append(choice.requestChoices, deviceTopologyRequestDomainChoice{
				request: request,
				domains: append([]api.DeviceDomain(nil), evaluation.fittingDomains...),
			})
		}
		if !fitsAllRequests {
			continue
		}
		result.candidateChoices = append(result.candidateChoices, choice)
		for _, requestChoice := range choice.requestChoices {
			result.candidateDomainCount += len(requestChoice.domains)
			if requestChoice.request.quantity > result.largestRequestQuantity {
				result.largestRequestQuantity = requestChoice.request.quantity
			}
		}
	}

	if len(result.candidateChoices) == 0 {
		for requestIndex, fits := range requirementFits {
			if !fits {
				return deviceTopologyPlanningTask{}, newPolicyResolutionError(selectPlanningFailureReason(failureReasons[requestIndex]), "Task %q has no candidate Node with a fitting local domain for %q", task.UID, requests[requestIndex].resourceName)
			}
		}
		return deviceTopologyPlanningTask{}, newPolicyResolutionError(deviceTopologyDomainFragmentedReason, "Task %q cannot satisfy all hard device topology requirements on one candidate Node", task.UID)
	}
	return result, nil
}

// selectPlanningFailureReason preserves the most actionable static reason
// when no candidate Node can fit a request.
func selectPlanningFailureReason(reasons map[string]struct{}) string {
	for _, reason := range []string{
		deviceTopologyDataNotReadyReason,
		deviceTopologyStaleReason,
		deviceTopologyDeviceUnhealthyReason,
		deviceTopologyReservationConflictReason,
		deviceTopologyDomainFragmentedReason,
	} {
		if _, exists := reasons[reason]; exists {
			return reason
		}
	}
	return deviceTopologyDomainFragmentedReason
}

// planDeviceTopologyGangWithoutFabric solves a complete hard local-domain
// assignment without applying a cross-Node fabric restriction.
func planDeviceTopologyGangWithoutFabric(snapshot *api.TopologySnapshot, policy *resolvedDeviceTopologyPolicy, tasks []deviceTopologyPlanningTask, maxSearchStates int) (*deviceTopologyGangPlan, error) {
	solver := newDeviceTopologyGangPlanSolver(snapshot, tasks, maxSearchStates)
	if !solver.solve() {
		if solver.budgetExceeded {
			return nil, newPolicyResolutionError(deviceTopologyPlanningBudgetExceededReason, "device topology gang plan exceeded %d search states", maxSearchStates)
		}
		return nil, newPolicyResolutionError(deviceTopologyDomainFragmentedReason, "no complete local-domain plan exists for device topology policy unit %q", policy.schedulingUnitID)
	}
	return newDeviceTopologyGangPlan(policy, snapshot, api.FabricDomainID{}, solver.placements, tasks), nil
}

// planDeviceTopologyGangWithFabric selects one fresh fabric before assigning
// any Task. Every returned local domain must be a declared member of that one
// fabric.
func planDeviceTopologyGangWithFabric(snapshot *api.TopologySnapshot, policy *resolvedDeviceTopologyPolicy, tasks []deviceTopologyPlanningTask, maxSearchStates int, at time.Time) (*deviceTopologyGangPlan, error) {
	fabricCandidates, err := freshPlanningFabricCandidates(snapshot, tasks, at)
	if err != nil {
		return nil, err
	}
	remainingSearchStates := maxSearchStates
	for _, candidate := range fabricCandidates {
		solver := newDeviceTopologyGangPlanSolver(snapshot, candidate.tasks, remainingSearchStates)
		if solver.solve() {
			return newDeviceTopologyGangPlan(policy, snapshot, candidate.fabric.ID, solver.placements, candidate.tasks), nil
		}
		if solver.budgetExceeded {
			return nil, newPolicyResolutionError(deviceTopologyPlanningBudgetExceededReason, "device topology gang plan exceeded %d search states", maxSearchStates)
		}
		remainingSearchStates -= solver.searchStates
	}
	return nil, newPolicyResolutionError(deviceTopologyFabricUnavailableReason, "no declared fabric can satisfy device topology policy unit %q", policy.schedulingUnitID)
}

// freshPlanningFabricCandidates returns only fabrics that can serve every
// planning Task and whose owner and members have fresh provider facts. It
// ignores stale or pending fabrics that cannot serve the gang, so they cannot
// obscure the useful failure reason for this policy unit.
func freshPlanningFabricCandidates(snapshot *api.TopologySnapshot, tasks []deviceTopologyPlanningTask, at time.Time) ([]deviceTopologyFabricPlanningCandidate, error) {
	if snapshot == nil {
		return nil, newPolicyResolutionError(deviceTopologyDataNotReadyReason, "device topology snapshot is unavailable for fabric planning")
	}
	fabrics := snapshot.Fabrics()
	result := make([]deviceTopologyFabricPlanningCandidate, 0, len(fabrics))
	hasPendingFacts := false
	hasStaleFacts := false
	for _, fabric := range fabrics {
		fabricTasks := planningTasksInFabric(tasks, fabric)
		if len(fabricTasks) != len(tasks) {
			continue
		}
		switch fabricFreshnessReason(snapshot, fabric, at) {
		case "":
			result = append(result, deviceTopologyFabricPlanningCandidate{fabric: fabric, tasks: fabricTasks})
		case deviceTopologyDataNotReadyReason:
			hasPendingFacts = true
		case deviceTopologyStaleReason:
			hasStaleFacts = true
		}
	}
	if len(result) > 0 {
		return result, nil
	}
	if hasPendingFacts {
		return nil, newPolicyResolutionError(deviceTopologyDataNotReadyReason, "no fabric compatible with device topology policy unit has synced provider facts")
	}
	if hasStaleFacts {
		return nil, newPolicyResolutionError(deviceTopologyStaleReason, "no fabric compatible with device topology policy unit has fresh provider facts")
	}
	return nil, newPolicyResolutionError(deviceTopologyFabricUnavailableReason, "no declared fabric can serve device topology policy unit")
}

// planningTasksInFabric restricts every Task's Node/domain choices to one
// fabric without modifying the original candidate choices.
func planningTasksInFabric(tasks []deviceTopologyPlanningTask, fabric api.FabricDomain) []deviceTopologyPlanningTask {
	result := make([]deviceTopologyPlanningTask, 0, len(tasks))
	for _, task := range tasks {
		filteredTask := task
		filteredTask.candidateChoices = make([]deviceTopologyTaskNodeChoice, 0, len(task.candidateChoices))
		filteredTask.candidateDomainCount = 0
		for _, choice := range task.candidateChoices {
			filteredChoice := deviceTopologyTaskNodeChoice{
				node:           choice.node,
				requestChoices: make([]deviceTopologyRequestDomainChoice, 0, len(choice.requestChoices)),
			}
			fitsFabric := true
			candidateDomainCount := 0
			for _, requestChoice := range choice.requestChoices {
				filteredDomains := make([]api.DeviceDomain, 0, len(requestChoice.domains))
				for _, domain := range requestChoice.domains {
					if fabricContainsDomain(fabric, domain.ID) {
						filteredDomains = append(filteredDomains, domain)
					}
				}
				if len(filteredDomains) == 0 {
					fitsFabric = false
					break
				}
				filteredChoice.requestChoices = append(filteredChoice.requestChoices, deviceTopologyRequestDomainChoice{
					request: requestChoice.request,
					domains: filteredDomains,
				})
				candidateDomainCount += len(filteredDomains)
			}
			if fitsFabric {
				filteredTask.candidateChoices = append(filteredTask.candidateChoices, filteredChoice)
				filteredTask.candidateDomainCount += candidateDomainCount
			}
		}
		if len(filteredTask.candidateChoices) == 0 {
			return nil
		}
		result = append(result, filteredTask)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].candidateDomainCount != result[right].candidateDomainCount {
			return result[left].candidateDomainCount < result[right].candidateDomainCount
		}
		if result[left].largestRequestQuantity != result[right].largestRequestQuantity {
			return result[left].largestRequestQuantity > result[right].largestRequestQuantity
		}
		return result[left].order < result[right].order
	})
	return result
}

// fabricContainsDomain reports whether a local domain is an explicit member
// of a fabric. Ordinary network-connected Nodes never satisfy this check.
func fabricContainsDomain(fabric api.FabricDomain, domainID api.DeviceDomainID) bool {
	for _, member := range fabric.Members {
		for _, memberDomainID := range member.LocalDomainIDs {
			if memberDomainID == domainID {
				return true
			}
		}
	}
	return false
}

// newDeviceTopologyGangPlanSolver initializes static per-domain capacity from
// ledger-available snapshot devices. This prevents one gang plan from using
// capacity already held, bound, allocated, releasing, or unreconciled.
func newDeviceTopologyGangPlanSolver(snapshot *api.TopologySnapshot, tasks []deviceTopologyPlanningTask, maxSearchStates int) *deviceTopologyGangPlanSolver {
	remainingCapacity := make(map[api.DeviceDomainID]int64)
	for _, task := range tasks {
		for _, choice := range task.candidateChoices {
			for _, requestChoice := range choice.requestChoices {
				for _, domain := range requestChoice.domains {
					if _, exists := remainingCapacity[domain.ID]; !exists {
						remainingCapacity[domain.ID] = int64(len(snapshot.AvailableDeviceIDs(domain.ID)))
					}
				}
			}
		}
	}
	return &deviceTopologyGangPlanSolver{
		tasks:             tasks,
		remainingCapacity: remainingCapacity,
		maxSearchStates:   maxSearchStates,
	}
}

// solve finds one complete set of Node-local domain assignments. It explores
// deterministic Node and domain order, reverting each tentative capacity use
// before trying the next alternative.
func (solver *deviceTopologyGangPlanSolver) solve() bool {
	return solver.solveTask(0)
}

func (solver *deviceTopologyGangPlanSolver) solveTask(taskIndex int) bool {
	if taskIndex == len(solver.tasks) {
		return true
	}
	task := solver.tasks[taskIndex]
	for _, choice := range task.candidateChoices {
		if !solver.consumeSearchState() {
			return false
		}
		if solver.solveTaskRequests(taskIndex, task, choice, 0) {
			return true
		}
		if solver.budgetExceeded {
			return false
		}
	}
	return false
}

func (solver *deviceTopologyGangPlanSolver) solveTaskRequests(taskIndex int, task deviceTopologyPlanningTask, choice deviceTopologyTaskNodeChoice, requestIndex int) bool {
	if requestIndex == len(choice.requestChoices) {
		return solver.solveTask(taskIndex + 1)
	}
	requestChoice := choice.requestChoices[requestIndex]
	for _, domain := range requestChoice.domains {
		if !solver.consumeSearchState() {
			return false
		}
		if solver.remainingCapacity[domain.ID] < requestChoice.request.quantity {
			continue
		}
		solver.remainingCapacity[domain.ID] -= requestChoice.request.quantity
		solver.placements = append(solver.placements, deviceTopologyGangPlacement{
			taskID:        task.task.UID,
			nodeName:      planningNodeName(choice.node),
			nodeUID:       choice.node.Node.UID,
			containerName: requestChoice.request.containerName,
			resourceName:  requestChoice.request.resourceName,
			quantity:      requestChoice.request.quantity,
			localDomainID: domain.ID,
		})
		if solver.solveTaskRequests(taskIndex, task, choice, requestIndex+1) {
			return true
		}
		solver.placements = solver.placements[:len(solver.placements)-1]
		solver.remainingCapacity[domain.ID] += requestChoice.request.quantity
		if solver.budgetExceeded {
			return false
		}
	}
	return false
}

// consumeSearchState bounds backtracking work so an unusually broad topology
// cannot monopolize a scheduling cycle. The limit is
// defaultDeviceTopologyGangPlanMaxSearchStates unless the planning context
// sets one, which only tests do.
func (solver *deviceTopologyGangPlanSolver) consumeSearchState() bool {
	if solver.searchStates >= solver.maxSearchStates {
		solver.budgetExceeded = true
		return false
	}
	solver.searchStates++
	return true
}

// newDeviceTopologyGangPlan copies the complete result into deterministic
// admission-unit order before returning it to a future allocation integration.
func newDeviceTopologyGangPlan(policy *resolvedDeviceTopologyPolicy, snapshot *api.TopologySnapshot, fabricID api.FabricDomainID, placements []deviceTopologyGangPlacement, tasks []deviceTopologyPlanningTask) *deviceTopologyGangPlan {
	taskOrder := make(map[api.TaskID]int, len(tasks))
	for _, task := range tasks {
		taskOrder[task.task.UID] = task.order
	}
	result := &deviceTopologyGangPlan{
		jobID:            policy.jobID,
		schedulingUnitID: policy.schedulingUnitID,
		revision:         snapshot.Revision(),
		fabricID:         fabricID,
		placements:       append([]deviceTopologyGangPlacement(nil), placements...),
	}
	sort.Slice(result.placements, func(left, right int) bool {
		leftOrder := taskOrder[result.placements[left].taskID]
		rightOrder := taskOrder[result.placements[right].taskID]
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if result.placements[left].resourceName != result.placements[right].resourceName {
			return result.placements[left].resourceName < result.placements[right].resourceName
		}
		if result.placements[left].containerName != result.placements[right].containerName {
			return result.placements[left].containerName < result.placements[right].containerName
		}
		return compareDeviceDomainIDs(result.placements[left].localDomainID, result.placements[right].localDomainID) < 0
	})
	return result
}

// planningNodeName returns the scheduler Node name, falling back to the
// Kubernetes Node name for test and adapter-created NodeInfo values.
func planningNodeName(node *api.NodeInfo) string {
	if node == nil {
		return ""
	}
	if node.Name != "" {
		return node.Name
	}
	if node.Node != nil {
		return node.Node.Name
	}
	return ""
}

// compareDeviceDomainIDs provides a stable total order for plan output.
func compareDeviceDomainIDs(left, right api.DeviceDomainID) int {
	if left.ProviderID != right.ProviderID {
		return stringCompare(left.ProviderID, right.ProviderID)
	}
	if left.Namespace != right.Namespace {
		return stringCompare(left.Namespace, right.Namespace)
	}
	if left.NodeUID != right.NodeUID {
		return stringCompare(string(left.NodeUID), string(right.NodeUID))
	}
	return stringCompare(left.Value, right.Value)
}

// stringCompare returns a conventional three-way lexical comparison.
func stringCompare(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
