/*
Copyright 2025 The Volcano Authors.

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

package ray

import (
	"context"
	"fmt"
	"slices"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/endpointslice"
	coredefaults "k8s.io/kubernetes/pkg/apis/core/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestRayPlugin(t *testing.T) {
	plugins := make(map[string][]string)
	plugins[RayPluginName] = []string{"--port=1234", "--dashboardPort=3456", "--clientPort=4567", "--head=test-head"}

	testcases := []struct {
		Name          string
		Job           *v1alpha1.Job
		Pod           *v1.Pod
		port          int
		dashboardPort int
		clientPort    int
		headName      string
		workerName    string
	}{
		{
			Name: "ray cluster test with only head task",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "ray-cluster-test"},
				Status: v1alpha1.JobStatus{
					ControlledResources: map[string]string{},
				},
				Spec: v1alpha1.JobSpec{
					Plugins: plugins,
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "test-head",
							Replicas: 1,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ray-cluster-test-head-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "test-head",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "head",
						},
					},
				},
			},
			port:          1234,
			dashboardPort: 3456,
			clientPort:    4567,
			headName:      "test-head",
			workerName:    DefaultWorker,
		},
		{
			Name: "ray cluster test with worker task",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "ray-cluster-worker"},
				Status: v1alpha1.JobStatus{
					ControlledResources: map[string]string{},
				},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "head",
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
					Name: "ray-cluster-test-worker-0",
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "worker",
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
			port:          DefaultPort,
			dashboardPort: DefaultDashboardPort,
			clientPort:    DefaultClientPort,
			headName:      DefaultHead,
			workerName:    DefaultWorker,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			rp := New(pluginsinterface.PluginClientset{KubeClients: fakeClient}, testcase.Job.Spec.Plugins[RayPluginName])
			if err := rp.OnPodCreate(testcase.Pod, testcase.Job); err != nil {
				t.Errorf("Case %d (%s): expect no error, but got error %v", i, testcase.Name, err)
			}
			if testcase.Pod.ObjectMeta.Annotations["volcano.sh/task-spec"] == testcase.headName {
				// This sentence checks if the head task pod command is set.
				if testcase.Pod.Spec.Containers[0].Command == nil || !slices.Equal(testcase.Pod.Spec.Containers[0].Command, []string{"sh", "-c", fmt.Sprintf("ray start --head --block --dashboard-host=0.0.0.0 --port=%d --dashboard-port=%d --ray-client-server-port=%d", testcase.port, testcase.dashboardPort, testcase.clientPort)}) {
					t.Errorf("Case %d (%s): wrong head container command, got %s", i, testcase.Name, testcase.Pod.Spec.Containers[0].Command)
				}
				// This if-else statement checks whether the head container in the pod has the correct port configured.
				if testcase.Pod.Spec.Containers[0].Ports == nil {
					t.Errorf("Case %d (%s): no container port in a ray head task", i, testcase.Name)
				} else {
					targets := map[int32]struct{}{
						int32(testcase.port):          {},
						int32(testcase.dashboardPort): {},
						int32(testcase.clientPort):    {},
					}

					portCnt := 0
					for _, p := range testcase.Pod.Spec.Containers[0].Ports {
						if _, ok := targets[p.ContainerPort]; ok {
							portCnt++
						}
					}

					if portCnt != len(targets) {
						t.Errorf("Case %d (%s): not enough container port, got %d, expected %d", i, testcase.Name, portCnt, len(targets))
					}
				}
			}
			if testcase.Pod.ObjectMeta.Annotations["volcano.sh/task-spec"] == "worker" {
				jobName := testcase.Job.ObjectMeta.Name
				if testcase.Pod.Spec.Containers[0].Command == nil || !slices.Equal(testcase.Pod.Spec.Containers[0].Command, []string{"sh", "-c", fmt.Sprintf("ray start --block --address=%s-%s-0.%s:%d", jobName, testcase.headName, jobName, testcase.port)}) {
					t.Errorf("Case %d (%s): wrong worker container command, got %s", i, testcase.Name, testcase.Pod.Spec.Containers[0].Command)
				}
			}

			if err := rp.OnJobAdd(testcase.Job); err != nil {
				t.Fatalf("OnJobAdd failed: %v", err)
			}

			headServiceName := testcase.Job.Name + "-head-svc"
			svc, createErr := fakeClient.CoreV1().Services(testcase.Job.Namespace).Get(context.TODO(), headServiceName, metav1.GetOptions{})
			if createErr != nil {
				t.Fatalf("Service not found: %v", createErr)
			}

			if len(svc.Spec.Ports) != 3 {
				t.Errorf("expected 3 ports, got %d", len(svc.Spec.Ports))
			}
			if testcase.Job.Status.ControlledResources["plugin-"+rp.Name()] != rp.Name() {
				t.Errorf("ControlledResources not updated: %v", testcase.Job.Status.ControlledResources)
			}

			if err := rp.OnJobDelete(testcase.Job); err != nil {
				t.Fatalf("OnJobDelete failed: %v", err)
			}
			_, deleteErr := fakeClient.CoreV1().Services(testcase.Job.Namespace).Get(context.TODO(), headServiceName, metav1.GetOptions{})
			if deleteErr == nil {
				t.Errorf("expected service to be deleted")
			}
			if _, ok := testcase.Job.Status.ControlledResources["plugin-"+rp.Name()]; ok {
				t.Errorf("expected ControlledResources entry to be deleted")
			}
		})
	}
}

// A Service must forward to the listener configured by the plugin even when
// the user has already declared that listener under another name.
func TestRayHeadServicePorts(t *testing.T) {
	tests := []struct {
		name      string
		portNames []string
		arguments []string
		ports     []int32
	}{
		{name: "generated ports", ports: []int32{6379, 8265, 10001}},
		{name: "canonical port names", portNames: []string{"gcs", "dashboard", "client-server"}, ports: []int32{6379, 8265, 10001}},
		{name: "custom port names", portNames: []string{"redis", "web", "client"}, ports: []int32{6379, 8265, 10001}},
		{name: "unnamed ports", portNames: []string{"", "", ""}, ports: []int32{6379, 8265, 10001}},
		{name: "mixed port names", portNames: []string{"gcs", "web", ""}, ports: []int32{6379, 8265, 10001}},
		{name: "configured ports", portNames: []string{"redis", "web", "client"}, arguments: []string{"--port=16379", "--dashboardPort=18265", "--clientPort=20001"}, ports: []int32{16379, 18265, 20001}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			plugin := New(pluginsinterface.PluginClientset{KubeClients: client}, tt.arguments)
			job := &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "ray-training", Namespace: "training", UID: "ray-training"},
				Spec:       v1alpha1.JobSpec{Tasks: []v1alpha1.TaskSpec{{Name: "head", Replicas: 1}}},
				Status:     v1alpha1.JobStatus{ControlledResources: map[string]string{}},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "ray-training-head-0", Namespace: job.Namespace, Annotations: map[string]string{v1alpha1.TaskSpecKey: "head"}},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "head", Image: "rayproject/ray:latest"}}},
			}
			for i, name := range tt.portNames {
				pod.Spec.Containers[0].Ports = append(pod.Spec.Containers[0].Ports, v1.ContainerPort{Name: name, ContainerPort: tt.ports[i], Protocol: v1.ProtocolTCP})
			}
			originalPorts := slices.Clone(pod.Spec.Containers[0].Ports)
			if err := plugin.OnPodCreate(pod, job); err != nil {
				t.Fatal(err)
			}
			if tt.portNames != nil && !slices.Equal(originalPorts, pod.Spec.Containers[0].Ports) {
				t.Errorf("existing listener declarations changed: got %v, want %v", pod.Spec.Containers[0].Ports, originalPorts)
			}
			if err := plugin.OnJobAdd(job); err != nil {
				t.Fatal(err)
			}
			service, err := client.CoreV1().Services(job.Namespace).Get(context.Background(), job.Name+"-head-svc", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(service.Spec.Ports) != len(tt.ports) {
				t.Fatalf("got %d Service ports, want %d", len(service.Spec.Ports), len(tt.ports))
			}
			// Apply the same Pod defaults as the API server before resolving
			// the generated Service with the EndpointSlice controller's resolver.
			coredefaults.SetObjectDefaults_Pod(pod)
			for i, port := range service.Spec.Ports {
				resolved, err := endpointslice.FindPort(pod, &port)
				if err != nil {
					t.Errorf("Service port %s cannot resolve a listener: %v", port.Name, err)
					continue
				}
				if port.Port != tt.ports[i] || resolved != int(tt.ports[i]) {
					t.Errorf("Service port %s resolves %d -> %d, want %d -> %d", port.Name, port.Port, resolved, tt.ports[i], tt.ports[i])
				}
			}
		})
	}
}
