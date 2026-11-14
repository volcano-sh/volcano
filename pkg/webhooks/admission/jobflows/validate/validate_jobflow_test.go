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
	"k8s.io/apimachinery/pkg/util/validation"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	schedulingv1beta2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
)

func TestAdmitJobFlowLabelValueLength(t *testing.T) {
	limit := validation.LabelValueMaxLength
	namespace := "ns"
	exactName := strings.Repeat("a", limit-len(namespace)-1)
	overName := exactName + "b"
	longFlow := strings.Repeat("f", limit-len("ml-training-pipelines"))

	testCases := []struct {
		name      string
		namespace string
		jobFlow   string
		flows     []string
		allowed   bool
		contains  []string
	}{
		{
			name:      "names from issue 6308 are rejected",
			namespace: "ml-training-pipelines",
			jobFlow:   "nightly-feature-extraction-and-training-run-v2",
			flows:     []string{"prep"},
			allowed:   false,
			contains: []string{
				"volcano.sh/createdByJobFlow",
				"ml-training-pipelines.nightly-feature-extraction-and-training-run-v2",
				"63",
			},
		},
		{
			name:      "flow name that overflows the jobtemplate label is rejected",
			namespace: "ml-training-pipelines",
			jobFlow:   "nightly-run",
			flows:     []string{"prep", longFlow},
			allowed:   false,
			contains: []string{
				"volcano.sh/createdByJobTemplate",
				longFlow,
				"63",
			},
		},
		{
			name:      "jobflow and flow labels that are both too long are both reported",
			namespace: "ml-training-pipelines",
			jobFlow:   "nightly-feature-extraction-and-training-run-v2",
			flows:     []string{longFlow},
			allowed:   false,
			contains: []string{
				"volcano.sh/createdByJobFlow",
				"volcano.sh/createdByJobTemplate",
			},
		},
		{
			name:      "label value at the 63 byte limit is allowed",
			namespace: namespace,
			jobFlow:   exactName,
			flows:     []string{"prep"},
			allowed:   true,
		},
		{
			name:      "label value one byte over the limit is rejected",
			namespace: namespace,
			jobFlow:   overName,
			flows:     []string{"prep"},
			allowed:   false,
			contains:  []string{"volcano.sh/createdByJobFlow", "64"},
		},
		{
			name:      "duplicate flow names are reported once",
			namespace: "ml-training-pipelines",
			jobFlow:   "nightly-run",
			flows:     []string{longFlow, longFlow},
			allowed:   false,
			contains:  []string{"volcano.sh/createdByJobTemplate"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			flows := make([]flowv1alpha1.Flow, 0, len(testCase.flows))
			for _, flowName := range testCase.flows {
				flows = append(flows, flowv1alpha1.Flow{Name: flowName})
			}
			jobFlow := &flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCase.jobFlow,
					Namespace: testCase.namespace,
				},
				Spec: flowv1alpha1.JobFlowSpec{
					Flows: flows,
				},
			}

			for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
				response := admitJobFlow(jobFlow, operation)
				if response.Allowed != testCase.allowed {
					t.Errorf("%s allowed = %v, want %v", operation, response.Allowed, testCase.allowed)
				}
				if testCase.allowed {
					if response.Result != nil {
						t.Errorf("%s result = %v, want nil", operation, response.Result)
					}
					continue
				}
				if response.Result == nil {
					t.Fatalf("%s result is nil", operation)
				}
				for _, want := range testCase.contains {
					if !strings.Contains(response.Result.Message, want) {
						t.Errorf("%s message %q does not contain %q", operation, response.Result.Message, want)
					}
				}
			}
		})
	}

	overFlow := strings.Repeat("f", limit)
	duplicate := admitJobFlow(&flowv1alpha1.JobFlow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nightly-run",
			Namespace: "ml-training-pipelines",
		},
		Spec: flowv1alpha1.JobFlowSpec{
			Flows: []flowv1alpha1.Flow{{Name: overFlow}, {Name: overFlow}},
		},
	}, admissionv1.Create)
	if duplicate.Result == nil {
		t.Fatal("expected a rejection for a duplicated long flow name")
	}
	if got, want := strings.Count(duplicate.Result.Message, "volcano.sh/createdByJobTemplate"), 1; got != want {
		t.Errorf("jobtemplate label reported %d times, want %d: %s", got, want, duplicate.Result.Message)
	}
}

func TestAdmitJobFlowStillRejectsCycle(t *testing.T) {
	jobFlow := &flowv1alpha1.JobFlow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "short",
			Namespace: "ns",
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
			},
		},
	}

	response := admitJobFlow(jobFlow, admissionv1.Create)
	if response.Allowed {
		t.Fatal("expected a cycle to be rejected")
	}
	if response.Result == nil || !strings.Contains(response.Result.Message, "jobflow Flow is not DAG") {
		t.Fatalf("message = %v", response.Result)
	}
}

func TestAdmitJobFlowReportsLabelAndDAGErrors(t *testing.T) {
	jobFlow := &flowv1alpha1.JobFlow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nightly-feature-extraction-and-training-run-v2",
			Namespace: "ml-training-pipelines",
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
			},
		},
	}

	response := admitJobFlow(jobFlow, admissionv1.Update)
	if response.Allowed {
		t.Fatal("expected the JobFlow to be rejected")
	}
	if response.Result == nil {
		t.Fatal("expected an admission message")
	}
	for _, want := range []string{"volcano.sh/createdByJobFlow", "jobflow Flow is not DAG"} {
		if !strings.Contains(response.Result.Message, want) {
			t.Errorf("message %q does not contain %q", response.Result.Message, want)
		}
	}
}

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
