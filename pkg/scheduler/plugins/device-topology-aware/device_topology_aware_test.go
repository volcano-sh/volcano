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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestDeviceTopologyPluginResolvesParentPolicy(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	gpuTask := newDeviceTopologyTask("gpu-worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "2")}, nil)
	cpuTask := newDeviceTopologyTask("cpu-worker", []corev1.Container{containerWithRequest("worker", corev1.ResourceCPU, "1")}, nil)
	job := api.NewJobInfo("job", gpuTask, cpuTask)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)

	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(&framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job}})

	policy, err := plugin.policyFor(job, job.SubJobs[job.DefaultSubJobID()])
	if err != nil {
		t.Fatalf("policyFor() error = %v", err)
	}
	if policy == nil {
		t.Fatal("policyFor() = nil, want parent policy")
	}
	if policy.scope != parentPolicyScope || policy.schedulingUnitID != string(job.UID) {
		t.Fatalf("resolved parent scope = (%q, %q), want (%q, %q)", policy.scope, policy.schedulingUnitID, parentPolicyScope, job.UID)
	}
	if len(policy.taskIDs) != 2 {
		t.Fatalf("policy Task count = %d, want 2", len(policy.taskIDs))
	}
	if len(policy.requirements) != 1 || len(policy.requirements[0].tasks) != 1 {
		t.Fatalf("resolved requirements = %#v, want one GPU Task", policy.requirements)
	}
	request := policy.requirements[0].tasks[0]
	if request.taskID != gpuTask.UID || request.containerName != "trainer" || request.quantity != 2 {
		t.Fatalf("resolved request = %#v, want GPU trainer request for two devices", request)
	}
}

func TestDeviceTopologyPluginResolvesSubGroupPolicy(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job")
	subJob := &api.SubJobInfo{
		UID:             "workers-0",
		Job:             job.UID,
		DeviceTopology:  testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.SoftDeviceTopologyMode),
		Tasks:           map[api.TaskID]*api.TaskInfo{task.UID: task},
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{},
	}
	job.SubJobs[subJob.UID] = subJob
	job.TaskToSubJob[task.UID] = subJob.UID

	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(&framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job}})

	policy, err := plugin.policyFor(job, subJob)
	if err != nil {
		t.Fatalf("policyFor() error = %v", err)
	}
	if policy == nil {
		t.Fatal("policyFor() = nil, want subgroup policy")
	}
	if policy.scope != subGroupPolicyScope || policy.schedulingUnitID != string(subJob.UID) {
		t.Fatalf("resolved subgroup scope = (%q, %q), want (%q, %q)", policy.scope, policy.schedulingUnitID, subGroupPolicyScope, subJob.UID)
	}
	if policy.requirements[0].mode != scheduling.SoftDeviceTopologyMode {
		t.Fatalf("resolved requirement mode = %q, want soft", policy.requirements[0].mode)
	}
	parentPolicy, err := plugin.policyFor(job, nil)
	if err != nil || parentPolicy != nil {
		t.Fatalf("parent policy = (%#v, %v), want (nil, nil)", parentPolicy, err)
	}
}

func TestDeviceTopologyPluginRejectsConflictingPolicyScopes(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job")
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	subJob := &api.SubJobInfo{
		UID:             "workers-0",
		Job:             job.UID,
		DeviceTopology:  testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode),
		Tasks:           map[api.TaskID]*api.TaskInfo{task.UID: task},
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{},
	}
	job.SubJobs[subJob.UID] = subJob

	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(&framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job}})

	if _, err := plugin.policyFor(job, nil); err == nil || !strings.Contains(err.Error(), "both parent and subgroup") {
		t.Fatalf("parent policy error = %v, want conflicting-scope error", err)
	}
	if _, err := plugin.policyFor(job, subJob); err == nil || !strings.Contains(err.Error(), "both parent and subgroup") {
		t.Fatalf("subgroup policy error = %v, want conflicting-scope error", err)
	}
}

func TestDeviceTopologyPluginSkipsPoliciesWhenFeatureDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, false)

	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)

	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(&framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job}})
	policy, err := plugin.policyFor(job, nil)
	if err != nil || policy != nil {
		t.Fatalf("disabled feature policy = (%#v, %v), want (nil, nil)", policy, err)
	}
}

func TestDeviceTopologyPluginClearsSessionPolicyState(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	task := newDeviceTopologyTask("worker", []corev1.Container{containerWithRequest("trainer", "nvidia.com/gpu", "1")}, nil)
	job := api.NewJobInfo("job", task)
	job.DeviceTopology = testDeviceTopologyPolicy("nvidia.com/gpu", scheduling.HardDeviceTopologyMode)
	session := &framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job}}

	plugin := New(framework.Arguments{}).(*deviceTopologyPlugin)
	plugin.OnSessionOpen(session)
	if len(plugin.policies) != 1 || len(plugin.errors) != 0 {
		t.Fatalf("session state = (%d policies, %d errors), want (1, 0)", len(plugin.policies), len(plugin.errors))
	}

	plugin.OnSessionClose(session)
	if len(plugin.policies) != 0 || len(plugin.errors) != 0 {
		t.Fatalf("closed session state = (%d policies, %d errors), want (0, 0)", len(plugin.policies), len(plugin.errors))
	}
}

func newDeviceTopologyTask(name string, containers, initContainers []corev1.Container) *api.TaskInfo {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(name),
		},
		Spec: corev1.PodSpec{
			Containers:     containers,
			InitContainers: initContainers,
		},
	}
	return api.NewTaskInfo(pod)
}

func containerWithRequest(name string, resourceName corev1.ResourceName, quantity string) corev1.Container {
	return corev1.Container{
		Name: name,
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			resourceName: resource.MustParse(quantity),
		}},
	}
}

func testDeviceTopologyPolicy(resourceName corev1.ResourceName, mode scheduling.DeviceTopologyMode) *scheduling.DeviceTopologySpec {
	return &scheduling.DeviceTopologySpec{
		Requirements: []scheduling.DeviceTopologyRequirement{{
			Resource: scheduling.DeviceResourceSelector{ExtendedResourceName: resourceName},
			Mode:     mode,
		}},
	}
}
