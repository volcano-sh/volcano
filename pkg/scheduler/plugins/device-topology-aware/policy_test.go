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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestResolveDeviceTopologyPolicyRejectsMalformedPolicies(t *testing.T) {
	gpuTask := newDeviceTopologyTask("gpu-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	gpuJob := api.NewJobInfo("gpu-job", gpuTask)
	emptyJob := api.NewJobInfo("empty-job")
	cpuTask := newDeviceTopologyTask("cpu-worker", []corev1.Container{containerWithRequest("worker", corev1.ResourceCPU, "1")}, nil)
	cpuJob := api.NewJobInfo("cpu-job", cpuTask)
	validRequirement := scheduling.DeviceTopologyRequirement{
		Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
		Mode:     scheduling.HardDeviceTopologyMode,
	}

	tests := []struct {
		name        string
		scope       policyScope
		job         *api.JobInfo
		subJob      *api.SubJobInfo
		policy      *scheduling.DeviceTopologySpec
		tasks       map[api.TaskID]*api.TaskInfo
		wantReason  string
		wantMessage string
	}{
		{
			name:        "missing owning Job",
			scope:       parentPolicyScope,
			policy:      testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode),
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "no owning Job",
		},
		{
			name:        "missing policy",
			scope:       parentPolicyScope,
			job:         gpuJob,
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "no device topology policy",
		},
		{
			name:        "no Tasks",
			scope:       parentPolicyScope,
			job:         emptyJob,
			policy:      testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode),
			tasks:       emptyJob.Tasks,
			wantReason:  unsupportedPodRequestPolicyReason,
			wantMessage: "has no Tasks",
		},
		{
			name:        "no requirements",
			scope:       parentPolicyScope,
			job:         gpuJob,
			policy:      &scheduling.DeviceTopologySpec{},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "has no requirements",
		},
		{
			name:  "subgroup scope without SubJob",
			scope: subGroupPolicyScope,
			job:   gpuJob,
			policy: testDeviceTopologyPolicy(
				"nvidia.com/gpu",
				scheduling.HardDeviceTopologyMode,
			),
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "has no SubJob",
		},
		{
			name:  "fabric affinity has multiple requirements",
			scope: parentPolicyScope,
			job:   gpuJob,
			policy: &scheduling.DeviceTopologySpec{
				Requirements: []scheduling.DeviceTopologyRequirement{
					validRequirement,
					{
						Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: "example.com/accelerator"},
						Mode:     scheduling.HardDeviceTopologyMode,
					},
				},
				Fabric: &scheduling.DeviceFabricAffinity{Mode: scheduling.HardDeviceTopologyMode},
			},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "requires exactly one resource requirement",
		},
		{
			name:  "fabric affinity has unsupported mode",
			scope: parentPolicyScope,
			job:   gpuJob,
			policy: &scheduling.DeviceTopologySpec{
				Requirements: []scheduling.DeviceTopologyRequirement{validRequirement},
				Fabric:       &scheduling.DeviceFabricAffinity{Mode: scheduling.DeviceTopologyMode("unsupported")},
			},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "fabric affinity",
		},
		{
			name:  "requirement has no selector",
			scope: parentPolicyScope,
			job:   gpuJob,
			policy: &scheduling.DeviceTopologySpec{Requirements: []scheduling.DeviceTopologyRequirement{{
				Mode: scheduling.HardDeviceTopologyMode,
			}}},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "must set extendedResourceName",
		},
		{
			name:  "resource is not an extended resource",
			scope: parentPolicyScope,
			job:   gpuJob,
			policy: &scheduling.DeviceTopologySpec{Requirements: []scheduling.DeviceTopologyRequirement{{
				Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: corev1.ResourceCPU},
				Mode:     scheduling.HardDeviceTopologyMode,
			}}},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "invalid extended resource",
		},
		{
			name:  "requirement has unsupported mode",
			scope: parentPolicyScope,
			job:   gpuJob,
			policy: &scheduling.DeviceTopologySpec{Requirements: []scheduling.DeviceTopologyRequirement{{
				Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
				Mode:     scheduling.DeviceTopologyMode("unsupported"),
			}}},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "unsupported mode",
		},
		{
			name:  "duplicate resource requirements",
			scope: parentPolicyScope,
			job:   gpuJob,
			policy: &scheduling.DeviceTopologySpec{Requirements: []scheduling.DeviceTopologyRequirement{
				validRequirement,
				validRequirement,
			}},
			tasks:       gpuJob.Tasks,
			wantReason:  invalidDeviceTopologyPolicyReason,
			wantMessage: "repeats device topology requirement",
		},
		{
			name:        "requirement selects no Tasks",
			scope:       parentPolicyScope,
			job:         cpuJob,
			policy:      testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode),
			tasks:       cpuJob.Tasks,
			wantReason:  unsupportedPodRequestPolicyReason,
			wantMessage: "selects no Tasks",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveDeviceTopologyPolicy(test.scope, test.job, test.subJob, test.policy, test.tasks)
			assertPolicyResolutionError(t, err, test.wantReason, test.wantMessage)
		})
	}
}

func TestResolveDeviceTopologyPolicyResolvesFabricAndSortsTasks(t *testing.T) {
	lastTask := newDeviceTopologyTask("z-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
	firstTask := newDeviceTopologyTask("a-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", lastTask, firstTask)
	policy := testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	policy.Fabric = &scheduling.DeviceFabricAffinity{Mode: scheduling.SoftDeviceTopologyMode}

	resolved, err := resolveDeviceTopologyPolicy(parentPolicyScope, job, nil, policy, job.Tasks)
	if err != nil {
		t.Fatalf("resolveDeviceTopologyPolicy() error = %v", err)
	}
	if !resolved.hasFabric || resolved.fabricMode != scheduling.SoftDeviceTopologyMode {
		t.Fatalf("fabric resolution = (%t, %q), want (true, %q)", resolved.hasFabric, resolved.fabricMode, scheduling.SoftDeviceTopologyMode)
	}
	if len(resolved.taskIDs) != 2 || resolved.taskIDs[0] != firstTask.UID || resolved.taskIDs[1] != lastTask.UID {
		t.Fatalf("resolved Task IDs = %v, want [%q %q]", resolved.taskIDs, firstTask.UID, lastTask.UID)
	}
	requests := resolved.requirements[0].tasks
	if len(requests) != 2 || requests[0].taskID != firstTask.UID || requests[1].taskID != lastTask.UID {
		t.Fatalf("resolved Task requests = %#v, want requests ordered by Task ID", requests)
	}
}

func TestResolveDeviceTopologyPolicyRejectsUnsupportedTaskRequests(t *testing.T) {
	tests := []struct {
		name        string
		containers  []corev1.Container
		init        []corev1.Container
		wantMessage string
	}{
		{
			name: "multiple regular containers request a device",
			containers: []corev1.Container{
				containerWithRequest("trainer-a", "nvidia.com/gpu", "1"),
				containerWithRequest("trainer-b", "nvidia.com/gpu", "1"),
			},
			wantMessage: "more than one regular container",
		},
		{
			name:        "init container requests a device",
			containers:  []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")},
			init:        []corev1.Container{containerWithRequest("setup", "nvidia.com/gpu", "1")},
			wantMessage: "init container",
		},
		{
			name:        "zero device request",
			containers:  []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "0")},
			wantMessage: "non-positive quantity",
		},
		{
			name:        "fractional device request",
			containers:  []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "500m")},
			wantMessage: "integral whole-device quantity",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := newDeviceTopologyTask("worker", test.containers, test.init)
			job := api.NewJobInfo("job", task)
			_, err := resolveDeviceTopologyPolicy(parentPolicyScope, job, nil, testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode), job.Tasks)
			assertPolicyResolutionError(t, err, unsupportedPodRequestPolicyReason, test.wantMessage)
		})
	}
}

func TestResolveTaskDeviceRequestSkipsTaskWithoutRequestedResource(t *testing.T) {
	task := newDeviceTopologyTask("cpu-worker", []corev1.Container{containerWithRequest("worker", corev1.ResourceCPU, "1")}, nil)
	request, selected, err := resolveTaskDeviceRequest(task, "nvidia.com/gpu")
	if err != nil {
		t.Fatalf("resolveTaskDeviceRequest() error = %v", err)
	}
	if selected || request != nil {
		t.Fatalf("resolveTaskDeviceRequest() = (%#v, %t), want (nil, false)", request, selected)
	}
}

func TestResolveTaskDeviceRequestRejectsMissingPod(t *testing.T) {
	_, _, err := resolveTaskDeviceRequest(nil, "nvidia.com/gpu")
	assertPolicyResolutionError(t, err, unsupportedPodRequestPolicyReason, "has no Pod")

	_, _, err = resolveTaskDeviceRequest(&api.TaskInfo{}, "nvidia.com/gpu")
	assertPolicyResolutionError(t, err, unsupportedPodRequestPolicyReason, "has no Pod")
}

func TestPolicyUnitKeyForSelectsEffectivePolicyOwner(t *testing.T) {
	if _, hasPolicy := policyUnitKeyFor(nil, nil); hasPolicy {
		t.Fatal("policyUnitKeyFor(nil, nil) reported a policy")
	}

	job := api.NewJobInfo("job")
	subJob := &api.SubJobInfo{UID: "workers", Job: job.UID}
	if _, hasPolicy := policyUnitKeyFor(job, subJob); hasPolicy {
		t.Fatal("policyUnitKeyFor() reported a policy for an unconfigured Job")
	}

	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	key, hasPolicy := policyUnitKeyFor(job, subJob)
	if !hasPolicy || key != parentPolicyUnitKey(job) {
		t.Fatalf("parent policy key = (%#v, %t), want (%#v, true)", key, hasPolicy, parentPolicyUnitKey(job))
	}

	subJob.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode)
	key, hasPolicy = policyUnitKeyFor(job, subJob)
	if !hasPolicy || key != subGroupPolicyUnitKey(job, subJob) {
		t.Fatalf("subgroup policy key = (%#v, %t), want (%#v, true)", key, hasPolicy, subGroupPolicyUnitKey(job, subJob))
	}
}

func assertPolicyResolutionError(t *testing.T, err error, wantReason, wantMessage string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s containing %q", wantReason, wantMessage)
	}

	var resolutionError *policyResolutionError
	if !errors.As(err, &resolutionError) {
		t.Fatalf("error type = %T, want *policyResolutionError", err)
	}
	if resolutionError.reason != wantReason {
		t.Fatalf("error reason = %q, want %q", resolutionError.reason, wantReason)
	}
	if !strings.Contains(resolutionError.message, wantMessage) {
		t.Fatalf("error message = %q, want it to contain %q", resolutionError.message, wantMessage)
	}
}
