/*
Copyright 2026 The Volcano Authors.

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const controlledResourceKey = "plugin-env"

func TestOnPodCreate(t *testing.T) {
	existingEnv := v1.EnvVar{Name: "EXISTING", Value: "kept"}

	tests := []struct {
		name               string
		podName            string
		containers         []v1.Container
		initContainers     []v1.Container
		wantContainers     [][]v1.EnvVar
		wantInitContainers [][]v1.EnvVar
	}{
		{
			name:       "adds the task index to every container",
			podName:    "job-worker-2",
			containers: []v1.Container{{Name: "main"}, {Name: "sidecar"}},
			wantContainers: [][]v1.EnvVar{
				{{Name: TaskVkIndex, Value: "2"}, {Name: TaskIndex, Value: "2"}},
				{{Name: TaskVkIndex, Value: "2"}, {Name: TaskIndex, Value: "2"}},
			},
		},
		{
			name:           "adds the task index to init containers",
			podName:        "job-worker-0",
			containers:     []v1.Container{{Name: "main"}},
			initContainers: []v1.Container{{Name: "init"}},
			wantContainers: [][]v1.EnvVar{
				{{Name: TaskVkIndex, Value: "0"}, {Name: TaskIndex, Value: "0"}},
			},
			wantInitContainers: [][]v1.EnvVar{
				{{Name: TaskVkIndex, Value: "0"}, {Name: TaskIndex, Value: "0"}},
			},
		},
		{
			name:       "keeps the existing env of a container",
			podName:    "job-worker-1",
			containers: []v1.Container{{Name: "main", Env: []v1.EnvVar{existingEnv}}},
			wantContainers: [][]v1.EnvVar{
				{existingEnv, {Name: TaskVkIndex, Value: "1"}, {Name: TaskIndex, Value: "1"}},
			},
		},
		{
			name:       "sets an empty index when the pod name has no task index",
			podName:    "standalone",
			containers: []v1.Container{{Name: "main"}},
			wantContainers: [][]v1.EnvVar{
				{{Name: TaskVkIndex, Value: ""}, {Name: TaskIndex, Value: ""}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := New(pluginsinterface.PluginClientset{}, nil)
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: tt.podName},
				Spec:       v1.PodSpec{Containers: tt.containers, InitContainers: tt.initContainers},
			}

			require.NoError(t, ep.OnPodCreate(pod, &batch.Job{}))

			require.Len(t, pod.Spec.Containers, len(tt.wantContainers))
			for i, want := range tt.wantContainers {
				assert.Equal(t, want, pod.Spec.Containers[i].Env, "container %s", pod.Spec.Containers[i].Name)
			}
			require.Len(t, pod.Spec.InitContainers, len(tt.wantInitContainers))
			for i, want := range tt.wantInitContainers {
				assert.Equal(t, want, pod.Spec.InitContainers[i].Env, "init container %s", pod.Spec.InitContainers[i].Name)
			}
		})
	}
}

func TestOnJobAdd(t *testing.T) {
	tests := []struct {
		name                string
		controlledResources map[string]string
		want                map[string]string
	}{
		{
			name:                "records the plugin in the controlled resources",
			controlledResources: map[string]string{},
			want:                map[string]string{controlledResourceKey: "env"},
		},
		{
			name:                "keeps the controlled resources when the plugin is already recorded",
			controlledResources: map[string]string{controlledResourceKey: "env", "plugin-svc": "svc"},
			want:                map[string]string{controlledResourceKey: "env", "plugin-svc": "svc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := New(pluginsinterface.PluginClientset{}, nil)
			job := &batch.Job{Status: batch.JobStatus{ControlledResources: tt.controlledResources}}

			require.NoError(t, ep.OnJobAdd(job))
			assert.Equal(t, tt.want, job.Status.ControlledResources)
		})
	}
}

func TestOnJobDelete(t *testing.T) {
	tests := []struct {
		name                string
		controlledResources map[string]string
		want                map[string]string
	}{
		{
			name:                "removes the plugin from the controlled resources",
			controlledResources: map[string]string{controlledResourceKey: "env", "plugin-svc": "svc"},
			want:                map[string]string{"plugin-svc": "svc"},
		},
		{
			name:                "keeps the controlled resources when the plugin is not recorded",
			controlledResources: map[string]string{"plugin-svc": "svc"},
			want:                map[string]string{"plugin-svc": "svc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := New(pluginsinterface.PluginClientset{}, nil)
			job := &batch.Job{Status: batch.JobStatus{ControlledResources: tt.controlledResources}}

			require.NoError(t, ep.OnJobDelete(job))
			assert.Equal(t, tt.want, job.Status.ControlledResources)
		})
	}
}
