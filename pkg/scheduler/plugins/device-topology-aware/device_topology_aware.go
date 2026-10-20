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
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName is the scheduler configuration name for device topology-aware scheduling.
	PluginName = "device-topology-aware"
)

// deviceTopologyPlugin resolves immutable PodGroup device topology policies at
// session open. Its predicate and Node-order callbacks consume those policy
// units without reparsing workload API objects.
type deviceTopologyPlugin struct {
	policies map[policyUnitKey]*resolvedDeviceTopologyPolicy
	errors   map[policyUnitKey]error
}

// New creates a device topology-aware scheduler plugin.
func New(_ framework.Arguments) framework.Plugin {
	return &deviceTopologyPlugin{
		policies: make(map[policyUnitKey]*resolvedDeviceTopologyPolicy),
		errors:   make(map[policyUnitKey]error),
	}
}

// Name returns the scheduler configuration name for this plugin.
func (plugin *deviceTopologyPlugin) Name() string {
	return PluginName
}

// OnSessionOpen resolves the canonical parent and subgroup policies present in
// the immutable scheduler session and registers static topology filter and
// scoring callbacks for the session lifetime.
func (plugin *deviceTopologyPlugin) OnSessionOpen(session *framework.Session) {
	clear(plugin.policies)
	clear(plugin.errors)
	if session == nil || !utilfeature.DefaultFeatureGate.Enabled(features.DeviceTopologyAwareScheduling) {
		return
	}

	for _, job := range session.Jobs {
		plugin.resolveJob(job)
	}

	session.AddPredicateFn(plugin.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		return plugin.predicate(session, task, node)
	})
	session.AddNodeOrderFn(plugin.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		return plugin.nodeOrder(session, task, node)
	})
}

// OnSessionClose discards policy data that belongs only to the completed
// scheduling session.
func (plugin *deviceTopologyPlugin) OnSessionClose(_ *framework.Session) {
	clear(plugin.policies)
	clear(plugin.errors)
}

// resolveJob records a resolved policy or a fail-closed error for every policy
// unit owned by one Job.
func (plugin *deviceTopologyPlugin) resolveJob(job *api.JobInfo) {
	if job == nil {
		return
	}

	hasSubGroupPolicy := false
	for _, subJob := range job.SubJobs {
		if subJob != nil && subJob.DeviceTopology != nil {
			hasSubGroupPolicy = true
			break
		}
	}
	if job.DeviceTopology != nil && hasSubGroupPolicy {
		err := newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Job %q configures both parent and subgroup device topology policies", job.UID)
		plugin.errors[parentPolicyUnitKey(job)] = err
		for _, subJob := range job.SubJobs {
			if subJob != nil && subJob.DeviceTopology != nil {
				plugin.errors[subGroupPolicyUnitKey(job, subJob)] = err
			}
		}
		return
	}

	if job.DeviceTopology != nil {
		policy, err := resolveDeviceTopologyPolicy(parentPolicyScope, job, nil, job.DeviceTopology, job.Tasks)
		plugin.storePolicy(parentPolicyUnitKey(job), policy, err)
	}
	for _, subJob := range job.SubJobs {
		if subJob == nil || subJob.DeviceTopology == nil {
			continue
		}
		if subJob.Job != job.UID {
			plugin.errors[subGroupPolicyUnitKey(job, subJob)] = newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "SubJob %q does not belong to Job %q", subJob.UID, job.UID)
			continue
		}
		policy, err := resolveDeviceTopologyPolicy(subGroupPolicyScope, job, subJob, subJob.DeviceTopology, subJob.Tasks)
		plugin.storePolicy(subGroupPolicyUnitKey(job, subJob), policy, err)
	}
}

// storePolicy records either a resolved policy or the fail-closed error that
// explains why the scheduler cannot use that policy unit.
func (plugin *deviceTopologyPlugin) storePolicy(key policyUnitKey, policy *resolvedDeviceTopologyPolicy, err error) {
	if err != nil {
		plugin.errors[key] = err
		return
	}
	plugin.policies[key] = policy
}

// policyFor returns the active resolved policy for a parent Job or one SubJob.
// A nil policy and nil error mean that the requested unit has no device
// topology policy.
func (plugin *deviceTopologyPlugin) policyFor(job *api.JobInfo, subJob *api.SubJobInfo) (*resolvedDeviceTopologyPolicy, error) {
	if !utilfeature.DefaultFeatureGate.Enabled(features.DeviceTopologyAwareScheduling) {
		return nil, nil
	}
	key, hasPolicy := policyUnitKeyFor(job, subJob)
	if !hasPolicy {
		return nil, nil
	}
	if err, exists := plugin.errors[key]; exists {
		return nil, err
	}
	policy, exists := plugin.policies[key]
	if !exists {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology policy for Job %q was not resolved", key.jobID)
	}
	return policy, nil
}
