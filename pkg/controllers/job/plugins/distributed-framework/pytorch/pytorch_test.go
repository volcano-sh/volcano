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

package pytorch

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestPytorchPodEnvAndPort(t *testing.T) {
	plugins := make(map[string][]string)
	plugins[PytorchPluginName] = []string{"--port=5000"}

	testcases := []struct {
		Name string
		Job  *v1alpha1.Job
		Pod  *v1.Pod
		port int
		envs []v1.EnvVar
	}{
		{
			Name: "test pod without master",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "worker",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "worker",
						},
					},
				},
			},
			port: -1,
			envs: nil,
		},
		{
			Name: "test master pod without port",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-master-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "master",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "master",
						},
					},
				},
			},
			port: DefaultPort,
			envs: []v1.EnvVar{
				{
					Name:  EnvMasterAddr,
					Value: "test-pytorch-master-0.test-pytorch",
				},
				{
					Name:  EnvMasterPort,
					Value: fmt.Sprintf("%v", DefaultPort),
				},
				{
					Name:  "WORLD_SIZE",
					Value: fmt.Sprintf("%v", 2),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 0),
				},
			},
		},
		{
			Name: "test master pod with port",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-master-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "master",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "master",
							Ports: []v1.ContainerPort{
								{
									Name:          "pytorchjob-port",
									ContainerPort: 23456,
								},
							},
						},
					},
				},
			},
			port: DefaultPort,
			envs: []v1.EnvVar{
				{
					Name:  EnvMasterAddr,
					Value: "test-pytorch-master-0.test-pytorch",
				},
				{
					Name:  EnvMasterPort,
					Value: fmt.Sprintf("%v", DefaultPort),
				},
				{
					Name:  "WORLD_SIZE",
					Value: fmt.Sprintf("%v", 2),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 0),
				},
			},
		},
		{
			Name: "test master pod env",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "extra",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-master-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "master",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "master",
							Ports: []v1.ContainerPort{
								{
									Name:          "pytorchjob-port",
									ContainerPort: 123,
								},
							},
						},
					},
				},
			},
			port: 123,
			envs: []v1.EnvVar{
				{
					Name:  EnvMasterAddr,
					Value: "test-pytorch-master-0.test-pytorch",
				},
				{
					Name:  EnvMasterPort,
					Value: fmt.Sprintf("%v", DefaultPort),
				},
				{
					Name:  "WORLD_SIZE",
					Value: fmt.Sprintf("%v", 3),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 0),
				},
			},
		},
		{
			Name: "test worker-1 pod env",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "extra",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "worker",
							Ports: []v1.ContainerPort{
								{
									Name:          "pytorchjob-port",
									ContainerPort: 123,
								},
							},
						},
					},
				},
			},
			port: 123,
			envs: []v1.EnvVar{
				{
					Name:  EnvMasterAddr,
					Value: "test-pytorch-master-0.test-pytorch",
				},
				{
					Name:  EnvMasterPort,
					Value: fmt.Sprintf("%v", DefaultPort),
				},
				{
					Name:  "WORLD_SIZE",
					Value: fmt.Sprintf("%v", 3),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 1),
				},
			},
		},
		{
			Name: "test worker-2 pod env",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "extra",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-1",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "worker",
							Ports: []v1.ContainerPort{
								{
									Name:          "pytorchjob-port",
									ContainerPort: 123,
								},
							},
						},
					},
				},
			},
			port: 123,
			envs: []v1.EnvVar{
				{
					Name:  EnvMasterAddr,
					Value: "test-pytorch-master-0.test-pytorch",
				},
				{
					Name:  EnvMasterPort,
					Value: fmt.Sprintf("%v", DefaultPort),
				},
				{
					Name:  "WORLD_SIZE",
					Value: fmt.Sprintf("%v", 3),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 2),
				},
			},
		},
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			mp := New(pluginsinterface.PluginClientset{}, testcase.Job.Spec.Plugins[PytorchPluginName])
			if err := mp.OnPodCreate(testcase.Pod, testcase.Job); err != nil {
				t.Errorf("Case %d (%s): expect no error, but got error %v", index, testcase.Name, err)
			}

			if testcase.port != -1 {
				if testcase.Pod.Spec.Containers[0].Ports == nil || testcase.Pod.Spec.Containers[0].Ports[0].ContainerPort != int32(testcase.port) {
					t.Errorf("Case %d (%s): wrong port, got %d, expected %v", index, testcase.Name, testcase.Pod.Spec.Containers[0].Ports[0].ContainerPort, testcase.port)
				}
			} else {
				if testcase.Pod.Spec.Containers[0].Ports != nil {
					t.Errorf("Case %d (%s): wrong port, got %d, expected empty", index, testcase.Name, testcase.Pod.Spec.Containers[0].Ports[0].ContainerPort)
				}
			}

			if !equality.Semantic.DeepEqual(testcase.Pod.Spec.Containers[0].Env, testcase.envs) {
				t.Errorf("Case %d (%s): wrong envs, got %v, expected %v", index, testcase.Name, testcase.Pod.Spec.Containers[0].Env, testcase.envs)
			}
		})
	}
}

func TestPytorchInitContainer(t *testing.T) {
	// Create plugin with wait-master enabled to generate expected script
	ppEnabled := New(pluginsinterface.PluginClientset{}, []string{"--wait-master-enabled=true"}).(*pytorchPlugin)
	expectedScript := ppEnabled.generateWaitForMasterScript("test-pytorch-master-0.test-pytorch")

	testcases := []struct {
		Name                 string
		PluginArgs           []string
		Job                  *v1alpha1.Job
		Pod                  *v1.Pod
		expectInitContainers []v1.Container
	}{
		{
			Name:       "worker pod with wait-master enabled should have init container",
			PluginArgs: []string{"--wait-master-enabled=true"},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "worker"},
					},
				},
			},
			expectInitContainers: []v1.Container{
				{
					Name:  "wait-for-master",
					Image: "busybox:1.36.1",
					Command: []string{
						"sh",
						"-c",
						expectedScript,
					},
					Resources: v1.ResourceRequirements{},
				},
			},
		},
		{
			Name:       "worker pod with wait-master disabled should not have init container",
			PluginArgs: []string{"--wait-master-enabled=false"},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "worker"},
					},
				},
			},
			expectInitContainers: nil,
		},
		{
			Name:       "master pod should not have wait-for-master init container",
			PluginArgs: []string{"--wait-master-enabled=true"},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-master-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "master",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "master"},
					},
				},
			},
			expectInitContainers: nil,
		},
		{
			Name:       "pod without master task should not have init container",
			PluginArgs: []string{"--wait-master-enabled=true"},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "worker",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "worker"},
					},
				},
			},
			expectInitContainers: nil,
		},
		{
			Name:       "worker pod with existing wait-for-master init container should not add duplicate",
			PluginArgs: []string{"--wait-master-enabled=true"},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name:  "wait-for-master",
							Image: "busybox:1.36.1",
							Command: []string{
								"sh",
								"-c",
								"echo 'already exists'",
							},
						},
					},
					Containers: []v1.Container{
						{Name: "worker"},
					},
				},
			},
			expectInitContainers: []v1.Container{
				{
					Name:  "wait-for-master",
					Image: "busybox:1.36.1",
					Command: []string{
						"sh",
						"-c",
						"echo 'already exists'",
					},
				},
			},
		},
		{
			Name:       "worker pod with custom image and timeout",
			PluginArgs: []string{"--wait-master-enabled=true", "--wait-master-image=alpine:latest", "--wait-master-timeout=600"},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pytorch-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "worker"},
					},
				},
			},
			expectInitContainers: []v1.Container{
				{
					Name:  "wait-for-master",
					Image: "alpine:latest",
					Command: []string{
						"sh",
						"-c",
						New(pluginsinterface.PluginClientset{}, []string{"--wait-master-enabled=true", "--wait-master-timeout=600"}).(*pytorchPlugin).generateWaitForMasterScript("test-pytorch-master-0.test-pytorch"),
					},
					Resources: v1.ResourceRequirements{},
				},
			},
		},
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			pp := New(pluginsinterface.PluginClientset{}, testcase.PluginArgs)
			if err := pp.OnPodCreate(testcase.Pod, testcase.Job); err != nil {
				t.Errorf("Case %d (%s): expect no error, but got error %v", index, testcase.Name, err)
			}

			if diff := cmp.Diff(testcase.expectInitContainers, testcase.Pod.Spec.InitContainers); diff != "" {
				t.Errorf("Case %d (%s): init containers mismatch (-want +got):\n%s", index, testcase.Name, diff)
			}
		})
	}
}
