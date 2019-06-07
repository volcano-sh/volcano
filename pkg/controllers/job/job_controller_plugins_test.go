/*
Copyright 2019 The Volcano Authors.

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

package job

import (
	"fmt"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeclient "k8s.io/client-go/kubernetes/fake"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	volcanoclient "volcano.sh/volcano/pkg/client/clientset/versioned/fake"

	kubebatchclient "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned/fake"
)

func newFakeController() *Controller {
	KubeBatchClientSet := kubebatchclient.NewSimpleClientset()
	VolcanoClientSet := volcanoclient.NewSimpleClientset()
	KubeClientSet := kubeclient.NewSimpleClientset()

	controller := NewJobController(KubeClientSet, KubeBatchClientSet, VolcanoClientSet)
	return controller
}

func TestPluginOnPodCreate(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name    string
		Job     *vkv1.Job
		Pod     *v1.Pod
		Plugins []string
		RetVal  error
	}{
		{
			Name: "All Plugin",
			Job: &vkv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Job1",
					Namespace: namespace,
				},
			},
			Pod:     buildPod(namespace, "pod1", v1.PodPending, nil),
			Plugins: []string{"env", "svc", "ssh"},
			RetVal:  nil,
		},
		{
			Name: "Wrong Plugin",
			Job: &vkv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Job1",
				},
			},
			Pod:     buildPod(namespace, "pod1", v1.PodPending, nil),
			Plugins: []string{"new"},
			RetVal:  fmt.Errorf("failed to get plugin %s", "new"),
		},
	}

	for i, testcase := range testcases {

		fakeController := newFakeController()
		jobPlugins := make(map[string][]string)

		for _, plugin := range testcase.Plugins {
			jobPlugins[plugin] = make([]string, 0)
		}

		testcase.Job.Spec.Plugins = jobPlugins

		err := fakeController.pluginOnPodCreate(testcase.Job, testcase.Pod)
		if testcase.RetVal != nil && err.Error() != testcase.RetVal.Error() {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.RetVal, err)
		}

		for _, plugin := range testcase.Plugins {
			if plugin == "env" {
				for _, container := range testcase.Pod.Spec.Containers {
					if len(container.Env) == 0 {
						t.Errorf("case %d (%s): expected: Env Length not to be zero", i, testcase.Name)
					}
				}
			}

			if plugin == "svc" {
				for _, container := range testcase.Pod.Spec.Containers {
					if len(container.VolumeMounts) == 0 {
						t.Errorf("case %d (%s): expected: VolumeMount Length not to be zero", i, testcase.Name)
					}
					exist := false
					for _, volume := range container.VolumeMounts {
						if volume.Name == fmt.Sprint(testcase.Job.Name, "-svc") {
							exist = true
						}
					}
					if !exist {
						t.Errorf("case %d (%s): expected: VolumeMount not created", i, testcase.Name)
					}
				}
			}

			if plugin == "ssh" {
				for _, container := range testcase.Pod.Spec.Containers {
					if len(container.VolumeMounts) == 0 {
						t.Errorf("case %d (%s): expected: VolumeMount Length not to be zero", i, testcase.Name)
					}
					exist := false
					for _, volume := range container.VolumeMounts {
						if volume.Name == fmt.Sprint(testcase.Job.Name, "-ssh") {
							exist = true
						}
					}
					if !exist {
						t.Errorf("case %d (%s): expected: VolumeMount not created", i, testcase.Name)
					}
				}
			}
		}
	}
}

func TestPluginOnJobAdd(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name    string
		Job     *vkv1.Job
		Plugins []string
		RetVal  error
	}{
		{
			Name: "Plugins",
			Job: &vkv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			Plugins: []string{"svc", "ssh", "env"},
			RetVal:  nil,
		},
		{
			Name: "Wrong Plugin",
			Job: &vkv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Job1",
				},
			},
			Plugins: []string{"new"},
			RetVal:  fmt.Errorf("failed to get plugin %s", "new"),
		},
	}

	for i, testcase := range testcases {

		fakeController := newFakeController()
		jobPlugins := make(map[string][]string)

		for _, plugin := range testcase.Plugins {
			jobPlugins[plugin] = make([]string, 0)
		}

		testcase.Job.Spec.Plugins = jobPlugins

		err := fakeController.pluginOnJobAdd(testcase.Job)
		if testcase.RetVal != nil && err.Error() != testcase.RetVal.Error() {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.RetVal, err)
		}

		for _, plugin := range testcase.Plugins {

			if plugin == "svc" {
				_, err := fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Get(fmt.Sprint(testcase.Job.Name, "-svc"), metav1.GetOptions{})
				if err != nil {
					t.Errorf("Case %d (%s): expected: ConfigMap to be created, but not created because of error %s", i, testcase.Name, err.Error())
				}

				_, err = fakeController.kubeClients.CoreV1().Services(namespace).Get(testcase.Job.Name, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Case %d (%s): expected: Service to be created, but not created because of error %s", i, testcase.Name, err.Error())
				}
			}

			if plugin == "ssh" {
				_, err := fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Get(fmt.Sprint(testcase.Job.Name, "-ssh"), metav1.GetOptions{})
				if err != nil {
					t.Errorf("Case %d (%s): expected: ConfigMap to be created, but not created because of error %s", i, testcase.Name, err.Error())
				}
			}

			if plugin == "env" {
				if testcase.Job.Status.ControlledResources["plugin-env"] == "" {
					t.Errorf("Case %d (%s): expected: to find controlled resource, but not found because of error %s", i, testcase.Name, err.Error())
				}
			}
		}
	}
}

func TestPluginOnJobDelete(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name    string
		Job     *vkv1.Job
		Plugins []string
		RetVal  error
	}{
		{
			Name: "Plugins",
			Job: &vkv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			Plugins: []string{"svc", "ssh", "env"},
			RetVal:  nil,
		},
		{
			Name: "Wrong Plugin",
			Job: &vkv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Job1",
				},
			},
			Plugins: []string{"new"},
			RetVal:  fmt.Errorf("failed to get plugin %s", "new"),
		},
	}

	for i, testcase := range testcases {

		fakeController := newFakeController()
		jobPlugins := make(map[string][]string)

		for _, plugin := range testcase.Plugins {
			jobPlugins[plugin] = make([]string, 0)
		}

		testcase.Job.Spec.Plugins = jobPlugins

		err := fakeController.pluginOnJobDelete(testcase.Job)
		if testcase.RetVal != nil && err.Error() != testcase.RetVal.Error() {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.RetVal, err)
		}

		for _, plugin := range testcase.Plugins {

			if plugin == "svc" {
				_, err := fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Get(fmt.Sprint(testcase.Job.Name, "-svc"), metav1.GetOptions{})
				if err == nil {
					t.Errorf("Case %d (%s): expected: ConfigMap to be deleted, but not deleted because of error %s", i, testcase.Name, err.Error())
				}

				_, err = fakeController.kubeClients.CoreV1().Services(namespace).Get(testcase.Job.Name, metav1.GetOptions{})
				if err == nil {
					t.Errorf("Case %d (%s): expected: Service to be deleted, but not deleted because of error %s", i, testcase.Name, err.Error())
				}
			}

			if plugin == "ssh" {
				_, err := fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Get(fmt.Sprint(testcase.Job.Name, "-ssh"), metav1.GetOptions{})
				if err == nil {
					t.Errorf("Case %d (%s): expected: ConfigMap to be deleted, but not deleted because of error %s", i, testcase.Name, err.Error())
				}
			}
		}
	}
}
