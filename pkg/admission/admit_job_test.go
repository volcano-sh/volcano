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

package admission

import (
	"strings"
	"testing"

	kubebatchclient "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned/fake"

	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbv1aplha1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	v1alpha1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

func TestValidateExecution(t *testing.T) {

	namespace := "test"
	var invTtl int32 = -1

	testCases := []struct {
		Name           string
		Job            v1alpha1.Job
		ExpectErr      bool
		reviewResponse v1beta1.AdmissionResponse
		ret            string
	}{
		{
			Name: "validate valid-job",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-Job",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// duplicate task name
		{
			Name: "duplicate-task-job",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "duplicate-task-job",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "duplicated-task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
						{
							Name:     "duplicated-task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "duplicated task name duplicated-task-1",
			ExpectErr:      true,
		},
		// Duplicated Policy Event
		{
			Name: "job-policy-duplicated",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-policy-duplicated",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  v1alpha1.PodFailedEvent,
							Action: v1alpha1.AbortJobAction,
						},
						{
							Event:  v1alpha1.PodFailedEvent,
							Action: v1alpha1.RestartJobAction,
						},
					},
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "duplicate",
			ExpectErr:      true,
		},
		// Min Available illegal
		{
			Name: "Min Available illegal",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-min-illegal",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 2,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "'minAvailable' should not be greater than total replicas in tasks",
			ExpectErr:      true,
		},
		// Job Plugin illegal
		{
			Name: "Job Plugin illegal",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-plugin-illegal",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Plugins: map[string][]string{
						"big_plugin": {},
					},
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "unable to find job plugin: big_plugin",
			ExpectErr:      true,
		},
		// ttl-illegal
		{
			Name: "job-ttl-illegal",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-ttl-illegal",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					TTLSecondsAfterFinished: &invTtl,
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "'ttlSecondsAfterFinished' cannot be less than zero",
			ExpectErr:      true,
		},
	}

	for _, testCase := range testCases {

		defaultqueue := kbv1aplha1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
			Spec: kbv1aplha1.QueueSpec{
				Weight: 1,
			},
		}
		// create fake kube-batch clientset
		KubeBatchClientSet = kubebatchclient.NewSimpleClientset()

		//create default queue
		_, err := KubeBatchClientSet.SchedulingV1alpha1().Queues().Create(&defaultqueue)
		if err != nil {
			t.Error("Queue Creation Failed")
		}

		ret := validateJob(testCase.Job, &testCase.reviewResponse)
		//fmt.Printf("test-case name:%s, ret:%v  testCase.reviewResponse:%v \n", testCase.Name, ret,testCase.reviewResponse)
		if testCase.ExpectErr == true && ret == "" {
			t.Errorf("%s: test case Expect error msg :%s, but got nil.", testCase.Name, testCase.ret)
		}
		if testCase.ExpectErr == true && testCase.reviewResponse.Allowed != false {
			t.Errorf("%s: test case Expect Allowed as false but got true.", testCase.Name)
		}
		if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
			t.Errorf("%s: test case Expect error msg :%s, but got diff error %v", testCase.Name, testCase.ret, ret)
		}

		if testCase.ExpectErr == false && ret != "" {
			t.Errorf("%s: test case Expect no error, but got error %v", testCase.Name, ret)
		}
		if testCase.ExpectErr == false && testCase.reviewResponse.Allowed != true {
			t.Errorf("%s: test case Expect Allowed as true but got false. %v", testCase.Name, testCase.reviewResponse)
		}

	}

}
