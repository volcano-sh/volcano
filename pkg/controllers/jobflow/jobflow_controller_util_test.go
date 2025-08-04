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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

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

		result := mergeJobLevelTasks(&baseTasks, &patchTasks)
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

		result := mergeJobLevelTasks(&baseTasks, &patchTasks)
		containers := result[0].Template.Spec.Containers
		if containers[0].Image != "image1-new" || containers[1].Image != "image2-new" {
			t.Error("Container images not properly merged")
		}
	})

	t.Run("null base tasks", func(t *testing.T) {
		var baseTasks []batch.TaskSpec
		patchTasks := []batch.TaskSpec{{Name: "task1"}}

		result := mergeJobLevelTasks(&baseTasks, &patchTasks)
		if len(result) != 1 {
			t.Errorf("Expected 1 task when base is empty, got %d", len(result))
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

		result := mergeJobLevelTasks(&baseTasks, &patchTasks)
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

		mergedTasks := mergeJobLevelTasks(&baseTasks, &patchTasks)

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

		result := mergeJobLevelTasks(&baseTasks, &emptyTasks)
		if len(result) != 1 {
			t.Errorf("Expected 1 task when patch is empty, got %d", len(result))
		}
	})

	t.Run("non-overlapping tasks", func(t *testing.T) {
		baseTasks := []batch.TaskSpec{{Name: "task1"}}
		patchTasks := []batch.TaskSpec{{Name: "task2"}}

		result := mergeJobLevelTasks(&baseTasks, &patchTasks)
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

		result := mergeJobLevelTasks(&baseTasks, &patchTasks)
		if len(result[0].Template.Spec.Containers[0].Env) != 2 {
			t.Errorf("Expected 2 env vars after merge, got %d",
				len(result[0].Template.Spec.Containers[0].Env))
		}
	})
}
