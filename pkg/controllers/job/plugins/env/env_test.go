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

// controlledKey marks a job whose resources were set up by the env plugin.
const controlledKey = "plugin-env"

func newTestPlugin(t *testing.T) *envPlugin {
	t.Helper()
	plugin, ok := New(pluginsinterface.PluginClientset{}, nil).(*envPlugin)
	require.True(t, ok, "New should return an *envPlugin")
	return plugin
}

func newTestJob(controlled map[string]string) *batch.Job {
	return &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "test-ns"},
		Status:     batch.JobStatus{ControlledResources: controlled},
	}
}

func indexEnv(index string) []v1.EnvVar {
	return []v1.EnvVar{
		{Name: TaskVkIndex, Value: index},
		{Name: TaskIndex, Value: index},
	}
}

func TestName(t *testing.T) {
	assert.Equal(t, "env", newTestPlugin(t).Name())
}

func TestOnPodCreate(t *testing.T) {
	existingEnv := v1.EnvVar{Name: "EXISTING", Value: "value"}

	tests := []struct {
		name      string
		podName   string
		podSpec   v1.PodSpec
		wantIndex string
	}{
		{
			name:      "injects the index into a single container",
			podName:   "test-job-worker-3",
			podSpec:   v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
			wantIndex: "3",
		},
		{
			name:    "injects the index into every container and init container",
			podName: "test-job-worker-12",
			podSpec: v1.PodSpec{
				InitContainers: []v1.Container{{Name: "init-a"}, {Name: "init-b"}},
				Containers:     []v1.Container{{Name: "main"}, {Name: "sidecar"}},
			},
			wantIndex: "12",
		},
		{
			name:    "keeps existing env vars and appends the index after them",
			podName: "test-job-worker-0",
			podSpec: v1.PodSpec{
				InitContainers: []v1.Container{{Name: "init", Env: []v1.EnvVar{existingEnv}}},
				Containers:     []v1.Container{{Name: "main", Env: []v1.EnvVar{existingEnv}}},
			},
			wantIndex: "0",
		},
		{
			name:      "task name containing dashes uses the last segment",
			podName:   "test-job-param-server-1",
			podSpec:   v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
			wantIndex: "1",
		},
		{
			name:      "pod name without an index suffix injects an empty index",
			podName:   "standalone",
			podSpec:   v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
			wantIndex: "",
		},
		{
			name:    "pod without containers is left unchanged",
			podName: "test-job-worker-0",
			podSpec: v1.PodSpec{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := newTestPlugin(t)
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: tt.podName, Namespace: "test-ns"},
				Spec:       *tt.podSpec.DeepCopy(),
			}

			require.NoError(t, plugin.OnPodCreate(pod, newTestJob(map[string]string{})))

			require.Len(t, pod.Spec.InitContainers, len(tt.podSpec.InitContainers))
			require.Len(t, pod.Spec.Containers, len(tt.podSpec.Containers))
			check := func(original, updated []v1.Container) {
				for i, c := range updated {
					want := append(append([]v1.EnvVar{}, original[i].Env...), indexEnv(tt.wantIndex)...)
					assert.Equal(t, want, c.Env, "env of container %q", c.Name)
				}
			}
			check(tt.podSpec.InitContainers, pod.Spec.InitContainers)
			check(tt.podSpec.Containers, pod.Spec.Containers)
		})
	}
}

func TestOnJobAdd(t *testing.T) {
	tests := []struct {
		name       string
		controlled map[string]string
		want       map[string]string
	}{
		{
			name:       "marks a new job as controlled",
			controlled: map[string]string{},
			want:       map[string]string{controlledKey: "env"},
		},
		{
			name:       "keeps entries owned by other plugins",
			controlled: map[string]string{"plugin-svc": "svc"},
			want:       map[string]string{"plugin-svc": "svc", controlledKey: "env"},
		},
		{
			name:       "is a no-op for a job already handled by the plugin",
			controlled: map[string]string{controlledKey: "env"},
			want:       map[string]string{controlledKey: "env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := newTestJob(tt.controlled)

			require.NoError(t, newTestPlugin(t).OnJobAdd(job))

			assert.Equal(t, tt.want, job.Status.ControlledResources)
		})
	}
}

func TestOnJobAddTwice(t *testing.T) {
	plugin := newTestPlugin(t)
	job := newTestJob(map[string]string{})

	require.NoError(t, plugin.OnJobAdd(job))
	require.NoError(t, plugin.OnJobAdd(job))

	assert.Equal(t, map[string]string{controlledKey: "env"}, job.Status.ControlledResources)
}

func TestOnJobDelete(t *testing.T) {
	tests := []struct {
		name       string
		controlled map[string]string
		want       map[string]string
	}{
		{
			name:       "removes the entry owned by the plugin",
			controlled: map[string]string{controlledKey: "env", "plugin-svc": "svc"},
			want:       map[string]string{"plugin-svc": "svc"},
		},
		{
			name:       "is a no-op for a job not handled by the plugin",
			controlled: map[string]string{"plugin-svc": "svc"},
			want:       map[string]string{"plugin-svc": "svc"},
		},
		{
			name:       "keeps the key when another value is stored under it",
			controlled: map[string]string{controlledKey: "other"},
			want:       map[string]string{controlledKey: "other"},
		},
		{
			name:       "nil controlled resources is a no-op",
			controlled: nil,
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := newTestJob(tt.controlled)

			require.NoError(t, newTestPlugin(t).OnJobDelete(job))

			assert.Equal(t, tt.want, job.Status.ControlledResources)
		})
	}
}

func TestOnJobAddThenDelete(t *testing.T) {
	plugin := newTestPlugin(t)
	job := newTestJob(map[string]string{})

	require.NoError(t, plugin.OnJobAdd(job))
	require.NoError(t, plugin.OnJobDelete(job))

	assert.Empty(t, job.Status.ControlledResources)
}

func TestOnJobUpdate(t *testing.T) {
	job := newTestJob(map[string]string{controlledKey: "env"})

	require.NoError(t, newTestPlugin(t).OnJobUpdate(job))

	assert.Equal(t, map[string]string{controlledKey: "env"}, job.Status.ControlledResources)
}
