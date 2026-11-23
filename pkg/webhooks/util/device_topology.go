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

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	corev1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
)

type deviceTopologyRequirement struct {
	extendedResourceName corev1.ResourceName
	mode                 string
}

type deviceTopologyPolicy struct {
	requirements []deviceTopologyRequirement
	fabricMode   string
	hasFabric    bool
}

// ValidateBatchDeviceTopology validates one DeviceTopologySpec from the batch
// API. It returns messages without an object path so callers can add the path
// appropriate for their workload API.
func ValidateBatchDeviceTopology(policy *batchv1alpha1.DeviceTopologySpec) []string {
	if policy == nil {
		return nil
	}

	requirements := make([]deviceTopologyRequirement, 0, len(policy.Requirements))
	for _, requirement := range policy.Requirements {
		requirements = append(requirements, deviceTopologyRequirement{
			extendedResourceName: requirement.Resource.ExtendedResourceName,
			mode:                 string(requirement.Mode),
		})
	}

	result := deviceTopologyPolicy{requirements: requirements}
	if policy.Fabric != nil {
		result.hasFabric = true
		result.fabricMode = string(policy.Fabric.Mode)
	}
	return validateDeviceTopology(result)
}

// ValidateSchedulingDeviceTopology validates one DeviceTopologySpec from the
// scheduling API. It returns messages without an object path so callers can
// add the path appropriate for their workload API.
func ValidateSchedulingDeviceTopology(policy *schedulingv1beta1.DeviceTopologySpec) []string {
	if policy == nil {
		return nil
	}

	requirements := make([]deviceTopologyRequirement, 0, len(policy.Requirements))
	for _, requirement := range policy.Requirements {
		requirements = append(requirements, deviceTopologyRequirement{
			extendedResourceName: requirement.Resource.ExtendedResourceName,
			mode:                 string(requirement.Mode),
		})
	}

	result := deviceTopologyPolicy{requirements: requirements}
	if policy.Fabric != nil {
		result.hasFabric = true
		result.fabricMode = string(policy.Fabric.Mode)
	}
	return validateDeviceTopology(result)
}

// ValidateBatchJobDeviceTopology validates DeviceTopology policies scoped to
// a Job. A parent policy may select any task that requests its resource, while
// a partition policy must select its own task template.
func ValidateBatchJobDeviceTopology(spec *batchv1alpha1.JobSpec) []string {
	if spec == nil {
		return nil
	}

	var errs []string
	hasSubGroupPolicy := false
	for _, task := range spec.Tasks {
		if task.PartitionPolicy != nil && task.PartitionPolicy.DeviceTopology != nil {
			hasSubGroupPolicy = true
			break
		}
	}
	if spec.DeviceTopology != nil && hasSubGroupPolicy {
		errs = append(errs, "deviceTopology cannot be set together with tasks[].partitionPolicy.deviceTopology")
	}

	errs = append(errs, prefixDeviceTopologyErrors("deviceTopology", ValidateBatchDeviceTopology(spec.DeviceTopology))...)
	if spec.DeviceTopology != nil {
		for index, requirement := range spec.DeviceTopology.Requirements {
			resourceName := requirement.Resource.ExtendedResourceName
			if resourceName != "" && !jobRequestsResource(spec, resourceName) {
				errs = append(errs, fmt.Sprintf("deviceTopology.requirements[%d].resource.extendedResourceName %q is not requested by any task", index, resourceName))
			}
		}
	}

	for taskIndex, task := range spec.Tasks {
		if task.PartitionPolicy == nil || task.PartitionPolicy.DeviceTopology == nil {
			continue
		}

		policyPath := fmt.Sprintf("tasks[%d].partitionPolicy.deviceTopology", taskIndex)
		policy := task.PartitionPolicy.DeviceTopology
		errs = append(errs, prefixDeviceTopologyErrors(policyPath, ValidateBatchDeviceTopology(policy))...)
		for requirementIndex, requirement := range policy.Requirements {
			resourceName := requirement.Resource.ExtendedResourceName
			if resourceName != "" && !taskRequestsResource(task, resourceName) {
				errs = append(errs, fmt.Sprintf("%s.requirements[%d].resource.extendedResourceName %q is not requested by task %q", policyPath, requirementIndex, resourceName, task.Name))
			}
		}
	}

	return errs
}

// ValidateSchedulingPodGroupDeviceTopology validates DeviceTopology policies
// scoped to a direct PodGroup and its matching subgroup policies.
func ValidateSchedulingPodGroupDeviceTopology(spec *schedulingv1beta1.PodGroupSpec) []string {
	if spec == nil {
		return nil
	}

	var errs []string
	hasSubGroupPolicy := false
	for _, policy := range spec.SubGroupPolicy {
		if policy.DeviceTopology != nil {
			hasSubGroupPolicy = true
			break
		}
	}
	if spec.DeviceTopology != nil && hasSubGroupPolicy {
		errs = append(errs, "deviceTopology cannot be set together with subGroupPolicy[].deviceTopology")
	}

	errs = append(errs, prefixDeviceTopologyErrors("deviceTopology", ValidateSchedulingDeviceTopology(spec.DeviceTopology))...)
	for index, policy := range spec.SubGroupPolicy {
		if policy.DeviceTopology == nil {
			continue
		}

		policyPath := fmt.Sprintf("subGroupPolicy[%d]", index)
		if policy.SubGroupSize == nil || *policy.SubGroupSize <= 0 {
			errs = append(errs, fmt.Sprintf("%s.subGroupSize must be greater than 0 when deviceTopology is set", policyPath))
		}
		if isEmptyLabelSelector(policy.LabelSelector) {
			errs = append(errs, fmt.Sprintf("%s.labelSelector must be non-empty when deviceTopology is set", policyPath))
		} else if _, err := metav1.LabelSelectorAsSelector(policy.LabelSelector); err != nil {
			errs = append(errs, fmt.Sprintf("%s.labelSelector is invalid: %v", policyPath, err))
		}
		errs = append(errs, prefixDeviceTopologyErrors(policyPath+".deviceTopology", ValidateSchedulingDeviceTopology(policy.DeviceTopology))...)
	}

	return errs
}

// SchedulingPodGroupDeviceTopologyPoliciesEqual reports whether a PodGroup
// update preserves every device-topology policy and the subgroup scope that
// determines which Pods it governs.
func SchedulingPodGroupDeviceTopologyPoliciesEqual(oldSpec, newSpec *schedulingv1beta1.PodGroupSpec) bool {
	if oldSpec == nil || newSpec == nil {
		return oldSpec == newSpec
	}
	if !apiequality.Semantic.DeepEqual(oldSpec.DeviceTopology, newSpec.DeviceTopology) {
		return false
	}
	return apiequality.Semantic.DeepEqual(
		subGroupDeviceTopologyPolicies(oldSpec.SubGroupPolicy),
		subGroupDeviceTopologyPolicies(newSpec.SubGroupPolicy),
	)
}

func validateDeviceTopology(policy deviceTopologyPolicy) []string {
	if !utilfeature.DefaultFeatureGate.Enabled(features.DeviceTopologyAwareScheduling) {
		return []string{"DeviceTopologyAwareScheduling feature is disabled"}
	}

	var errs []string
	if len(policy.requirements) == 0 {
		errs = append(errs, "requirements must contain at least one resource")
	}

	resourceNames := make(map[corev1.ResourceName]struct{}, len(policy.requirements))
	for index, requirement := range policy.requirements {
		resourcePath := fmt.Sprintf("requirements[%d].resource", index)
		if requirement.extendedResourceName == "" {
			errs = append(errs, resourcePath+".extendedResourceName is required")
		} else {
			if !corev1helper.IsExtendedResourceName(requirement.extendedResourceName) {
				errs = append(errs, fmt.Sprintf("%s.extendedResourceName %q must be a valid Kubernetes extended resource name", resourcePath, requirement.extendedResourceName))
			}
			if _, exists := resourceNames[requirement.extendedResourceName]; exists {
				errs = append(errs, fmt.Sprintf("requirements[%d] duplicates resource %q", index, requirement.extendedResourceName))
			} else {
				resourceNames[requirement.extendedResourceName] = struct{}{}
			}
		}
		if !validDeviceTopologyMode(requirement.mode) {
			errs = append(errs, fmt.Sprintf("requirements[%d].mode must be hard or soft", index))
		}
	}

	if policy.hasFabric {
		if len(policy.requirements) != 1 {
			errs = append(errs, "fabric requires exactly one resource requirement in alpha")
		}
		if !validDeviceTopologyMode(policy.fabricMode) {
			errs = append(errs, "fabric.mode must be hard or soft")
		}
	}

	return errs
}

func validDeviceTopologyMode(mode string) bool {
	return mode == string(batchv1alpha1.HardDeviceTopologyMode) || mode == string(batchv1alpha1.SoftDeviceTopologyMode)
}

func prefixDeviceTopologyErrors(path string, errs []string) []string {
	if len(errs) == 0 {
		return nil
	}
	prefixed := make([]string, 0, len(errs))
	for _, err := range errs {
		prefixed = append(prefixed, path+": "+err)
	}
	return prefixed
}

func jobRequestsResource(spec *batchv1alpha1.JobSpec, resourceName corev1.ResourceName) bool {
	for _, task := range spec.Tasks {
		if taskRequestsResource(task, resourceName) {
			return true
		}
	}
	return false
}

func taskRequestsResource(task batchv1alpha1.TaskSpec, resourceName corev1.ResourceName) bool {
	for _, container := range task.Template.Spec.Containers {
		quantity, exists := container.Resources.Requests[resourceName]
		if exists && !quantity.IsZero() {
			return true
		}
	}
	return false
}

func isEmptyLabelSelector(selector *metav1.LabelSelector) bool {
	return selector == nil || (len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0)
}

type subGroupDeviceTopologyPolicy struct {
	Name           string
	SubGroupSize   *int32
	LabelSelector  *metav1.LabelSelector
	MatchLabelKeys []string
	DeviceTopology *schedulingv1beta1.DeviceTopologySpec
}

func subGroupDeviceTopologyPolicies(policies []schedulingv1beta1.SubGroupPolicySpec) []subGroupDeviceTopologyPolicy {
	result := make([]subGroupDeviceTopologyPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy.DeviceTopology == nil {
			continue
		}
		result = append(result, subGroupDeviceTopologyPolicy{
			Name:           policy.Name,
			SubGroupSize:   policy.SubGroupSize,
			LabelSelector:  policy.LabelSelector,
			MatchLabelKeys: policy.MatchLabelKeys,
			DeviceTopology: policy.DeviceTopology,
		})
	}
	return result
}
