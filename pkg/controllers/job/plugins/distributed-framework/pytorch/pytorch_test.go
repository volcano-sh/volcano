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
	"k8s.io/apimachinery/pkg/api/resource"
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
					Name:  EnvSensecoreWorldSize,
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNNodes,
					Value: fmt.Sprintf("%v", 2),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNodeRank,
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
					Name:  EnvSensecoreWorldSize,
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNNodes,
					Value: fmt.Sprintf("%v", 2),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNodeRank,
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
					Name:  EnvSensecoreWorldSize,
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNNodes,
					Value: fmt.Sprintf("%v", 3),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNodeRank,
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
					Name:  EnvSensecoreWorldSize,
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNNodes,
					Value: fmt.Sprintf("%v", 3),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 1),
				},
				{
					Name:  EnvSensecoreNodeRank,
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
					Name:  EnvSensecoreWorldSize,
					Value: fmt.Sprintf("%v", 0),
				},
				{
					Name:  EnvSensecoreNNodes,
					Value: fmt.Sprintf("%v", 3),
				},
				{
					Name:  "RANK",
					Value: fmt.Sprintf("%v", 2),
				},
				{
					Name:  EnvSensecoreNodeRank,
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

func TestPytorchMultipleWorkersRank(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-multi"},
		Spec: v1alpha1.JobSpec{
			Plugins: map[string][]string{
				PytorchPluginName: {"--worker=worker,worker-0,worker-1"},
			},
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
					Name:     "worker-0",
					Replicas: 3,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "worker-1",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}

	testcases := []struct {
		Name         string
		PodName      string
		TaskSpec     string
		ExpectedRank string
	}{
		{Name: "master pod", PodName: "test-pytorch-multi-master-0", TaskSpec: "master", ExpectedRank: "0"},
		{Name: "second pod of first worker task", PodName: "test-pytorch-multi-worker-1", TaskSpec: "worker", ExpectedRank: "2"},
		{Name: "third pod of second worker task", PodName: "test-pytorch-multi-worker-0-2", TaskSpec: "worker-0", ExpectedRank: "5"},
		{Name: "first pod of third worker task", PodName: "test-pytorch-multi-worker-1-0", TaskSpec: "worker-1", ExpectedRank: "6"},
	}

	getEnvValue := func(pod *v1.Pod, name string) string {
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == name {
				return env.Value
			}
		}
		return ""
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testcase.PodName,
					Annotations: map[string]string{v1alpha1.TaskSpecKey: testcase.TaskSpec},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "main"}},
				},
			}
			pp := New(pluginsinterface.PluginClientset{}, job.Spec.Plugins[PytorchPluginName])
			if err := pp.OnPodCreate(pod, job); err != nil {
				t.Fatalf("Case %d (%s): expect no error, but got error %v", index, testcase.Name, err)
			}
			if rank := getEnvValue(pod, EnvRank); rank != testcase.ExpectedRank {
				t.Errorf("Case %d (%s): RANK mismatch, expected '%s', got '%s'", index, testcase.Name, testcase.ExpectedRank, rank)
			}
			if nodeRank := getEnvValue(pod, EnvSensecoreNodeRank); nodeRank != testcase.ExpectedRank {
				t.Errorf("Case %d (%s): SENSECORE_PYTORCH_NODE_RANK should mirror RANK, expected '%s', got '%s'", index, testcase.Name, testcase.ExpectedRank, nodeRank)
			}
			if worldSize := getEnvValue(pod, EnvWorldSize); worldSize != "7" {
				t.Errorf("Case %d (%s): WORLD_SIZE mismatch, expected '7', got '%s'", index, testcase.Name, worldSize)
			}
			if nnodes := getEnvValue(pod, EnvSensecoreNNodes); nnodes != "7" {
				t.Errorf("Case %d (%s): SENSECORE_PYTORCH_NNODES mismatch, expected '7', got '%s'", index, testcase.Name, nnodes)
			}
			if addr := getEnvValue(pod, EnvMasterAddr); addr != "test-pytorch-multi-master-0.test-pytorch-multi" {
				t.Errorf("Case %d (%s): MASTER_ADDR mismatch, got '%s'", index, testcase.Name, addr)
			}
		})
	}
}

// TestPytorchSingleWorkerNameCompatibility verifies that a single custom worker
// task name keeps exactly the same rank behavior as before the multi-worker
// support.
func TestPytorchSingleWorkerNameCompatibility(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-compat"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:     "master",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "mlworker",
					Replicas: 3,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "other",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}

	getEnvValue := func(pod *v1.Pod, name string) string {
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == name {
				return env.Value
			}
		}
		return ""
	}

	newPod := func(name, taskSpec string) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: map[string]string{v1alpha1.TaskSpecKey: taskSpec},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "main"}},
			},
		}
	}

	// worker pod keeps the legacy rank formula: pod index + 1
	workerPod := newPod("test-pytorch-compat-mlworker-2", "mlworker")
	pp := New(pluginsinterface.PluginClientset{}, []string{"--worker=mlworker"})
	if err := pp.OnPodCreate(workerPod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	if rank := getEnvValue(workerPod, EnvRank); rank != "3" {
		t.Errorf("worker RANK mismatch, expected '3', got '%s'", rank)
	}
	// default NODES_NUMBER mode: WORLD_SIZE counts the replicas of master and
	// the worker task, task "other" contributes none either
	if worldSize := getEnvValue(workerPod, EnvWorldSize); worldSize != "4" {
		t.Errorf("worker WORLD_SIZE mismatch, expected '4', got '%s'", worldSize)
	}

	// task not listed by the worker flag gets no RANK, as before
	otherPod := newPod("test-pytorch-compat-other-0", "other")
	if err := pp.OnPodCreate(otherPod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	if rank := getEnvValue(otherPod, EnvRank); rank != "" {
		t.Errorf("unrelated task should have no RANK, but got '%s'", rank)
	}
	if worldSize := getEnvValue(otherPod, EnvWorldSize); worldSize != "4" {
		t.Errorf("unrelated task WORLD_SIZE mismatch, expected '4', got '%s'", worldSize)
	}
}

// TestPytorchWorkerOrderFollowsWorkerArgument pins that the worker argument
// order defines rank assignment: worker-1 listed before worker-0 contributes
// its replicas to worker-0's rank base, regardless of the task definition
// order in the job.
func TestPytorchWorkerOrderFollowsWorkerArgument(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-order"},
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
					Name:     "worker-0",
					Replicas: 3,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "worker-1",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pytorch-order-worker-0-2",
			Annotations: map[string]string{v1alpha1.TaskSpecKey: "worker-0"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "main"}},
		},
	}

	// worker-0 listed after worker-1, although defined before it in the job
	pp := New(pluginsinterface.PluginClientset{}, []string{"--worker=worker-1,worker-0,worker"})
	if err := pp.OnPodCreate(pod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	// rank = 1 (master) + 1 replica of worker-1 listed before + pod index 2
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == EnvRank && env.Value != "4" {
			t.Errorf("RANK should follow worker argument order, expected '4', got '%s'", env.Value)
		}
	}
}

// TestPytorchWorkersFlag verifies the dedicated multi-worker flag drives the
// same rank assignment as documented.
func TestPytorchWorkersFlag(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-workers"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{Name: "master", Replicas: 1, Template: v1.PodTemplateSpec{}},
				{Name: "worker", Replicas: 2, Template: v1.PodTemplateSpec{}},
				{Name: "worker-0", Replicas: 3, Template: v1.PodTemplateSpec{}},
				{Name: "worker-1", Replicas: 1, Template: v1.PodTemplateSpec{}},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pytorch-workers-worker-0-2",
			Annotations: map[string]string{v1alpha1.TaskSpecKey: "worker-0"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}

	pp := New(pluginsinterface.PluginClientset{}, []string{"--workers=worker-1,worker-0,worker"})
	if err := pp.OnPodCreate(pod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	// rank = 1 (master) + 1 replica of worker-1 listed before + pod index 2
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == EnvRank && env.Value != "4" {
			t.Errorf("RANK mismatch, expected '4', got '%s'", env.Value)
		}
	}
}

// TestPytorchWorkersFlagDedup verifies duplicated worker names in the workers
// flag are counted once: ranks stay contiguous.
func TestPytorchWorkersFlagDedup(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-dedup"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{Name: "master", Replicas: 1, Template: v1.PodTemplateSpec{}},
				{Name: "a", Replicas: 2, Template: v1.PodTemplateSpec{}},
				{Name: "b", Replicas: 1, Template: v1.PodTemplateSpec{}},
			},
		},
	}

	getEnvValue := func(pod *v1.Pod, name string) string {
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == name {
				return env.Value
			}
		}
		return ""
	}
	newPod := func(name, taskSpec string) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: map[string]string{v1alpha1.TaskSpecKey: taskSpec},
			},
			Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
		}
	}

	pp := New(pluginsinterface.PluginClientset{}, []string{"--workers=a, a,b,a"})
	// task a: rank 1,2; task b: rank 1+2=3, not 1+2+2 — contiguous ranks 0..3;
	// the cpu-only job counts 0 accelerator cards so WORLD_SIZE is 0
	cases := []struct{ podName, taskSpec, expectedRank string }{
		{"test-pytorch-dedup-a-0", "a", "1"},
		{"test-pytorch-dedup-a-1", "a", "2"},
		{"test-pytorch-dedup-b-0", "b", "3"},
	}
	for _, tc := range cases {
		pod := newPod(tc.podName, tc.taskSpec)
		if err := pp.OnPodCreate(pod, job); err != nil {
			t.Fatalf("pod %s: expect no error, but got error %v", tc.podName, err)
		}
		if rank := getEnvValue(pod, EnvRank); rank != tc.expectedRank {
			t.Errorf("pod %s: RANK mismatch, expected '%s', got '%s'", tc.podName, tc.expectedRank, rank)
		}
		if ws := getEnvValue(pod, EnvWorldSize); ws != "4" {
			t.Errorf("pod %s: WORLD_SIZE mismatch, expected '4', got '%s'", tc.podName, ws)
		}
	}
}

// TestPytorchWorldSizeCountsAccelerators verifies WORLD_SIZE is the total
// accelerator cards of the job while SENSECORE_PYTORCH_NNODES keeps counting
// replicas; pods without accelerator resources count 0 cards.
func TestPytorchWorldSizeCountsAccelerators(t *testing.T) {
	gpuLimit := v1.ResourceRequirements{
		Limits: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
	}
	npuRequest := v1.ResourceRequirements{
		Requests: v1.ResourceList{"huawei.com/Ascend910": resource.MustParse("8")},
	}
	newTemplate := func(res v1.ResourceRequirements) v1.PodTemplateSpec {
		return v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "main", Resources: res}},
			},
		}
	}
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-cards"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{Name: "master", Replicas: 1, Template: newTemplate(gpuLimit)},               // 1 pod × 2 gpu
				{Name: "worker", Replicas: 2, Template: newTemplate(npuRequest)},             // 2 pods × 8 npu (requests only)
				{Name: "cpu", Replicas: 1, Template: newTemplate(v1.ResourceRequirements{})}, // not a worker, excluded
			},
		},
	}

	pp := New(pluginsinterface.PluginClientset{}, []string{"--workers=worker", "--world-size-calculation=GPU_NUMBER"}).(*pytorchPlugin)
	worldSize := pp.getTotalAccelerators(job)
	if worldSize != 18 { // 1×2 + 2×8
		t.Errorf("total accelerators mismatch, expected 18, got %d", worldSize)
	}
	if replicas := pp.getTotalReplicas(job); replicas != 3 {
		t.Errorf("total replicas mismatch, expected 3, got %d", replicas)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pytorch-cards-worker-0",
			Annotations: map[string]string{v1alpha1.TaskSpecKey: "worker"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}
	if err := pp.OnPodCreate(pod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	getEnvValue := func(pod *v1.Pod, name string) string {
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == name {
				return env.Value
			}
		}
		return ""
	}
	if ws := getEnvValue(pod, EnvWorldSize); ws != "18" {
		t.Errorf("WORLD_SIZE mismatch, expected '18' (cards), got '%s'", ws)
	}
	if nn := getEnvValue(pod, EnvSensecoreNNodes); nn != "3" {
		t.Errorf("SENSECORE_PYTORCH_NNODES mismatch, expected '3' (replicas), got '%s'", nn)
	}
	if scws := getEnvValue(pod, EnvSensecoreWorldSize); scws != "18" {
		t.Errorf("SENSECORE_WORLD_SIZE mismatch, expected '18' (cards), got '%s'", scws)
	}

	// a master without accelerators counts 0 cards: 0×1 + 2×8 = 16
	job.Spec.Tasks[0].Template = newTemplate(v1.ResourceRequirements{})
	if worldSize := pp.getTotalAccelerators(job); worldSize != 16 {
		t.Errorf("total accelerators with cpu master mismatch, expected 16, got %d", worldSize)
	}

	// no pod has any accelerator resource: WORLD_SIZE is 0
	job.Spec.Tasks[1].Template = newTemplate(v1.ResourceRequirements{})
	if worldSize := pp.getTotalAccelerators(job); worldSize != 0 {
		t.Errorf("total accelerators without accelerators mismatch, expected 0, got %d", worldSize)
	}
}

// TestPytorchAcceleratorResourcesFlag verifies the accelerator-resources
// argument extends the default resource list (union, deduplicated) so custom
// accelerators are counted into WORLD_SIZE next to the defaults.
func TestPytorchAcceleratorResourcesFlag(t *testing.T) {
	newTemplate := func(resourceName v1.ResourceName, quantity string) v1.PodTemplateSpec {
		return v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{resourceName: resource.MustParse(quantity)},
					},
				}},
			},
		}
	}
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-accel-flag"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				// default resource: 1 pod × 8 mlu
				{Name: "master", Replicas: 1, Template: newTemplate("cambricon.com/mlu", "8")},
				// custom resource via flag: 1 pod × 4 xpu
				{Name: "worker", Replicas: 1, Template: newTemplate("example.com/xpu", "4")},
			},
		},
	}

	// without the flag the custom resource is not counted: 8 mlu + 0 for the
	// accelerator-less worker pod
	pp := New(pluginsinterface.PluginClientset{}, []string{"--workers=worker"}).(*pytorchPlugin)
	if cards := pp.getTotalAccelerators(job); cards != 8 {
		t.Errorf("total accelerators without flag mismatch, expected 8, got %d", cards)
	}

	// with the flag both count: 8 mlu + 4 xpu = 12
	pp = New(pluginsinterface.PluginClientset{}, []string{"--workers=worker", "--accelerator-resources=example.com/xpu, example.com/xpu"}).(*pytorchPlugin)
	if cards := pp.getTotalAccelerators(job); cards != 12 {
		t.Errorf("total accelerators with flag mismatch, expected 12, got %d", cards)
	}

	// defaults keep working beside the flag: nvidia gpu counted
	job.Spec.Tasks[1].Template = newTemplate("nvidia.com/gpu", "2")
	if cards := pp.getTotalAccelerators(job); cards != 10 {
		t.Errorf("total accelerators with flag and default resource mismatch, expected 10, got %d", cards)
	}
}

// TestPytorchAcceleratorsPerPod verifies getAcceleratorsPerPod returns the raw
// card count of a pod template: pods without accelerator resources return 0
// instead of being counted as 1 card, so they never inflate WORLD_SIZE.
func TestPytorchAcceleratorsPerPod(t *testing.T) {
	newTemplate := func(res v1.ResourceRequirements) v1.PodTemplateSpec {
		return v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "main", Resources: res}},
			},
		}
	}
	cpu := v1.ResourceRequirements{}
	gpuLimit := v1.ResourceRequirements{
		Limits: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
	}
	npuRequest := v1.ResourceRequirements{
		Requests: v1.ResourceList{"huawei.com/Ascend910": resource.MustParse("8")},
	}

	pp := New(pluginsinterface.PluginClientset{}, []string{"--workers=worker"}).(*pytorchPlugin)
	if cards := pp.getAcceleratorsPerPod(newTemplate(cpu)); cards != 0 {
		t.Errorf("cpu-only pod should count 0 accelerators, got %d", cards)
	}
	if cards := pp.getAcceleratorsPerPod(newTemplate(gpuLimit)); cards != 2 {
		t.Errorf("gpu pod should count 2 accelerators, got %d", cards)
	}
	if cards := pp.getAcceleratorsPerPod(newTemplate(npuRequest)); cards != 8 {
		t.Errorf("npu pod should count 8 accelerators, got %d", cards)
	}

	// a cpu-only master next to gpu workers no longer adds a phantom card
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-per-pod"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{Name: "master", Replicas: 1, Template: newTemplate(cpu)},
				{Name: "worker", Replicas: 2, Template: newTemplate(gpuLimit)},
			},
		},
	}
	if cards := pp.getTotalAccelerators(job); cards != 4 { // 0×1 + 2×2
		t.Errorf("total accelerators mismatch, expected 4, got %d", cards)
	}

	// a job where no pod has accelerators falls back to the replicas
	job.Spec.Tasks[1].Template = newTemplate(cpu)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pytorch-per-pod-worker-0",
			Annotations: map[string]string{v1alpha1.TaskSpecKey: "worker"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}
	if err := pp.OnPodCreate(pod, job); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	worldSize := ""
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == EnvWorldSize {
			worldSize = env.Value
		}
	}
	if worldSize != "3" {
		t.Errorf("WORLD_SIZE mismatch, expected '3' (replicas fallback), got '%s'", worldSize)
	}
}

// TestPytorchWorldSizeCalculation verifies the world-size-calculation argument
// selects how WORLD_SIZE is computed: NODES_NUMBER (the default) mirrors
// SENSECORE_PYTORCH_NNODES (master plus worker replicas), GPU_NUMBER counts
// the total accelerator cards, and unknown values fall back to NODES_NUMBER.
func TestPytorchWorldSizeCalculation(t *testing.T) {
	newTemplate := func(res v1.ResourceRequirements) v1.PodTemplateSpec {
		return v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "main", Resources: res}},
			},
		}
	}
	cpu := v1.ResourceRequirements{}
	gpu := v1.ResourceRequirements{
		Limits: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
	}

	// 3 pods × 2 gpu = 6 cards, 3 replicas, task "cpu" excluded from both
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-ws-mode"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{Name: "master", Replicas: 1, Template: newTemplate(gpu)},
				{Name: "worker", Replicas: 2, Template: newTemplate(gpu)},
				{Name: "cpu", Replicas: 1, Template: newTemplate(cpu)},
			},
		},
	}

	cases := []struct {
		name     string
		args     []string
		expected int64
	}{
		{"default counts replicas like nnodes", []string{"--workers=worker"}, 3},
		{"GPU_NUMBER counts accelerator cards", []string{"--workers=worker", "--world-size-calculation=GPU_NUMBER"}, 6},
		{"NODES_NUMBER counts replicas like nnodes", []string{"--workers=worker", "--world-size-calculation=NODES_NUMBER"}, 3},
		{"unknown value falls back to replicas", []string{"--workers=worker", "--world-size-calculation=FOO"}, 3},
	}
	for _, tc := range cases {
		pp := New(pluginsinterface.PluginClientset{}, tc.args).(*pytorchPlugin)
		if ws := pp.getWorldSize(job); ws != tc.expected {
			t.Errorf("%s: WORLD_SIZE mismatch, expected %d, got %d", tc.name, tc.expected, ws)
		}
		if replicas := pp.getTotalReplicas(job); int64(replicas) != 3 {
			t.Errorf("%s: SENSECORE_PYTORCH_NNODES mismatch, expected 3 replicas, got %d", tc.name, replicas)
		}
	}

	// a cpu-only job opting into GPU_NUMBER falls back to the replicas too,
	// keeping the legacy WORLD_SIZE instead of counting 0 cards
	cpuOnlyFallback := New(pluginsinterface.PluginClientset{}, []string{"--world-size-calculation=GPU_NUMBER"}).(*pytorchPlugin)

	// a cpu-only job with NODES_NUMBER keeps WORLD_SIZE equal to the replicas
	cpuJob := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pytorch-ws-mode-cpu"},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{Name: "master", Replicas: 1, Template: newTemplate(cpu)},
				{Name: "worker", Replicas: 3, Template: newTemplate(cpu)},
			},
		},
	}
	// 1 master + 3 workers, no accelerator: replicas based fallback
	if ws := cpuOnlyFallback.getWorldSize(cpuJob); ws != 4 {
		t.Errorf("cpu-only job with GPU_NUMBER mismatch, expected WORLD_SIZE 4 (replicas), got %d", ws)
	}
	pp := New(pluginsinterface.PluginClientset{}, []string{"--world-size-calculation=NODES_NUMBER"}).(*pytorchPlugin)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pytorch-ws-mode-cpu-worker-0",
			Annotations: map[string]string{v1alpha1.TaskSpecKey: "worker"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}
	if err := pp.OnPodCreate(pod, cpuJob); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	getEnvValue := func(pod *v1.Pod, name string) string {
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == name {
				return env.Value
			}
		}
		return ""
	}
	if ws := getEnvValue(pod, EnvWorldSize); ws != "4" {
		t.Errorf("WORLD_SIZE mismatch, expected '4' (replicas), got '%s'", ws)
	}
	if nn := getEnvValue(pod, EnvSensecoreNNodes); nn != "4" {
		t.Errorf("SENSECORE_PYTORCH_NNODES mismatch, expected '4' (replicas), got '%s'", nn)
	}
}
