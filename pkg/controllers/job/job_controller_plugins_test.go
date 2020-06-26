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
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes/fake"

	batch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	volcanoclient "volcano.sh/volcano/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func newFakeController() *jobcontroller {
	volcanoClientSet := volcanoclient.NewSimpleClientset()
	kubeClientSet := kubeclient.NewSimpleClientset()

	sharedInformers := informers.NewSharedInformerFactory(kubeClientSet, 0)

	controller := &jobcontroller{}
	opt := &framework.ControllerOption{
		VolcanoClient:         volcanoClientSet,
		KubeClient:            kubeClientSet,
		SharedInformerFactory: sharedInformers,
		WorkerNum:             3,
	}

	controller.Initialize(opt)

	return controller
}

func TestPluginOnPodCreate(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name    string
		Job     *batch.Job
		Pod     *v1.Pod
		Plugins []string
		RetVal  error
	}{
		{
			Name: "All Plugin",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Job1",
					Namespace: namespace,
					UID:       "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			Pod:     buildPod(namespace, "pod1", v1.PodPending, nil),
			Plugins: []string{"env", "svc", "ssh"},
			RetVal:  nil,
		},
		{
			Name: "Wrong Plugin",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Job1",
					UID:  "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			Pod:     buildPod(namespace, "pod1", v1.PodPending, nil),
			Plugins: []string{"new"},
			RetVal:  fmt.Errorf("failed to get plugin %s", "new"),
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
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
							if volume.Name == fmt.Sprintf("%s-%s", testcase.Job.Name, "ssh") {
								exist = true
							}
						}
						if !exist {
							t.Errorf("case %d (%s): expected: VolumeMount not created", i, testcase.Name)
						}
					}
				}
			}
		})
	}
}

func TestPluginOnJobAdd(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name    string
		Job     *batch.Job
		Plugins []string
		RetVal  error
	}{
		{
			Name: "Plugins",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
					UID:       "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			Plugins: []string{"svc", "ssh", "env"},
			RetVal:  nil,
		},
		{
			Name: "Wrong Plugin",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Job1",
					UID:  "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			Plugins: []string{"new"},
			RetVal:  fmt.Errorf("failed to get plugin %s", "new"),
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
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
					_, err := fakeController.kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), fmt.Sprint(testcase.Job.Name, "-svc"), metav1.GetOptions{})
					if err != nil {
						t.Errorf("Case %d (%s): expected: ConfigMap to be created, but not created because of error %s", i, testcase.Name, err.Error())
					}

					_, err = fakeController.kubeClient.CoreV1().Services(namespace).Get(context.TODO(), testcase.Job.Name, metav1.GetOptions{})
					if err != nil {
						t.Errorf("Case %d (%s): expected: Service to be created, but not created because of error %s", i, testcase.Name, err.Error())
					}
				}

				if plugin == "ssh" {
					_, err := fakeController.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(),
						fmt.Sprintf("%s-%s", testcase.Job.Name, "ssh"), metav1.GetOptions{})
					if err != nil {
						t.Errorf("Case %d (%s): expected: Secret to be created, but not created because of error %s", i, testcase.Name, err.Error())
					}
				}

				if plugin == "env" {
					if testcase.Job.Status.ControlledResources["plugin-env"] == "" {
						t.Errorf("Case %d (%s): expected: to find controlled resource, but not found because of error %s", i, testcase.Name, err.Error())
					}
				}
			}
		})
	}
}

func TestPluginOnJobDelete(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name    string
		Job     *batch.Job
		Plugins []string
		RetVal  error
	}{
		{
			Name: "Plugins",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
					UID:       "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			Plugins: []string{"svc", "ssh", "env"},
			RetVal:  nil,
		},
		{
			Name: "Wrong Plugin",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "Job1",
					UID:  "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			Plugins: []string{"new"},
			RetVal:  fmt.Errorf("failed to get plugin %s", "new"),
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
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
					_, err := fakeController.kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), fmt.Sprint(testcase.Job.Name, "-svc"), metav1.GetOptions{})
					if err == nil {
						t.Errorf("Case %d (%s): expected: ConfigMap to be deleted, but not deleted.", i, testcase.Name)
					}

					_, err = fakeController.kubeClient.CoreV1().Services(namespace).Get(context.TODO(), testcase.Job.Name, metav1.GetOptions{})
					if err == nil {
						t.Errorf("Case %d (%s): expected: Service to be deleted, but not deleted.", i, testcase.Name)
					}
				}

				if plugin == "ssh" {
					_, err := fakeController.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(),
						fmt.Sprintf("%s-%s-%s", testcase.Job.Name, testcase.Job.UID, "ssh"), metav1.GetOptions{})
					if err == nil {
						t.Errorf("Case %d (%s): expected: secret to be deleted, but not deleted.", i, testcase.Name)
					}
				}
			}
		})
	}
}
