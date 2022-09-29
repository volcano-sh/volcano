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

package validate

import (
	"context"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcschedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
)

func TestValidatePod(t *testing.T) {

	namespace := "test"
	pgName := "podgroup-p1"

	testCases := []struct {
		Name           string
		Pod            v1.Pod
		ExpectErr      bool
		reviewResponse admissionv1.AdmissionResponse
		ret            string
		disabledPG     bool
		queueName      string
		queueState     vcschedulingv1.QueueState
	}{
		// validate normal pod with default-scheduler
		{
			Name: "validate default normal pod",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "normal-pod-1",
				},
				Spec: v1.PodSpec{
					SchedulerName: "default-scheduler",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// validate volcano pod with volcano scheduler when get pg failed
		{
			Name: "validate volcano volcano pod when get pg failed",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-2",
					Annotations: map[string]string{vcschedulingv1.KubeGroupNameAnnotationKey: pgName},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            `failed to get PodGroup for pod <test/volcano-pod-2>: podgroups.scheduling.volcano.sh "podgroup-p1" not found`,
			ExpectErr:      true,
			disabledPG:     true,
		},
		// validate volcano pod with volcano scheduler when queue is closed when no pg
		{
			Name: "validate pod when volcano queue is closed when no pg",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-3",
					Annotations: map[string]string{vcschedulingv1.QueueNameAnnotationKey: "queue-closed"},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "can only submit job to queue with state `Open`, queue `queue-closed` status is `Closed`",
			ExpectErr:      true,
			queueState:     vcschedulingv1.QueueStateClosed,
			queueName:      "queue-closed",
		},
		// validate volcano pod with volcano scheduler when queue is Open when no pg
		{
			Name: "validate pod when volcano queue is open when no pg",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-4",
					Annotations: map[string]string{vcschedulingv1.QueueNameAnnotationKey: "queue-open"},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
			queueState:     vcschedulingv1.QueueStateOpen,
			queueName:      "queue-open",
		},
		// validate volcano pod with volcano scheduler when queue is Open when pg
		{
			Name: "validate pod when volcano queue is open when pg",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-5",
					Annotations: map[string]string{vcschedulingv1.KubeGroupNameAnnotationKey: pgName},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
			queueName:      "queue-open",
			queueState:     vcschedulingv1.QueueStateOpen,
		},
		// validate volcano pod with volcano scheduler when queue is Closed when pg
		{
			Name: "validate pod when volcano queue is closed when pg",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-6",
					Annotations: map[string]string{vcschedulingv1.KubeGroupNameAnnotationKey: pgName},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "can only submit job to queue with state `Open`, queue `queue-closed` status is `Closed`",
			ExpectErr:      true,
			queueName:      "queue-closed",
			queueState:     vcschedulingv1.QueueStateClosed,
		},
		// validate volcano pod with volcano scheduler when no queue and no pg
		{
			Name: "validate volcano pod with volcano scheduler when no queue and no pg",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "volcano-pod-7",
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
			disabledPG:     true,
		},
		// validate volcano pod with volcano scheduler when queue name is empty and when pg
		{
			Name: "validate volcano pod with volcano scheduler when no queue and no pg",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-8",
					Annotations: map[string]string{vcschedulingv1.KubeGroupNameAnnotationKey: pgName},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
			queueName:      "",
		},
	}

	for _, testCase := range testCases {

		pg := &vcschedulingv1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "podgroup-p1",
			},
			Spec: vcschedulingv1.PodGroupSpec{
				MinMember: 1,
				Queue:     testCase.queueName,
			},
			Status: vcschedulingv1.PodGroupStatus{
				Phase: vcschedulingv1.PodGroupPending,
			},
		}
		queue := vcschedulingv1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: testCase.queueName,
			},
			Spec: vcschedulingv1.QueueSpec{
				Weight: 1,
			},
			Status: vcschedulingv1.QueueStatus{
				State: testCase.queueState,
			},
		}

		// create fake volcano clientset
		config.VolcanoClient = vcclient.NewSimpleClientset()
		config.SchedulerNames = []string{"volcano"}

		if !testCase.disabledPG {
			_, err := config.VolcanoClient.SchedulingV1beta1().PodGroups(namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
			if err != nil {
				t.Error("PG Creation Failed")
			}
		}

		if testCase.queueName != "" && testCase.queueState != "" {
			//create default queue
			_, err := config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &queue, metav1.CreateOptions{})
			if err != nil {
				t.Error("Queue Creation Failed")
			}
		}

		ret := validatePod(&testCase.Pod, &testCase.reviewResponse)

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
