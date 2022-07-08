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
	controllerMpi "volcano.sh/volcano/pkg/controllers/job/plugins/distributed-framework/mpi"
)

func TestMpi(t *testing.T) {
	workerName := "fakeWorker"
	masterName := "fakeMaster"
	plugins := make(map[string][]string)
	plugins[controllerMpi.MPIPluginName] = []string{"--master=" + masterName, "--worker=" + workerName}
	testcases := []struct {
		Name string
		Job  *v1alpha1.Job
	}{
		{
			Name: "add dependsOn",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-0"},
				Spec: v1alpha1.JobSpec{
					Plugins: plugins,
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     masterName,
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     workerName,
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
		},
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			AddDependsOn(testcase.Job)
			if testcase.Job.Spec.Tasks[0].DependsOn == nil || len(testcase.Job.Spec.Tasks[0].DependsOn.Name) == 0 {
				t.Errorf("Case %d (%s): no dependencies added ", index, testcase.Name)
				return
			}
			if testcase.Job.Spec.Tasks[0].DependsOn.Name[0] != workerName {
				t.Errorf("Case %d (%s): Dependency add error expect %s, but got %s", index, testcase.Name, workerName, testcase.Job.Spec.Tasks[0].DependsOn.Name[0])
			}
		})
	}
}
