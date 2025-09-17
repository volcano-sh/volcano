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

package mpi

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestMpi(t *testing.T) {
	plugins := make(map[string][]string)
	plugins[MPIPluginName] = []string{"--port=5000"}
	testjob5000 := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-1"},
		Spec: v1alpha1.JobSpec{
			Plugins: plugins,
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:     "fakeMaster",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "fakeWorker",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}

	testcases := []struct {
		Name string
		Job  *v1alpha1.Job
		Pod  *v1.Pod
		port int
	}{
		{
			Name: "add port",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-0"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "fakeMaster",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "fakeWorker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mpi-fakeMaster-0",
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
		},
		{
			Name: "add 5000 port",
			Job:  testjob5000,
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mpi-fakeMaster-0",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "master",
						},
					},
				},
			},
			port: 5000,
		},
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			mp := New(pluginsinterface.PluginClientset{}, testcase.Job.Spec.Plugins[MPIPluginName])
			if err := mp.OnPodCreate(testcase.Pod, testcase.Job); err != nil {
				t.Errorf("Case %d (%s): expect no error, but got error %v", index, testcase.Name, err)
			}
			if testcase.Pod.Spec.Containers[0].Ports == nil || testcase.Pod.Spec.Containers[0].Ports[0].ContainerPort != int32(testcase.port) {
				t.Errorf("Case %d (%s): wrong port, got %d", index, testcase.Name, testcase.Pod.Spec.Containers[0].Ports[0].ContainerPort)
			}
		})
	}
}

func TestMpiWithEmptyWorker(t *testing.T) {
	testcases := []struct {
		Name           string
		Job            *v1alpha1.Job
		Pod            *v1.Pod
		ExpectedEnvVar v1.EnvVar
		IsMaster       bool
	}{
		{
			Name: "master pod with no worker task - should not panic",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-no-worker"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						// No worker task defined
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mpi-no-worker-master-0",
					Annotations: map[string]string{
						"volcano.sh/task-spec": "master",
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
			ExpectedEnvVar: v1.EnvVar{},
			IsMaster:       true,
		},
		{
			Name: "master pod with worker task having 0 replicas",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-zero-worker"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "master",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "worker",
							Replicas: 0, // Zero replicas
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mpi-zero-worker-master-0",
					Annotations: map[string]string{
						"volcano.sh/task-spec": "master",
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
			ExpectedEnvVar: v1.EnvVar{},
			IsMaster:       true,
		},
		{
			Name: "master pod with valid worker task",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-valid-worker"},
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
					Name: "test-mpi-valid-worker-master-0",
					Annotations: map[string]string{
						"volcano.sh/task-spec": "master",
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
			ExpectedEnvVar: v1.EnvVar{
				Name:  MPIHost,
				Value: "test-mpi-valid-worker-worker-0.test-mpi-valid-worker,test-mpi-valid-worker-worker-1.test-mpi-valid-worker",
			},
			IsMaster: true,
		},
		{
			Name: "worker pod should not have MPI_HOST env var",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-worker-pod"},
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
					Name: "test-mpi-worker-pod-worker-0",
					Annotations: map[string]string{
						"volcano.sh/task-spec": "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "worker",
						},
					},
				},
			},
			ExpectedEnvVar: v1.EnvVar{}, // Should be empty for worker pods
			IsMaster:       false,
		},
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			mp := New(pluginsinterface.PluginClientset{}, []string{})

			// Test that OnPodCreate doesn't panic
			if err := mp.OnPodCreate(testcase.Pod, testcase.Job); err != nil {
				t.Errorf("Case %d (%s): expect no error, but got error %v", index, testcase.Name, err)
			}

			// Check if master pod has the correct MPI_HOST environment variable
			if testcase.IsMaster {
				// Check init containers
				checkMPIHostEnvVar(t, index, testcase.Name, "InitContainer", testcase.Pod.Spec.InitContainers, testcase.ExpectedEnvVar)

				// Check regular containers
				checkMPIHostEnvVar(t, index, testcase.Name, "Container", testcase.Pod.Spec.Containers, testcase.ExpectedEnvVar)
			} else {
				// For worker pods, ensure no MPI_HOST environment variable is set
				checkNoMPIHostEnvVar(t, index, testcase.Name, testcase.Pod.Spec.Containers)
			}
		})
	}
}

// checkMPIHostEnvVar checks if containers have the expected MPI_HOST environment variable
func checkMPIHostEnvVar(t *testing.T, index int, testName, containerType string, containers []v1.Container, expectedEnvVar v1.EnvVar) {
	for _, c := range containers {
		found := false
		for _, env := range c.Env {
			if env.Name == MPIHost {
				found = true
				if env.Value != expectedEnvVar.Value {
					t.Errorf("Case %d (%s): %s MPI_HOST value mismatch, expected '%s', got '%s'",
						index, testName, containerType, expectedEnvVar.Value, env.Value)
				}
				break
			}
		}
		if !found && expectedEnvVar.Name != "" {
			t.Errorf("Case %d (%s): %s missing MPI_HOST environment variable", index, testName, containerType)
		}
	}
}

// checkNoMPIHostEnvVar ensures that containers do not have MPI_HOST environment variable
func checkNoMPIHostEnvVar(t *testing.T, index int, testName string, containers []v1.Container) {
	for _, c := range containers {
		for _, env := range c.Env {
			if env.Name == MPIHost {
				t.Errorf("Case %d (%s): Worker pod should not have MPI_HOST environment variable, but found: %s",
					index, testName, env.Value)
			}
		}
	}
}
