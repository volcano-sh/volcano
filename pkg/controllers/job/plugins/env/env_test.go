/*
Copyright 2024 The Volcano Authors.

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

package env

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestENVPlugin(t *testing.T) {
	var (
		job = &v1alpha1.Job{
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
			Status: v1alpha1.JobStatus{
				ControlledResources: map[string]string{},
			},
		}
		pod = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pytorch-worker-0",
			},
			Spec: v1.PodSpec{
				InitContainers: []v1.Container{
					{
						Name: "worker",
					},
				},
				Containers: []v1.Container{
					{
						Name: "worker",
					},
				},
			},
		}
	)
	tests := []struct {
		name            string
		pluginArguments []string
		wantErr         error
	}{
		{
			name:            "no params specified",
			pluginArguments: []string{"test1", "test2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pluginInterface := New(pluginsinterface.PluginClientset{KubeClients: fake.NewSimpleClientset(pod)}, test.pluginArguments)
			plugin := pluginInterface.(*envPlugin)

			gotErr := plugin.OnPodCreate(pod, job)
			if !equality.Semantic.DeepEqual(gotErr, test.wantErr) {
				t.Fatalf("OnPodCreate error = %v, wantErr %v", gotErr, test.wantErr)
			}
			gotErr = plugin.OnJobAdd(job)
			if !equality.Semantic.DeepEqual(gotErr, test.wantErr) {
				t.Fatalf("OnJobAdd error = %v, wantErr %v", gotErr, test.wantErr)
			}
			gotErr = plugin.OnJobUpdate(job)
			if !equality.Semantic.DeepEqual(gotErr, test.wantErr) {
				t.Fatalf("OnJobUpdate error = %v, wantErr %v", gotErr, test.wantErr)
			}
			gotErr = plugin.OnJobDelete(job)
			if !equality.Semantic.DeepEqual(gotErr, test.wantErr) {
				t.Fatalf("OnJobDelete error = %v, wantErr %v", gotErr, test.wantErr)
			}
		})
	}
}
