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

package sharding

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestCalculateMetricsFromPodsAndNode(t *testing.T) {
	container := func(name, cpu, memory string) corev1.Container {
		return corev1.Container{Name: name, Image: "busybox:1.36", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory),
		}}}
	}
	sidecar := container("sidecar", "500m", "256Mi")
	sidecar.RestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)
	tests := []struct {
		name        string
		spec        corev1.PodSpec
		cpu, memory float64
	}{
		{
			name: "ordinary containers",
			spec: corev1.PodSpec{Containers: []corev1.Container{
				container("app", "1", "1Gi"), container("helper", "500m", "512Mi"),
			}},
			cpu: 0.375, memory: 1.5 / 32,
		},
		{
			name: "init maxima are per resource",
			spec: corev1.PodSpec{
				Containers: []corev1.Container{container("app", "500m", "512Mi")},
				InitContainers: []corev1.Container{
					container("cpu-init", "2", "256Mi"), container("memory-init", "100m", "2Gi"),
				},
			},
			cpu: 0.5, memory: 2.0 / 32,
		},
		{
			name: "restartable sidecar overlaps later init",
			spec: corev1.PodSpec{
				Containers:     []corev1.Container{container("app", "1", "1Gi")},
				InitContainers: []corev1.Container{sidecar, container("init", "2", "2Gi")},
			},
			// The init peak is 2 CPU + the 500m sidecar, and 2Gi + 256Mi.
			cpu: 0.625, memory: 2.25 / 32,
		},
		{
			name: "admitted runtime overhead",
			spec: corev1.PodSpec{
				Containers:       []corev1.Container{container("app", "1", "1Gi")},
				RuntimeClassName: ptr.To("sandbox"),
				Overhead: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
			cpu: 0.3125, memory: 1.125 / 32,
		},
		{
			name: "pod level requests",
			spec: corev1.PodSpec{
				Containers: []corev1.Container{container("app", "500m", "512Mi")},
				Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				}},
			},
			cpu: 0.75, memory: 4.0 / 32,
		},
		{
			name: "partial pod level requests",
			spec: corev1.PodSpec{
				Containers: []corev1.Container{container("app", "500m", "512Mi")},
				Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("3"),
				}},
			},
			cpu: 0.75, memory: 0.5 / 32,
		},
		{
			name: "capacity clamp",
			spec: corev1.PodSpec{Containers: []corev1.Container{container("app", "8", "64Gi")}},
			cpu:  1, memory: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "default"}, Spec: tt.spec}
			before := pod.DeepCopy()
			controller := &ShardingController{}
			metrics, err := controller.calculateMetricsFromPodsAndNode([]*corev1.Pod{pod}, CreateTestNode("node-1", "4", false, nil))
			require.NoError(t, err)
			assert.InDelta(t, tt.cpu, metrics.CPUUtilization, 0.000001)
			assert.InDelta(t, tt.memory, metrics.MemoryUtilization, 0.000001)
			assert.Equal(t, 1, metrics.PodCount)
			assert.Equal(t, before, pod, "accounting must not mutate informer objects")
		})
	}
}

// Exercise the actual controller startup, informer-backed collection and
// policy pipeline, rather than injecting a hand-written utilization metric.
func TestShardAssignmentUsesInitContainerRequests(t *testing.T) {
	node := CreateTestNode("init-heavy-node", "4", false, nil)
	pod := CreateTestPod("default", "initializing", node.Name, "500m", "512Mi")
	pod.Status.Phase = corev1.PodPending
	pod.Spec.Containers[0].Image = "busybox:1.36"
	pod.Spec.InitContainers = []corev1.Container{{Name: "prepare", Image: "busybox:1.36", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("3"), corev1.ResourceMemory: resource.MustParse("2Gi"),
	}}}}
	testController := newTestShardingController(t, &TestControllerOption{
		InitialObjects: []runtime.Object{node, pod},
		SchedulerConfigs: []SchedulerConfigSpec{
			{Name: "low-load", Type: "volcano", CPUUtilizationMin: 0, CPUUtilizationMax: 0.49, MaxNodes: 10},
			{Name: "high-load", Type: "agent", CPUUtilizationMin: 0.5, CPUUtilizationMax: 1, MaxNodes: 10},
		},
	})
	t.Cleanup(func() { closeTestShardingController(testController) })
	controller := testController.Controller
	WaitForNodeMetricsUpdate(t, controller, node.Name, 5*time.Second)
	metrics := controller.GetAllNodeMetrics()[node.Name]
	require.NotNil(t, metrics)
	assert.InDelta(t, 0.75, metrics.CPUUtilization, 0.000001, "init request is 3 of 4 CPUs")
	assert.Eventually(t, func() bool {
		high, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.Background(), "high-load", metav1.GetOptions{})
		if err != nil {
			return false
		}
		low, err := controller.vcClient.ShardV1alpha1().NodeShards().Get(context.Background(), "low-load", metav1.GetOptions{})
		return err == nil && len(high.Spec.NodesDesired) == 1 && high.Spec.NodesDesired[0] == node.Name && len(low.Spec.NodesDesired) == 0
	}, 5*time.Second, 20*time.Millisecond, "the init-heavy node must go to the high-load shard")
}
