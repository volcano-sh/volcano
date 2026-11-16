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

// TestAddDependsOnCompatibility covers DependsOn generation for single worker
// (legacy behavior) and multi worker lists.
func TestAddDependsOnCompatibility(t *testing.T) {
	newJob := func(args []string, withMaster bool, existingDependsOn *v1alpha1.DependsOn) *v1alpha1.Job {
		tasks := []v1alpha1.TaskSpec{}
		if withMaster {
			master := v1alpha1.TaskSpec{Name: "master", Replicas: 1, Template: v1.PodTemplateSpec{}}
			master.DependsOn = existingDependsOn
			tasks = append(tasks, master)
		}
		tasks = append(tasks,
			v1alpha1.TaskSpec{Name: "worker", Replicas: 2, Template: v1.PodTemplateSpec{}},
			v1alpha1.TaskSpec{Name: "worker-0", Replicas: 2, Template: v1.PodTemplateSpec{}},
		)
		return &v1alpha1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mpi-depends-on"},
			Spec: v1alpha1.JobSpec{
				Plugins: map[string][]string{controllerMpi.MPIPluginName: args},
				Tasks:   tasks,
			},
		}
	}

	testcases := []struct {
		name            string
		job             *v1alpha1.Job
		expectNil       bool
		expectedDepends []string
		existingKept    bool
	}{
		{
			name:            "legacy single worker name",
			job:             newJob([]string{"--worker=worker"}, true, nil),
			expectedDepends: []string{"worker"},
		},
		{
			name:            "default worker flag value",
			job:             newJob(nil, true, nil),
			expectedDepends: []string{"worker"},
		},
		{
			name:            "multi worker names keep argument order",
			job:             newJob([]string{"--worker=worker-0,worker"}, true, nil),
			expectedDepends: []string{"worker-0", "worker"},
		},
		{
			name:            "nonexistent worker names are filtered",
			job:             newJob([]string{"--worker=ghost,worker,ghost-1"}, true, nil),
			expectedDepends: []string{"worker"},
		},
		{
			name:         "existing dependsOn is untouched",
			job:          newJob([]string{"--worker=worker"}, true, &v1alpha1.DependsOn{Name: []string{"custom-task"}}),
			existingKept: true,
		},
		{
			name:      "no master task, nothing set",
			job:       newJob([]string{"--worker=worker"}, false, nil),
			expectNil: true,
		},
	}

	for index, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			AddDependsOn(tc.job)

			var dependsOn *v1alpha1.DependsOn
			if len(tc.job.Spec.Tasks) > 0 {
				dependsOn = tc.job.Spec.Tasks[0].DependsOn
			}

			if tc.existingKept {
				if dependsOn == nil || len(dependsOn.Name) != 1 || dependsOn.Name[0] != "custom-task" {
					t.Errorf("Case %d (%s): existing DependsOn was modified, got %v", index, tc.name, dependsOn)
				}
				return
			}
			if tc.expectNil {
				if dependsOn != nil {
					t.Errorf("Case %d (%s): expected no DependsOn, but got %v", index, tc.name, dependsOn)
				}
				return
			}
			if dependsOn == nil {
				t.Fatalf("Case %d (%s): expected DependsOn to be set", index, tc.name)
			}
			if len(dependsOn.Name) != len(tc.expectedDepends) {
				t.Fatalf("Case %d (%s): expected DependsOn %v, but got %v", index, tc.name, tc.expectedDepends, dependsOn.Name)
			}
			for i := range tc.expectedDepends {
				if dependsOn.Name[i] != tc.expectedDepends[i] {
					t.Errorf("Case %d (%s): expected DependsOn %v, but got %v", index, tc.name, tc.expectedDepends, dependsOn.Name)
					break
				}
			}
		})
	}
}
