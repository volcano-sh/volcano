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
	"errors"
	"fmt"
	"reflect"
	"testing"

	gomonkey "github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1alpha2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
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
		Secrets        []v1.Secret
		Pods           map[string]*v1.Pod
		Plugins        []string
		ExpectVal      error
	}{
		{
			Name: "KillJob success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					UID:             "e7f18111-1cec-11ea-b688-fa163ec79500",
					ResourceVersion: "100",
				},
			},
			PodGroup: &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-e7f18111-1cec-11ea-b688-fa163ec79500",
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
			Secrets: []v1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1-e7f18111-1cec-11ea-b688-fa163ec79500-ssh",
						Namespace: namespace,
					},
				},
			},
			Pods: map[string]*v1.Pod{
				"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
				"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
			},
			Plugins:   []string{"svc", "ssh", "env"},
			ExpectVal: nil,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			jobPlugins := make(map[string][]string)

			for _, service := range testcase.Services {
				_, err := fakeController.kubeClient.CoreV1().Services(namespace).Create(context.TODO(), &service, metav1.CreateOptions{})
				if err != nil {
					t.Error("Error While Creating Service")
				}
			}

			for _, secret := range testcase.Secrets {
				_, err := fakeController.kubeClient.CoreV1().Secrets(namespace).Create(context.TODO(), &secret, metav1.CreateOptions{})
				if err != nil {
					t.Error("Error While Creating Secret.")
				}
			}

			for _, pod := range testcase.Pods {
				_, err := fakeController.kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
				if err != nil {
					t.Error("Error While Creating ConfigMaps")
				}
			}

			_, err := fakeController.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.Job, metav1.CreateOptions{})
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

			testcase.JobInfo.Job.Status.ControlledResources = map[string]string{}
			for _, name := range testcase.Plugins {
				testcase.JobInfo.Job.Status.ControlledResources["plugin-"+name] = name
			}

			err = fakeController.killJob(testcase.JobInfo, testcase.PodRetainPhase, testcase.UpdateStatus)
			if err != nil {
				t.Errorf("Case %d (%s): expected: No Error, but got error %v.", i, testcase.Name, err)
			}

			for _, plugin := range testcase.Plugins {

				if plugin == "svc" {
					_, err = fakeController.kubeClient.CoreV1().Services(namespace).Get(context.TODO(), testcase.Job.Name, metav1.GetOptions{})
					if err == nil {
						t.Errorf("Case %d (%s): expected: Service to be deleted, but not deleted.", i, testcase.Name)
					}
				}

				if plugin == "ssh" {
					_, err := fakeController.kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(),
						fmt.Sprintf("%s-%s-%s", testcase.Job.Name, testcase.Job.UID, "ssh"), metav1.GetOptions{})
					if err == nil {
						t.Errorf("Case %d (%s): expected: Secret to be deleted, but not deleted.", i, testcase.Name)
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
		ExpectVal      error
	}{
		{
			Name: "SyncJob success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "100",
					UID:             "e7f18111-1cec-11ea-b688-fa163ec79500",
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
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Pending,
					},
				},
			},
			PodGroup: &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-e7f18111-1cec-11ea-b688-fa163ec79500",
					Namespace: namespace,
				},
				Spec: schedulingv1alpha2.PodGroupSpec{
					MinResources:  &v1.ResourceList{},
					MinTaskMember: map[string]int32{},
				},
				Status: schedulingv1alpha2.PodGroupStatus{
					Phase: schedulingv1alpha2.PodGroupInqueue,
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
			ExpectVal:    nil,
		},
	}
	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			patches := gomonkey.ApplyMethod(reflect.TypeOf(fakeController), "GetQueueInfo", func(_ *jobcontroller, _ string) (*schedulingv1alpha2.Queue, error) {
				return &schedulingv1alpha2.Queue{}, nil
			})

			defer patches.Reset()

			jobPlugins := make(map[string][]string)

			for _, plugin := range testcase.Plugins {
				jobPlugins[plugin] = make([]string, 0)
			}
			testcase.JobInfo.Job = testcase.Job
			testcase.JobInfo.Job.Spec.Plugins = jobPlugins

			fakeController.pgInformer.Informer().GetIndexer().Add(testcase.PodGroup)

			for _, pod := range testcase.Pods {
				_, err := fakeController.kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
				if err != nil {
					t.Error("Error While Creating pods")
				}
			}

			_, err := fakeController.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.Job, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Expected no Error while creating job, but got error: %s", err)
			}

			err = fakeController.cache.Add(testcase.Job)
			if err != nil {
				t.Error("Error While Adding Job in cache")
			}

			err = fakeController.syncJob(testcase.JobInfo, nil)
			if err != testcase.ExpectVal {
				t.Errorf("Expected no error while syncing job, but got error: %s", err)
			}

			podList, err := fakeController.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
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
			Name: "Create Job IO case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "100",
				},
				Spec: v1alpha1.JobSpec{
					Volumes: []v1alpha1.VolumeSpec{
						{
							VolumeClaimName: "pvc1",
						},
					},
				},
			},
			ExpextVal: errors.New("pvc pvc1 is not found, the job will be in the Pending state until the PVC is created"),
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			job, err := fakeController.createJobIOIfNotExist(testcase.Job)
			if testcase.ExpextVal == nil {
				if err != nil {
					t.Errorf("Expected Return value to be : %v, but got: %v in testcase %d", testcase.ExpextVal, err, i)
				}
			} else {
				if err == nil || err.Error() != testcase.ExpextVal.Error() {
					t.Errorf("Expected Return value to be : %v, but got: %v in testcase %d", testcase.ExpextVal.Error(), err.Error(), i)
				}
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
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "100",
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
			_, err = fakeController.kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), "pvc1", metav1.GetOptions{})
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
					Namespace:       namespace,
					Name:            "job1",
					ResourceVersion: "100",
					UID:             "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
			},
			ExpextVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()

			err := fakeController.createOrUpdatePodGroup(testcase.Job)
			if err != testcase.ExpextVal {
				t.Errorf("Expected return value to be equal to expected: %s, but got: %s", testcase.ExpextVal, err)
			}

			pgName := testcase.Job.Name + "-" + string(testcase.Job.UID)
			_, err = fakeController.vcClient.SchedulingV1beta1().PodGroups(namespace).Get(context.TODO(), pgName, metav1.GetOptions{})
			if err != nil {
				t.Error("Expected PodGroup to get created, but not created")
			}
		})

	}
}

func TestUpdatePodGroupIfJobUpdateFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name      string
		PodGroup  *schedulingv1alpha2.PodGroup
		Job       *v1alpha1.Job
		ExpectVal error
	}{
		{
			Name: "UpdatePodGroup success Case",
			PodGroup: &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "job1-e7f18111-1cec-11ea-b688-fa163ec79500",
				},
				Spec: schedulingv1alpha2.PodGroupSpec{
					MinResources: &v1.ResourceList{},
				},
			},
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       namespace,
					Name:            "job1",
					ResourceVersion: "100",
					UID:             "e7f18111-1cec-11ea-b688-fa163ec79500",
				},
				Spec: v1alpha1.JobSpec{
					PriorityClassName: "new",
				},
			},
			ExpectVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			fakeController := newFakeController()
			fakeController.pgInformer.Informer().GetIndexer().Add(testcase.PodGroup)
			fakeController.vcClient.SchedulingV1beta1().PodGroups(testcase.PodGroup.Namespace).Create(context.TODO(), testcase.PodGroup, metav1.CreateOptions{})

			err := fakeController.createOrUpdatePodGroup(testcase.Job)
			if err != testcase.ExpectVal {
				t.Errorf("Expected return value to be equal to expected: %s, but got: %s", testcase.ExpectVal, err)
			}

			pgName := testcase.Job.Name + "-" + string(testcase.Job.UID)
			pg, err := fakeController.vcClient.SchedulingV1beta1().PodGroups(namespace).Get(context.TODO(), pgName, metav1.GetOptions{})
			if err != nil {
				t.Error("Expected PodGroup to be created, but not created")
			}
			if pg.Spec.PriorityClassName != testcase.Job.Spec.PriorityClassName {
				t.Errorf("Expected PodGroup.Spec.PriorityClassName to be updated to: %s, but got: %s", testcase.Job.Spec.PriorityClassName, pg.Spec.PriorityClassName)
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
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "100",
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
				_, err := fakeController.kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
				if err != nil {
					t.Error("Expected error not to occur")
				}
			}

			err := fakeController.deleteJobPod(testcase.Job.Name, testcase.DeletePod)
			if err != testcase.ExpextVal {
				t.Errorf("Expected return value to be equal to expected: %s, but got: %s", testcase.ExpextVal, err)
			}

			_, err = fakeController.kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), "job1-task1-0", metav1.GetOptions{})
			if err == nil {
				t.Error("Expected Pod to be deleted but not deleted")
			}
		})
	}
}
