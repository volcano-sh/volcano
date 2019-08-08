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

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingv1alpha2 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

func TestKillJobFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name           string
		Job            *v1alpha1.Job
		PodGroup       *schedulingv1alpha2.PodGroup
		PodRetainPhase state.PhaseMap
		UpdateStatus   state.UpdateStatusFn
		JobInfo        *apis.JobInfo
		Services       []v1.Service
		ConfigMaps     []v1.ConfigMap
		Pods           map[string]*v1.Pod
		Plugins        []string
		ExpextVal      error
	}{
		{
			Name: "KillJob success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			PodGroup: &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			PodRetainPhase: state.PodRetainPhaseNone,
			UpdateStatus:   nil,
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Services: []v1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
				},
			},
			ConfigMaps: []v1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1-ssh",
						Namespace: namespace,
					},
				},
			},
			Pods: map[string]*v1.Pod{
				"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
				"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
			},
			Plugins:   []string{"svc", "ssh", "env"},
			ExpextVal: nil,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			jobPlugins := make(map[string][]string)

			for _, service := range testcase.Services {
				_, err := fakeController.kubeClients.CoreV1().Services(namespace).Create(&service)
				if err != nil {
					t.Error("Error While Creating Service")
				}
			}

			for _, configMap := range testcase.ConfigMaps {
				_, err := fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Create(&configMap)
				if err != nil {
					t.Error("Error While Creating ConfigMaps")
				}
			}

			for _, pod := range testcase.Pods {
				_, err := fakeController.kubeClients.CoreV1().Pods(namespace).Create(pod)
				if err != nil {
					t.Error("Error While Creating ConfigMaps")
				}
			}

			_, err := fakeController.vkClients.BatchV1alpha1().Jobs(namespace).Create(testcase.Job)
			if err != nil {
				t.Error("Error While Creating Jobs")
			}
			err = fakeController.cache.Add(testcase.Job)
			if err != nil {
				t.Error("Error While Adding Job in cache")
			}

			for _, plugin := range testcase.Plugins {
				jobPlugins[plugin] = make([]string, 0)
			}

			testcase.JobInfo.Job = testcase.Job
			testcase.JobInfo.Job.Spec.Plugins = jobPlugins

			err = fakeController.killJob(testcase.JobInfo, testcase.PodRetainPhase, testcase.UpdateStatus)
			if err != nil {
				t.Errorf("Case %d (%s): expected: No Error, but got error %s", i, testcase.Name, err.Error())
			}

			for _, plugin := range testcase.Plugins {

				if plugin == "svc" {
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

		})
	}
}

func TestCreateJobFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name         string
		Job          *v1alpha1.Job
		PodGroup     *schedulingv1alpha2.PodGroup
		UpdateStatus state.UpdateStatusFn
		JobInfo      *apis.JobInfo
		Plugins      []string
		ExpextVal    error
	}{
		{
			Name: "CreateJob success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Pending,
					},
				},
			},
			PodGroup: &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			UpdateStatus: nil,
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
			},
			Plugins:   []string{"svc", "ssh", "env"},
			ExpextVal: nil,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			jobPlugins := make(map[string][]string)

			for _, plugin := range testcase.Plugins {
				jobPlugins[plugin] = make([]string, 0)
			}
			testcase.JobInfo.Job = testcase.Job
			testcase.JobInfo.Job.Spec.Plugins = jobPlugins

			_, err := fakeController.vkClients.BatchV1alpha1().Jobs(namespace).Create(testcase.Job)
			if err != nil {
				t.Errorf("Case %d (%s): expected: No Error, but got error %s", i, testcase.Name, err.Error())
			}

			err = fakeController.cache.Add(testcase.Job)
			if err != nil {
				t.Error("Error While Adding Job in cache")
			}

			err = fakeController.createJob(testcase.JobInfo, testcase.UpdateStatus)
			if err != nil {
				t.Errorf("Case %d (%s): expected: No Error, but got error %s", i, testcase.Name, err.Error())
			}

			job, err := fakeController.vkClients.BatchV1alpha1().Jobs(namespace).Get(testcase.Job.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Case %d (%s): expected: No Error, but got error %s", i, testcase.Name, err.Error())
			}
			for _, plugin := range testcase.Plugins {

				if plugin == "svc" {
					_, err = fakeController.kubeClients.CoreV1().Services(namespace).Get(testcase.Job.Name, metav1.GetOptions{})
					if err != nil {
						t.Errorf("Case %d (%s): expected: Service to be created, but not created because of error %s", i, testcase.Name, err.Error())
					}

					_, err = fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Get(fmt.Sprint(testcase.Job.Name, "-svc"), metav1.GetOptions{})
					if err != nil {
						t.Errorf("Case %d (%s): expected: Service to be created, but not created because of error %s", i, testcase.Name, err.Error())
					}

					exist := job.Status.ControlledResources["plugin-svc"]
					if exist == "" {
						t.Errorf("Case %d (%s): expected: ControlledResources should be added, but not got added", i, testcase.Name)
					}
				}

				if plugin == "ssh" {
					_, err := fakeController.kubeClients.CoreV1().ConfigMaps(namespace).Get(fmt.Sprint(testcase.Job.Name, "-ssh"), metav1.GetOptions{})
					if err != nil {
						t.Errorf("Case %d (%s): expected: ConfigMap to be created, but not created because of error %s", i, testcase.Name, err.Error())
					}
					exist := job.Status.ControlledResources["plugin-ssh"]
					if exist == "" {
						t.Errorf("Case %d (%s): expected: ControlledResources should be added, but not got added", i, testcase.Name)
					}
				}
				if plugin == "env" {
					exist := job.Status.ControlledResources["plugin-env"]
					if exist == "" {
						t.Errorf("Case %d (%s): expected: ControlledResources should be added, but not got added", i, testcase.Name)
					}
				}
			}
		})

	}
}

func TestSyncJobFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name           string
		Job            *v1alpha1.Job
		PodGroup       *schedulingv1alpha2.PodGroup
		PodRetainPhase state.PhaseMap
		UpdateStatus   state.UpdateStatusFn
		JobInfo        *apis.JobInfo
		Pods           map[string]*v1.Pod
		Plugins        []string
		TotalNumPods   int
		ExpextVal      error
	}{
		{
			Name: "SyncJob success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
				},
			},
			PodGroup: &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			PodRetainPhase: state.PodRetainPhaseNone,
			UpdateStatus:   nil,
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"job1-task1-0": buildPod(namespace, "job1-task1-0", v1.PodRunning, nil),
						"job1-task1-1": buildPod(namespace, "job1-task1-1", v1.PodRunning, nil),
					},
				},
			},
			Pods: map[string]*v1.Pod{
				"job1-task1-0": buildPod(namespace, "job1-task1-0", v1.PodRunning, nil),
				"job1-task1-1": buildPod(namespace, "job1-task1-1", v1.PodRunning, nil),
			},
			TotalNumPods: 6,
			Plugins:      []string{"svc", "ssh", "env"},
			ExpextVal:    nil,
		},
	}
	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			jobPlugins := make(map[string][]string)

			for _, plugin := range testcase.Plugins {
				jobPlugins[plugin] = make([]string, 0)
			}
			testcase.JobInfo.Job = testcase.Job
			testcase.JobInfo.Job.Spec.Plugins = jobPlugins

			for _, pod := range testcase.Pods {
				_, err := fakeController.kubeClients.CoreV1().Pods(namespace).Create(pod)
				if err != nil {
					t.Error("Error While Creating pods")
				}
			}

			_, err := fakeController.vkClients.BatchV1alpha1().Jobs(namespace).Create(testcase.Job)
			if err != nil {
				t.Errorf("Expected no Error while creating job, but got error: %s", err)
			}

			err = fakeController.cache.Add(testcase.Job)
			if err != nil {
				t.Error("Error While Adding Job in cache")
			}

			err = fakeController.syncJob(testcase.JobInfo, nil)
			if err != testcase.ExpextVal {
				t.Errorf("Expected no error while syncing job, but got error: %s", err)
			}

			podList, err := fakeController.kubeClients.CoreV1().Pods(namespace).List(metav1.ListOptions{})
			if err != nil {
				t.Errorf("Expected no error while listing pods, but got error %s in case %d", err, i)
			}
			if testcase.TotalNumPods != len(podList.Items) {
				t.Errorf("Expected Total number of pods to be same as podlist count: Expected: %d, Got: %d in case: %d", testcase.TotalNumPods, len(podList.Items), i)
			}
		})
	}
}

func TestCreateJobIOIfNotExistFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name      string
		Job       *v1alpha1.Job
		ExpextVal error
	}{
		{
			Name: "Create Job IO success case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					Volumes: []v1alpha1.VolumeSpec{
						{
							VolumeClaimName: "pvc1",
						},
					},
				},
			},
			ExpextVal: nil,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			job, err := fakeController.createJobIOIfNotExist(testcase.Job)
			if err != testcase.ExpextVal {
				t.Errorf("Expected Return value to be : %s, but got: %s in testcase %d", testcase.ExpextVal, err, i)
			}

			if len(job.Spec.Volumes) == 0 {
				t.Errorf("Expected number of volumes to be greater than 0 but got: %d in case: %d", len(job.Spec.Volumes), i)
			}
		})
	}
}

func TestCreatePVCFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		VolumeClaim *v1.PersistentVolumeClaimSpec
		ExpextVal   error
	}{
		{
			Name: "CreatePVC success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			VolumeClaim: &v1.PersistentVolumeClaimSpec{
				VolumeName: "vol1",
			},
			ExpextVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			err := fakeController.createPVC(testcase.Job, "pvc1", testcase.VolumeClaim)
			if err != testcase.ExpextVal {
				t.Errorf("Expected return value to be equal to expected: %s, but got: %s", testcase.ExpextVal, err)
			}
			_, err = fakeController.kubeClients.CoreV1().PersistentVolumeClaims(namespace).Get("pvc1", metav1.GetOptions{})
			if err != nil {
				t.Error("Expected PVC to get created, but not created")
			}
		})
	}
}

func TestCreatePodGroupIfNotExistFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name      string
		Job       *v1alpha1.Job
		ExpextVal error
	}{
		{
			Name: "CreatePodGroup success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "job1",
				},
			},
			ExpextVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			err := fakeController.createPodGroupIfNotExist(testcase.Job)
			if err != testcase.ExpextVal {
				t.Errorf("Expected return value to be equal to expected: %s, but got: %s", testcase.ExpextVal, err)
			}

			_, err = fakeController.vkClients.SchedulingV1alpha2().PodGroups(namespace).Get(testcase.Job.Name, metav1.GetOptions{})
			if err != nil {
				t.Error("Expected PodGroup to get created, but not created")
			}
		})

	}
}

func TestDeleteJobPod(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name      string
		Job       *v1alpha1.Job
		Pods      map[string]*v1.Pod
		DeletePod *v1.Pod
		ExpextVal error
	}{
		{
			Name: "DeleteJobPod success case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			Pods: map[string]*v1.Pod{
				"job1-task1-0": buildPod(namespace, "job1-task1-0", v1.PodRunning, nil),
				"job1-task1-1": buildPod(namespace, "job1-task1-1", v1.PodRunning, nil),
			},
			DeletePod: buildPod(namespace, "job1-task1-0", v1.PodRunning, nil),
			ExpextVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			for _, pod := range testcase.Pods {
				_, err := fakeController.kubeClients.CoreV1().Pods(namespace).Create(pod)
				if err != nil {
					t.Error("Expected error not to occur")
				}
			}

			err := fakeController.deleteJobPod(testcase.Job.Name, testcase.DeletePod)
			if err != testcase.ExpextVal {
				t.Errorf("Expected return value to be equal to expected: %s, but got: %s", testcase.ExpextVal, err)
			}

			_, err = fakeController.kubeClients.CoreV1().Pods(namespace).Get("job1-task1-0", metav1.GetOptions{})
			if err == nil {
				t.Error("Expected Pod to be deleted but not deleted")
			}
		})
	}
}
