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

package validate

import (
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	schedulingv1beta2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
)

func TestValidateJobFlowCreate(t *testing.T) {
	namespace := "test"
	testCases := []struct {
		Name           string
		JobFlow        flowv1alpha1.JobFlow
		ExpectErr      bool
		reviewResponse admissionv1.AdmissionResponse
		ret            string
	}{
		{
			Name: "validate valid-jobflow",
			JobFlow: flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-jobflow",
					Namespace: namespace,
				},
				Spec: flowv1alpha1.JobFlowSpec{
					Flows: []flowv1alpha1.Flow{
						{
							Name: "a",
						},
						{
							Name: "b",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a"},
							},
						},
						{
							Name: "c",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a", "b"},
							},
						},
						{
							Name: "d",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"b"},
							},
						},
						{
							Name: "e",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"c", "d"},
							},
						},
					},
					JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// duplicate flow name
		{
			Name: "validate valid-jobflow with duplicate flow name",
			JobFlow: flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-jobflow",
					Namespace: namespace,
				},
				Spec: flowv1alpha1.JobFlowSpec{
					Flows: []flowv1alpha1.Flow{
						{
							Name: "a",
						},
						{
							Name: "a",
						},
						{
							Name: "b",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a"},
							},
						},
						{
							Name: "c",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a", "b"},
							},
						},
						{
							Name: "d",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"b"},
							},
						},
						{
							Name: "e",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"c", "d"},
							},
						},
					},
					JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// 	Miss flow name a
		{
			Name: "validate valid-jobflow with miss flow name a",
			JobFlow: flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-jobflow",
					Namespace: namespace,
				},
				Spec: flowv1alpha1.JobFlowSpec{
					Flows: []flowv1alpha1.Flow{
						{
							Name: "b",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a"},
							},
						},
						{
							Name: "c",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a", "b"},
							},
						},
						{
							Name: "d",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"b"},
							},
						},
						{
							Name: "e",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"c", "d"},
							},
						},
					},
					JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "jobflow Flow is not DAG: vertex is not defined: a",
			ExpectErr:      true,
		},
		// 	jobflow flows not dag
		{
			Name: "validate valid-jobflow with flows not dag",
			JobFlow: flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-jobflow",
					Namespace: namespace,
				},
				Spec: flowv1alpha1.JobFlowSpec{
					Flows: []flowv1alpha1.Flow{
						{
							Name: "a",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"b"},
							},
						},
						{
							Name: "b",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a"},
							},
						},
						{
							Name: "c",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a", "b"},
							},
						},
						{
							Name: "d",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"b"},
							},
						},
						{
							Name: "e",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"c", "d"},
							},
						},
					},
					JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "jobflow Flow is not DAG",
			ExpectErr:      true,
		},
		// 	jobflow flows with muti c
		{
			Name: "validate valid-jobflow with lows with muti c",
			JobFlow: flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-jobflow",
					Namespace: namespace,
				},
				Spec: flowv1alpha1.JobFlowSpec{
					Flows: []flowv1alpha1.Flow{
						{
							Name: "a",
						},
						{
							Name: "b",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a"},
							},
						},
						{
							Name: "c",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"a"},
							},
						},
						{
							Name: "c",
							DependsOn: &flowv1alpha1.DependsOn{
								Targets: []string{"b"},
							},
						},
					},
					JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
	}

	defaultqueue := &schedulingv1beta2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: namespace,
		},
		Spec: schedulingv1beta2.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta2.QueueStatus{
			State: schedulingv1beta2.QueueStateOpen,
		},
	}

	// create fake volcano clientset
	config.VolcanoClient = fakeclient.NewSimpleClientset(defaultqueue)
	informerFactory := informers.NewSharedInformerFactory(config.VolcanoClient, 0)
	queueInformer := informerFactory.Scheduling().V1beta1().Queues()
	config.QueueLister = queueInformer.Lister()

	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	for informerType, ok := range informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			panic(fmt.Errorf("failed to sync cache: %v", informerType))
		}
	}
	defer close(stopCh)

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			ret := validateJobFlowDAG(&testCase.JobFlow, &testCase.reviewResponse)
			//fmt.Printf("test-case name:%s, ret:%v  testCase.reviewResponse:%v \n", testCase.Name, ret,testCase.reviewResponse)
			if testCase.ExpectErr == true && ret == "" {
				t.Errorf("Expect error msg :%s, but got nil.", testCase.ret)
			}
			if testCase.ExpectErr == true && testCase.reviewResponse.Allowed != false {
				t.Errorf("Expect Allowed as false but got true.")
			}
			if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
				t.Errorf("Expect error msg :%s, but got diff error %v", testCase.ret, ret)
			}

			if testCase.ExpectErr == false && ret != "" {
				t.Errorf("Expect no error, but got error %v", ret)
			}
			if testCase.ExpectErr == false && testCase.reviewResponse.Allowed != true {
				t.Errorf("Expect Allowed as true but got false. %v", testCase.reviewResponse)
			}
		})
	}
}
