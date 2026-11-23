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
	"errors"
	"fmt"
	"sort"
	"time"

	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

type deviceTopologyBatchPolicyUnit struct {
	key      policyUnitKey
	job      *api.JobInfo
	subJob   *api.SubJobInfo
	policy   *resolvedDeviceTopologyPolicy
	contexts map[api.TaskID]*cache.BindContext
}

type deviceTopologyPreparedBindBatch struct {
	plugin       *deviceTopologyPlugin
	session      *framework.Session
	reservations []api.TopologyReservation
	// taskIDs are the Tasks whose devices the reservations hold.
	taskIDs    []api.TaskID
	committed  bool
	rolledBack bool
}

// PrepareBindBatch reserves every hard device-topology policy unit represented
// by an allocation statement and attaches immutable adapter handoffs to the
// corresponding Pod bind contexts. Soft-only policies remain on the normal
// scoring path and return nil preparation.
func (plugin *deviceTopologyPlugin) PrepareBindBatch(ctx context.Context, bindContexts []*cache.BindContext) (cache.BatchBindPreparation, error) {
	if plugin == nil || plugin.session == nil || !utilfeature.DefaultFeatureGate.Enabled(features.DeviceTopologyAwareScheduling) {
		return nil, nil
	}

	units, err := plugin.batchPolicyUnits(bindContexts)
	if err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return nil, nil
	}

	prepared := &deviceTopologyPreparedBindBatch{plugin: plugin, session: plugin.session}
	handoffsByContext := make(map[*cache.BindContext][]cache.DeviceTopologyBindHandoff)
	fail := func(preparationErr error) (cache.BatchBindPreparation, error) {
		rollbackErr := prepared.Rollback(ctx)
		if rollbackErr != nil {
			return nil, errors.Join(preparationErr, rollbackErr)
		}
		return nil, preparationErr
	}

	for _, unit := range units {
		tasks, assignedNodes, err := completeHardBatchPolicyUnit(plugin.session, unit)
		if err != nil {
			return fail(err)
		}

		gangPlan, err := plugin.planGang(plugin.session, deviceTopologyGangPlanContext{
			job:           unit.job,
			subJob:        unit.subJob,
			tasks:         tasks,
			assignedNodes: assignedNodes,
			at:            time.Now(),
		})
		if err != nil {
			return fail(err)
		}
		if gangPlan == nil {
			return fail(newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "hard device topology policy unit %q produced no gang plan", unit.policy.schedulingUnitID))
		}

		reservation, err := plugin.reserveGangPlan(ctx, plugin.session, gangPlan)
		if err != nil {
			return fail(err)
		}
		prepared.reservations = append(prepared.reservations, reservation)
		for _, task := range tasks {
			prepared.taskIDs = append(prepared.taskIDs, task.UID)
		}

		handoffs, err := plugin.session.PrepareDeviceTopologyReservationBind(ctx, reservation)
		if err != nil {
			return fail(fmt.Errorf("prepare device topology bind handoffs for policy unit %q: %w", unit.policy.schedulingUnitID, err))
		}
		for _, task := range tasks {
			bindContext := unit.contexts[task.UID]
			taskHandoffs := handoffs[task.UID]
			if len(taskHandoffs) == 0 {
				return fail(newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation produced no bind handoff for Task %q", task.UID))
			}
			handoffsByContext[bindContext] = append(handoffsByContext[bindContext], taskHandoffs...)
		}
	}

	// Handoffs are attached only after every policy unit succeeded. After any
	// later failure the statement discards these bind contexts, so the
	// extension does not need to be removed on rollback.
	for _, bindContext := range bindContexts {
		handoffs := handoffsByContext[bindContext]
		if len(handoffs) == 0 {
			continue
		}
		if bindContext.Extensions == nil {
			bindContext.Extensions = make(map[string]cache.BindContextExtension)
		}
		bindContext.Extensions[PluginName] = cache.DeviceTopologyBindContextExtension{Handoffs: handoffs}
	}

	return prepared, nil
}

// Commit advances every adapter-backed device reservation to Binding after all
// required Pod handoffs have been prepared. A failure rolls the entire batch
// back, including reservations already committed by earlier policy units.
func (prepared *deviceTopologyPreparedBindBatch) Commit(ctx context.Context) error {
	if prepared == nil {
		return nil
	}
	if prepared.rolledBack {
		return fmt.Errorf("device topology bind preparation has already been rolled back")
	}
	if prepared.committed {
		return nil
	}
	for _, reservation := range prepared.reservations {
		if err := prepared.session.CommitDeviceTopologyReservationBind(ctx, reservation); err != nil {
			rollbackErr := prepared.Rollback(ctx)
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("commit device topology reservation bind: %w", err), rollbackErr)
			}
			return fmt.Errorf("commit device topology reservation bind: %w", err)
		}
	}
	prepared.committed = true
	// The topology snapshot now shows these devices as held, so the Tasks no
	// longer count as pending allocations.
	for _, taskID := range prepared.taskIDs {
		prepared.plugin.forgetPendingAllocation(taskID)
	}
	return nil
}

// Rollback releases every reservation in reverse order. It is safe to call
// more than once.
func (prepared *deviceTopologyPreparedBindBatch) Rollback(ctx context.Context) error {
	if prepared == nil || prepared.rolledBack {
		return nil
	}
	prepared.rolledBack = true

	var rollbackErrors []error
	for index := len(prepared.reservations) - 1; index >= 0; index-- {
		if err := prepared.session.RollbackDeviceTopologyReservationBind(ctx, prepared.reservations[index]); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback device topology reservation bind: %w", err))
		}
	}
	return errors.Join(rollbackErrors...)
}

// batchPolicyUnits selects only policy units with at least one hard request in
// this statement. It still fails closed for malformed policies encountered by
// a context that belongs to a configured device-topology unit.
func (plugin *deviceTopologyPlugin) batchPolicyUnits(bindContexts []*cache.BindContext) ([]*deviceTopologyBatchPolicyUnit, error) {
	unitsByKey := make(map[policyUnitKey]*deviceTopologyBatchPolicyUnit)
	for _, bindContext := range bindContexts {
		if bindContext == nil || bindContext.TaskInfo == nil {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "batch bind contains an empty Task context")
		}
		task := bindContext.TaskInfo
		job, exists := plugin.session.Jobs[task.Job]
		if !exists || job == nil {
			continue
		}
		subJob, err := topologySubJobForTask(job, task)
		if err != nil {
			return nil, err
		}
		policy, err := plugin.policyFor(job, subJob)
		if err != nil {
			return nil, err
		}
		if policy == nil || len(planningRequestsForTask(policy, task.UID)) == 0 {
			continue
		}
		key, hasPolicy := policyUnitKeyFor(job, subJob)
		if !hasPolicy {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Task %q has a resolved policy without a policy unit", task.UID)
		}
		unit, exists := unitsByKey[key]
		if !exists {
			unit = &deviceTopologyBatchPolicyUnit{
				key:      key,
				job:      job,
				subJob:   subJob,
				policy:   policy,
				contexts: make(map[api.TaskID]*cache.BindContext),
			}
			unitsByKey[key] = unit
		}
		if _, exists := unit.contexts[task.UID]; exists {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "batch bind repeats hard device topology Task %q", task.UID)
		}
		unit.contexts[task.UID] = bindContext
	}

	result := make([]*deviceTopologyBatchPolicyUnit, 0, len(unitsByKey))
	for _, unit := range unitsByKey {
		result = append(result, unit)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].key.jobID != result[right].key.jobID {
			return result[left].key.jobID < result[right].key.jobID
		}
		return result[left].key.subJobID < result[right].key.subJobID
	})
	return result, nil
}

// completeHardBatchPolicyUnit resolves the exact allocated Node for every hard
// Task of a policy unit that is in this bind batch. The batch is the gang plan
// unit: Tasks admitted by a later allocation wave receive their own plan. A
// hard fabric is the exception, because a later wave must reuse the fabric of
// earlier members and that choice is not tracked yet, so such a wave fails
// closed.
func completeHardBatchPolicyUnit(session *framework.Session, unit *deviceTopologyBatchPolicyUnit) ([]*api.TaskInfo, map[api.TaskID]*api.NodeInfo, error) {
	if session == nil || unit == nil || unit.job == nil || unit.policy == nil {
		return nil, nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "batch bind has an incomplete device topology policy unit")
	}
	tasks := make([]*api.TaskInfo, 0, len(unit.contexts))
	assignedNodes := make(map[api.TaskID]*api.NodeInfo)
	hardFabric := unit.policy.hasFabric && unit.policy.fabricMode == scheduling.HardDeviceTopologyMode
	for _, taskID := range unit.policy.taskIDs {
		if len(planningRequestsForTask(unit.policy, taskID)) == 0 {
			continue
		}
		bindContext, exists := unit.contexts[taskID]
		if !exists {
			if member, found := unit.job.Tasks[taskID]; hardFabric && found && member != nil && api.AllocatedStatus(member.Status) {
				return nil, nil, newPolicyResolutionError(deviceTopologyFabricUnavailableReason, "hard device topology fabric for policy unit %q is already used by Task %q; later allocation waves are not supported yet", unit.policy.schedulingUnitID, taskID)
			}
			continue
		}
		task, exists := unit.job.Tasks[taskID]
		if !exists || task == nil || bindContext.TaskInfo == nil {
			return nil, nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "hard device topology policy unit %q references missing Task %q", unit.policy.schedulingUnitID, taskID)
		}
		if bindContext.TaskInfo.NodeName == "" || task.NodeName != bindContext.TaskInfo.NodeName {
			return nil, nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Task %q has no consistent allocated Node for device topology bind", taskID)
		}
		node, exists := session.Nodes[bindContext.TaskInfo.NodeName]
		if !exists || node == nil {
			return nil, nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Task %q references missing allocated Node %q", taskID, bindContext.TaskInfo.NodeName)
		}
		tasks = append(tasks, task)
		assignedNodes[taskID] = node
	}
	if len(tasks) == 0 {
		return nil, nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology policy unit %q has no hard Task requests", unit.policy.schedulingUnitID)
	}
	return tasks, assignedNodes, nil
}
