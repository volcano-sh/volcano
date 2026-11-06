/*
Copyright 2025 The Volcano Authors.

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

package hcclrank

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

// TestNew tests the New function to ensure it creates a plugin instance correctly
func TestNew(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{
		KubeClients: fake.NewSimpleClientset(),
	}

	arguments := []string{}
	plugin := New(clientset, arguments)

	if plugin == nil {
		t.Fatal("Expected plugin to be created, but got nil")
	}

	if plugin.Name() != HCCLRankPluginName {
		t.Errorf("Expected plugin name to be %s, but got %s", HCCLRankPluginName, plugin.Name())
	}
}

// TestHcclrankPlugin_Name tests the Name method
func TestHcclrankPlugin_Name(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{clientset: clientset}

	name := plugin.Name()
	if name != HCCLRankPluginName {
		t.Errorf("Expected plugin name to be %s, but got %s", HCCLRankPluginName, name)
	}
}

// TestHcclrankPlugin_OnJobAdd tests the OnJobAdd method
func TestHcclrankPlugin_OnJobAdd(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{clientset: clientset}

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	err := plugin.OnJobAdd(job)
	if err != nil {
		t.Errorf("Expected OnJobAdd to return nil, but got error: %v", err)
	}
}

// TestHcclrankPlugin_OnJobDelete tests the OnJobDelete method
func TestHcclrankPlugin_OnJobDelete(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{clientset: clientset}

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	err := plugin.OnJobDelete(job)
	if err != nil {
		t.Errorf("Expected OnJobDelete to return nil, but got error: %v", err)
	}
}

// TestHcclrankPlugin_OnJobUpdate tests the OnJobUpdate method
func TestHcclrankPlugin_OnJobUpdate(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{clientset: clientset}

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	err := plugin.OnJobUpdate(job)
	if err != nil {
		t.Errorf("Expected OnJobUpdate to return nil, but got error: %v", err)
	}
}

// TestGetEnv tests the getEnv function
func TestGetEnv(t *testing.T) {
	testCases := []struct {
		name     string
		pod      *v1.Pod
		envName  string
		expected string
	}{
		{
			name: "Find environment variable in first container",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container1",
							Env: []v1.EnvVar{
								{Name: "RANK", Value: "0"},
								{Name: "OTHER", Value: "value"},
							},
						},
					},
				},
			},
			envName:  "RANK",
			expected: "0",
		},
		{
			name: "Find environment variable in second container",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container1",
							Env: []v1.EnvVar{
								{Name: "OTHER", Value: "value"},
							},
						},
						{
							Name: "container2",
							Env: []v1.EnvVar{
								{Name: "RANK", Value: "1"},
							},
						},
					},
				},
			},
			envName:  "RANK",
			expected: "1",
		},
		{
			name: "Environment variable not found",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container1",
							Env: []v1.EnvVar{
								{Name: "OTHER", Value: "value"},
							},
						},
					},
				},
			},
			envName:  "RANK",
			expected: "",
		},
		{
			name: "Empty containers list",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{},
				},
			},
			envName:  "RANK",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getEnv(tc.pod, tc.envName)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}

// TestHcclrankPlugin_OnPodCreate tests the OnPodCreate method with various scenarios
func TestHcclrankPlugin_OnPodCreate(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:   clientset,
		masterName:  "master",
		workerNames: []string{"worker"},
	}

	testCases := []struct {
		name           string
		pod            *v1.Pod
		job            *batch.Job
		expectedRank   string
		expectError    bool
		expectedAction string
	}{
		{
			name: "Pod already has HCCL rank annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						HCCLRankKey: "1",
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
			},
			expectedRank:   "1",
			expectError:    false,
			expectedAction: "keep existing",
		},
		{
			name: "Pod has RANK environment variable",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container1",
							Env: []v1.EnvVar{
								{Name: EnvRank, Value: "5"},
							},
						},
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
			},
			expectedRank:   "5",
			expectError:    false,
			expectedAction: "use env rank",
		},
		{
			name: "Master pod with valid task index",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						batch.TaskSpecKey: "master",
					},
					Labels: map[string]string{
						batch.TaskIndex: "2",
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "master",
							Replicas: 3,
						},
						{
							Name:     "worker",
							Replicas: 2,
						},
					},
				},
			},
			expectedRank:   "2",
			expectError:    false,
			expectedAction: "master rank",
		},
		{
			name: "Worker pod with valid task index",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						batch.TaskSpecKey: "worker",
					},
					Labels: map[string]string{
						batch.TaskIndex: "1",
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "master",
							Replicas: 2,
						},
						{
							Name:     "worker",
							Replicas: 3,
						},
					},
				},
			},
			expectedRank:   "3", // master replicas (2) + worker index (1)
			expectError:    false,
			expectedAction: "worker rank",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			podCopy := tc.pod.DeepCopy()

			err := plugin.OnPodCreate(podCopy, tc.job)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none for test case: %s", tc.name)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v for test case: %s", err, tc.name)
				return
			}

			if tc.expectedRank != "" {
				if podCopy.Annotations == nil {
					t.Errorf("Expected annotations to be created for test case: %s", tc.name)
					return
				}

				actualRank := podCopy.Annotations[HCCLRankKey]
				if actualRank != tc.expectedRank {
					t.Errorf("Expected rank %s, but got %s for test case: %s", tc.expectedRank, actualRank, tc.name)
				}
			} else {
				if podCopy.Annotations != nil {
					if rank, exists := podCopy.Annotations[HCCLRankKey]; exists && rank != "" {
						t.Errorf("Expected no rank to be set, but got %s for test case: %s", rank, tc.name)
					}
				}
			}
		})
	}
}

// TestHcclrankPlugin_OnPodCreate_WithCustomTaskNames tests OnPodCreate with custom master and worker task names
func TestHcclrankPlugin_OnPodCreate_WithCustomTaskNames(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:   clientset,
		masterName:  "custom-master",
		workerNames: []string{"custom-worker"},
	}

	testCases := []struct {
		name         string
		pod          *v1.Pod
		job          *batch.Job
		expectedRank string
	}{
		{
			name: "Custom master pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						batch.TaskSpecKey: "custom-master",
					},
					Labels: map[string]string{
						batch.TaskIndex: "3",
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "custom-master",
							Replicas: 5,
						},
						{
							Name:     "custom-worker",
							Replicas: 3,
						},
					},
				},
			},
			expectedRank: "3",
		},
		{
			name: "Custom worker pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						batch.TaskSpecKey: "custom-worker",
					},
					Labels: map[string]string{
						batch.TaskIndex: "2",
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "custom-master",
							Replicas: 4,
						},
						{
							Name:     "custom-worker",
							Replicas: 3,
						},
					},
				},
			},
			expectedRank: "6", // master replicas (4) + worker index (2)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			podCopy := tc.pod.DeepCopy()

			err := plugin.OnPodCreate(podCopy, tc.job)
			if err != nil {
				t.Errorf("Unexpected error: %v for test case: %s", err, tc.name)
				return
			}

			actualRank := podCopy.Annotations[HCCLRankKey]
			if actualRank != tc.expectedRank {
				t.Errorf("Expected rank %s, but got %s for test case: %s", tc.expectedRank, actualRank, tc.name)
			}
		})
	}
}

// TestHcclrankPlugin_OnPodCreate_EdgeCases tests edge cases for OnPodCreate
func TestHcclrankPlugin_OnPodCreate_EdgeCases(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:   clientset,
		masterName:  "master",
		workerNames: []string{"worker"},
	}

	testCases := []struct {
		name        string
		pod         *v1.Pod
		job         *batch.Job
		expectError bool
	}{
		{
			name: "Nil job",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						batch.TaskSpecKey: "master",
					},
					Labels: map[string]string{
						batch.TaskIndex: "0",
					},
				},
			},
			job:         nil,
			expectError: false, // just return nil
		},
		{
			name: "Zero master replicas",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						batch.TaskSpecKey: "worker",
					},
					Labels: map[string]string{
						batch.TaskIndex: "0",
					},
				},
			},
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "master",
							Replicas: 0,
						},
						{
							Name:     "worker",
							Replicas: 2,
						},
					},
				},
			},
			expectError: false, // Should work with zero master replicas
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use defer to catch panics
			defer func() {
				if r := recover(); r != nil {
					if !tc.expectError {
						t.Errorf("Unexpected panic: %v for test case: %s", r, tc.name)
					}
				}
			}()

			err := plugin.OnPodCreate(tc.pod, tc.job)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none for test case: %s", tc.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v for test case: %s", err, tc.name)
				}
			}
		})
	}
}

// TestAddFlags tests the addFlags method
func TestAddFlags(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:         clientset,
		hcclrankArguments: []string{"--master", "test-master", "--worker", "test-worker"},
	}

	plugin.addFlags()

	if plugin.masterName != "test-master" {
		t.Errorf("Expected master name to be 'test-master', but got '%s'", plugin.masterName)
	}
	if len(plugin.workerNames) != 1 || plugin.workerNames[0] != "test-worker" {
		t.Errorf("Expected worker names to be ['test-worker'], but got %v", plugin.workerNames)
	}
}

// TestAddFlags_EmptyArguments tests addFlags with empty arguments
func TestAddFlags_EmptyArguments(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:         clientset,
		hcclrankArguments: []string{},
	}

	plugin.addFlags()
	if plugin.masterName != DefaultMaster {
		t.Errorf("Expected master name to be default '%s', but got '%s'", DefaultMaster, plugin.masterName)
	}
	if len(plugin.workerNames) != 1 || plugin.workerNames[0] != DefaultWorker {
		t.Errorf("Expected worker names to be default ['%s'], but got %v", DefaultWorker, plugin.workerNames)
	}
}

// TestGetEnv_WithMultipleContainers tests getEnv with multiple containers
func TestGetEnv_WithMultipleContainers(t *testing.T) {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "container1",
					Env: []v1.EnvVar{
						{Name: "RANK", Value: "first"},
					},
				},
				{
					Name: "container2",
					Env: []v1.EnvVar{
						{Name: "RANK", Value: "second"},
					},
				},
			},
		},
	}

	result := getEnv(pod, "RANK")
	if result != "first" {
		t.Errorf("Expected 'first' (first occurrence), but got '%s'", result)
	}
}

// TestHcclrankPlugin_OnPodCreate_WithHelpers tests the integration with helper functions
func TestHcclrankPlugin_OnPodCreate_WithHelpers(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:   clientset,
		masterName:  "master",
		workerNames: []string{"worker"},
	}

	// Test that the plugin correctly uses helpers.GetTaskKey
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Annotations: map[string]string{
				batch.TaskSpecKey: "master",
			},
			Labels: map[string]string{
				batch.TaskIndex: "5",
			},
		},
	}

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-job",
		},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name:     "master",
					Replicas: 10,
				},
				{
					Name:     "worker",
					Replicas: 5,
				},
			},
		},
	}

	err := plugin.OnPodCreate(pod, job)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify that the task key was correctly retrieved and used
	expectedRank := "5"
	actualRank := pod.Annotations[HCCLRankKey]
	if actualRank != expectedRank {
		t.Errorf("Expected rank %s, but got %s", expectedRank, actualRank)
	}
}

func TestHcclrankPlugin_OnPodCreate_MultipleWorkers(t *testing.T) {
	clientset := pluginsinterface.PluginClientset{}
	plugin := &hcclrankPlugin{
		clientset:   clientset,
		masterName:  "master",
		workerNames: []string{"worker", "worker-0"},
	}

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-job",
		},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name:     "master",
					Replicas: 1,
				},
				{
					Name:     "worker",
					Replicas: 2,
				},
				{
					Name:     "worker-0",
					Replicas: 2,
				},
			},
		},
	}

	testCases := []struct {
		name         string
		taskSpec     string
		taskIndex    string
		expectedRank string
	}{
		{
			name:         "second pod of first worker task",
			taskSpec:     "worker",
			taskIndex:    "1",
			expectedRank: "2", // 1 master replica + 0 preceding workers + index 1
		},
		{
			name:         "second pod of second worker task",
			taskSpec:     "worker-0",
			taskIndex:    "1",
			expectedRank: "4", // 1 master replica + 2 preceding worker replicas + index 1
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Annotations: map[string]string{batch.TaskSpecKey: tc.taskSpec},
					Labels:      map[string]string{batch.TaskIndex: tc.taskIndex},
				},
			}
			if err := plugin.OnPodCreate(pod, job); err != nil {
				t.Fatalf("expect no error, but got error %v", err)
			}
			if rank := pod.Annotations[HCCLRankKey]; rank != tc.expectedRank {
				t.Errorf("expected rank '%s', but got '%s'", tc.expectedRank, rank)
			}
		})
	}
}

func TestHcclrankWorkersFlag(t *testing.T) {
	plugin := &hcclrankPlugin{
		masterName:        "master",
		hcclrankArguments: []string{"--workers=worker,worker-0"},
	}
	plugin.addFlags()

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{Name: "master", Replicas: 1},
				{Name: "worker", Replicas: 2},
				{Name: "worker-0", Replicas: 2},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Annotations: map[string]string{batch.TaskSpecKey: "worker-0"},
			Labels:      map[string]string{batch.TaskIndex: "1"},
		},
	}
	if err := plugin.OnPodCreate(pod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	// rank = 1 master replica + 2 worker replicas listed before + pod index 1
	if rank := pod.Annotations[HCCLRankKey]; rank != "4" {
		t.Errorf("expected rank '4', but got '%s'", rank)
	}
}
