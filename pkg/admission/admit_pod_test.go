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

	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	vcclient "volcano.sh/volcano/pkg/client/clientset/versioned/fake"
)

func TestValidatePod(t *testing.T) {

	namespace := "test"
	pgName := "podgroup-p1"
	isController := true

	testCases := []struct {
		Name           string
		Pod            v1.Pod
		ExpectErr      bool
		reviewResponse v1beta1.AdmissionResponse
		ret            string
		disabledPG     bool
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

			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// validate normal pod with volcano scheduler
		{
			Name: "validate volcano-scheduler normal pod",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "normal-pod-2",
					OwnerReferences: []metav1.OwnerReference{
						{UID: "p1", Controller: &isController},
					},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: v1beta1.AdmissionResponse{Allowed: false},
			ret:            "Failed to create pod <test/normal-pod-2>, because the podgroup phase is Pending",
			ExpectErr:      true,
		},
		// validate volcano pod with volcano scheduler
		{
			Name: "validate volcano-scheduler volcano pod",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "volcano-pod-1",
					Annotations: map[string]string{v1alpha2.GroupNameAnnotationKey: pgName},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: v1beta1.AdmissionResponse{Allowed: false},
			ret:            "Failed to create pod <test/volcano-pod-1>, because the podgroup phase is Pending",
			ExpectErr:      true,
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
					Annotations: map[string]string{v1alpha2.GroupNameAnnotationKey: pgName},
				},
				Spec: v1.PodSpec{
					SchedulerName: "volcano",
				},
			},

			reviewResponse: v1beta1.AdmissionResponse{Allowed: false},
			ret:            `Failed to get PodGroup for pod <test/volcano-pod-2>: podgroups.scheduling "podgroup-p1" not found`,
			ExpectErr:      true,
			disabledPG:     true,
		},
	}

	for _, testCase := range testCases {

		pg := &v1alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "podgroup-p1",
			},
			Spec: v1alpha2.PodGroupSpec{
				MinMember: 1,
			},
			Status: v1alpha2.PodGroupStatus{
				Phase: v1alpha2.PodGroupPending,
			},
		}

		// create fake volcano clientset
		VolcanoClientSet = vcclient.NewSimpleClientset()

		if !testCase.disabledPG {
			_, err := VolcanoClientSet.SchedulingV1alpha2().PodGroups(namespace).Create(pg)
			if err != nil {
				t.Error("PG Creation Failed")
			}
		}

		c := Controller{
			VcClients:     VolcanoClientSet,
			SchedulerName: "volcano",
		}

		ret := c.validatePod(testCase.Pod, &testCase.reviewResponse)

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
