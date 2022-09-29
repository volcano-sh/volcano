package pytorch

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestPytorch(t *testing.T) {
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
					Name:  "RANK",
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
					Name:  "RANK",
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
					Name:  "RANK",
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
					Name:  "RANK",
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
					Name:  "RANK",
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

			if !reflect.DeepEqual(testcase.Pod.Spec.Containers[0].Env, testcase.envs) {
				t.Errorf("Case %d (%s): wrong envs, got %v, expected %v", index, testcase.Name, testcase.Pod.Spec.Containers[0].Env, testcase.envs)
			}
		})
	}
}
