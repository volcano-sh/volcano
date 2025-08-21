/*
Copyright 2022 The Volcano Authors.

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

package jobflow

import (
	"testing"
	v1alpha1flow "volcano.sh/apis/pkg/apis/flow/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestGetJobNameFunc(t *testing.T) {
	type args struct {
		jobFlowName     string
		jobTemplateName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "GetJobName success case",
			args: args{
				jobFlowName:     "jobFlowA",
				jobTemplateName: "jobTemplateA",
			},
			want: "jobFlowA-jobTemplateA",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getJobName(tt.args.jobFlowName, tt.args.jobTemplateName); got != tt.want {
				t.Errorf("getJobName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetConnectionOfJobAndJobTemplate(t *testing.T) {
	type args struct {
		namespace string
		name      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "TestGetConnectionOfJobAndJobTemplate",
			args: args{
				namespace: "default",
				name:      "flow",
			},
			want: "default.flow",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateObjectString(tt.args.namespace, tt.args.name); got != tt.want {
				t.Errorf("GenerateObjectString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetJobFlowNameByJob(t *testing.T) {
	type args struct {
		job *batch.Job
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "TestGetConnectionOfJobAndJobTemplate",
			args: args{
				job: &batch.Job{
					TypeMeta: v1.TypeMeta{},
					ObjectMeta: v1.ObjectMeta{
						OwnerReferences: []v1.OwnerReference{
							{
								APIVersion:         "flow.volcano.sh/v1alpha1",
								Kind:               JobFlow,
								Name:               "jobflowtest",
								UID:                "",
								Controller:         nil,
								BlockOwnerDeletion: nil,
							},
						},
					},
					Spec:   batch.JobSpec{},
					Status: batch.JobStatus{},
				},
			},
			want: "jobflowtest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getJobFlowNameByJob(tt.args.job); got != tt.want {
				t.Errorf("getJobFlowNameByJob() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeJobLevelTasks(t *testing.T) {
	t.Run("volume mount merge", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{{Name: "vol1"}},
						Containers: []corev1.Container{
							{
								Name: "container1",
								VolumeMounts: []corev1.VolumeMount{
									{Name: "vol1", MountPath: "/data"},
								},
							},
						},
					},
				},
			},
		}

		patchTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{{Name: "vol2"}},
						Containers: []corev1.Container{
							{
								Name: "container1",
								VolumeMounts: []corev1.VolumeMount{
									{Name: "vol2", MountPath: "/config"},
								},
							},
						},
					},
				},
			},
		}

		result, err := mergeJobLevelTasks(baseTasks, patchTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		container := result[0].Template.Spec.Containers[0]
		if len(container.VolumeMounts) != 2 {
			t.Errorf("Expected 2 volume mounts, got %d", len(container.VolumeMounts))
		}
	})

	t.Run("multiple containers merge", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "container1", Image: "image1"},
							{Name: "container2", Image: "image2"},
						},
					},
				},
			},
		}

		patchTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "container1", Image: "image1-new"},
							{Name: "container2", Image: "image2-new"},
						},
					},
				},
			},
		}

		result, err := mergeJobLevelTasks(baseTasks, patchTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		containers := result[0].Template.Spec.Containers
		if containers[0].Image != "image1-new" || containers[1].Image != "image2-new" {
			t.Error("Container images not properly merged")
		}
	})

	t.Run("empty base tasks", func(t *testing.T) {
		var baseTasks []batch.TaskSpec
		patchTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "container1",
								Image: "test-image",
							},
						},
					},
				},
			},
		}

		result, err := mergeJobLevelTasks(baseTasks, patchTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("Expected 1 task when base is empty, got %d", len(result))
		}
		if result[0].Name != "task1" {
			t.Errorf("Expected task name 'task1', got %s", result[0].Name)
		}
		if result[0].Template.Spec.Containers[0].Image != "test-image" {
			t.Errorf("Expected image 'test-image', got %s", result[0].Template.Spec.Containers[0].Image)
		}
	})

	t.Run("resource requirements merge", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "container1",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
		}

		patchTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "container1",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
				},
			},
		}

		result, err := mergeJobLevelTasks(baseTasks, patchTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		resources := result[0].Template.Spec.Containers[0].Resources
		if len(resources.Limits) != 2 {
			t.Errorf("Expected 2 resource limits after merge, got %d", len(resources.Limits))
		}
	})

	t.Run("basic merge", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "container1",
								Image: "image1",
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 80,
									},
								},
							},
						},
					},
				},
			},
		}

		patchTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "container1",
								Image: "image2",
								LivenessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/healthz",
											Port: intstr.FromInt(80),
										},
									},
								},
							},
						},
					},
				},
			},
		}

		mergedTasks, err := mergeJobLevelTasks(baseTasks, patchTasks)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if len(mergedTasks) != 1 {
			t.Errorf("Expected 1 task, got %d", len(mergedTasks))
		}

		mergedTask := mergedTasks[0]
		if mergedTask.Name != "task1" {
			t.Errorf("Expected task name 'task1', got %s", mergedTask.Name)
		}

		container := mergedTask.Template.Spec.Containers[0]
		if container.Image != "image2" {
			t.Errorf("Expected image 'image2', got %s", container.Image)
		}

		if container.LivenessProbe == nil {
			t.Error("Expected LivenessProbe to be present")
		}

		if len(container.Ports) != 1 {
			t.Errorf("Expected 1 port, got %d", len(container.Ports))
		}
	})

	t.Run("empty tasks", func(t *testing.T) {
		emptyTasks := []batch.TaskSpec{}
		baseTasks := []batch.TaskSpec{{Name: "task1"}}

		result, err := mergeJobLevelTasks(baseTasks, emptyTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("Expected 1 task when patch is empty, got %d", len(result))
		}
	})

	t.Run("non-overlapping tasks", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{{Name: "task1"}}
		patchTasks := []batch.TaskSpec{{Name: "task2"}}

		result, err := mergeJobLevelTasks(baseTasks, patchTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 tasks for non-overlapping merge, got %d", len(result))
		}
	})

	t.Run("complex container merge", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "container1",
								Env: []corev1.EnvVar{
									{Name: "VAR1", Value: "value1"},
								},
							},
						},
					},
				},
			},
		}

		patchTasks := []batch.TaskSpec{
			{
				Name: "task1",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "container1",
								Env: []corev1.EnvVar{
									{Name: "VAR2", Value: "value2"},
								},
							},
						},
					},
				},
			},
		}

		result, err := mergeJobLevelTasks(baseTasks, patchTasks)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if len(result[0].Template.Spec.Containers[0].Env) != 2 {
			t.Errorf("Expected 2 env vars after merge, got %d",
				len(result[0].Template.Spec.Containers[0].Env))
		}
	})
}

func TestGetFlowByName(t *testing.T) {
	testCases := []struct {
		name        string
		jobFlow     *v1alpha1flow.JobFlow
		flowName    string
		expectFlow  *v1alpha1flow.Flow
		expectError bool
	}{
		{
			name: "flow exists",
			jobFlow: &v1alpha1flow.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobflow",
				},
				Spec: v1alpha1flow.JobFlowSpec{
					Flows: []v1alpha1flow.Flow{
						{
							Name: "flow1",
						},
						{
							Name: "flow2",
						},
					},
				},
			},
			flowName:    "flow1",
			expectFlow:  &v1alpha1flow.Flow{Name: "flow1"},
			expectError: false,
		},
		{
			name: "flow does not exist",
			jobFlow: &v1alpha1flow.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobflow",
				},
				Spec: v1alpha1flow.JobFlowSpec{
					Flows: []v1alpha1flow.Flow{
						{
							Name: "flow1",
						},
					},
				},
			},
			flowName:    "non-existent",
			expectFlow:  nil,
			expectError: true,
		},
		{
			name:        "nil jobFlow",
			jobFlow:     nil,
			flowName:    "flow1",
			expectFlow:  nil,
			expectError: true,
		},
		{
			name: "empty flows array",
			jobFlow: &v1alpha1flow.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobflow",
				},
				Spec: v1alpha1flow.JobFlowSpec{
					Flows: []v1alpha1flow.Flow{},
				},
			},
			flowName:    "flow1",
			expectFlow:  nil,
			expectError: true,
		},
		{
			name: "duplicate flow names",
			jobFlow: &v1alpha1flow.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobflow",
				},
				Spec: v1alpha1flow.JobFlowSpec{
					Flows: []v1alpha1flow.Flow{
						{
							Name: "flow1",
						},
						{
							Name: "flow1",
						},
					},
				},
			},
			flowName:    "flow1",
			expectFlow:  &v1alpha1flow.Flow{Name: "flow1"},
			expectError: false,
		},
		{
			name: "empty flow name",
			jobFlow: &v1alpha1flow.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobflow",
				},
				Spec: v1alpha1flow.JobFlowSpec{
					Flows: []v1alpha1flow.Flow{
						{
							Name: "",
						},
					},
				},
			},
			flowName:    "",
			expectFlow:  &v1alpha1flow.Flow{Name: ""},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flow, err := getFlowByName(tc.jobFlow, tc.flowName)
			if tc.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tc.expectFlow != nil && flow.Name != tc.expectFlow.Name {
				t.Errorf("expected flow name %s but got %s", tc.expectFlow.Name, flow.Name)
			}
		})
	}
}
