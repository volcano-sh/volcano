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
