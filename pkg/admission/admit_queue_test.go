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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	fakeclient "volcano.sh/volcano/pkg/client/clientset/versioned/fake"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestAdmitQueues(t *testing.T) {
	stateNotSet := v1alpha2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-not-set",
		},
		Spec: v1alpha2.QueueSpec{
			Weight: 1,
		},
	}

	stateNotSetJSON, err := json.Marshal(stateNotSet)
	if err != nil {
		t.Errorf("Marshal queue without state set failed for %v", err)
	}

	openState := v1alpha2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-set-open",
		},
		Spec: v1alpha2.QueueSpec{
			Weight: 1,
			State:  v1alpha2.QueueStateOpen,
		},
	}

	openStateJSON, err := json.Marshal(openState)
	if err != nil {
		t.Errorf("Marshal queue with open state failed for %v", err)
	}

	closedState := v1alpha2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-set-closed",
		},
		Spec: v1alpha2.QueueSpec{
			Weight: 1,
			State:  v1alpha2.QueueStateClosed,
		},
	}

	closedStateJSON, err := json.Marshal(closedState)
	if err != nil {
		t.Errorf("Marshal queue with closed state failed for %v", err)
	}

	wrongState := v1alpha2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "abnormal-case",
		},
		Spec: v1alpha2.QueueSpec{
			Weight: 1,
			State:  "wrong",
		},
	}

	wrongStateJSON, err := json.Marshal(wrongState)
	if err != nil {
		t.Errorf("Marshal queue with wrong state failed for %v", err)
	}

	openStateForDelete := v1alpha2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-state-for-delete",
		},
		Spec: v1alpha2.QueueSpec{
			Weight: 1,
			State:  v1alpha2.QueueStateOpen,
		},
		Status: v1alpha2.QueueStatus{
			State: v1alpha2.QueueStateOpen,
		},
	}

	openStateForDeleteJSON, err := json.Marshal(openStateForDelete)
	if err != nil {
		t.Errorf("Marshal queue for delete with open state failed for %v", err)
	}

	closedStateForDelete := v1alpha2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "closed-state-for-delete",
		},
		Spec: v1alpha2.QueueSpec{
			Weight: 1,
			State:  v1alpha2.QueueStateClosed,
		},
		Status: v1alpha2.QueueStatus{
			State: v1alpha2.QueueStateClosed,
		},
	}

	closedStateForDeleteJSON, err := json.Marshal(closedStateForDelete)
	if err != nil {
		t.Errorf("Marshal queue for delete with closed state failed for %v", err)
	}

	VolcanoClientSet = fakeclient.NewSimpleClientset()
	_, err = VolcanoClientSet.SchedulingV1alpha2().Queues().Create(&openStateForDelete)
	if err != nil {
		t.Errorf("Crate queue with open state failed for %v", err)
	}

	_, err = VolcanoClientSet.SchedulingV1alpha2().Queues().Create(&closedStateForDelete)
	if err != nil {
		t.Errorf("Crate queue with close state failed for %v", err)
	}

	defer func() {
		if err := VolcanoClientSet.SchedulingV1alpha2().Queues().Delete(openStateForDelete.Name, &v1.DeleteOptions{}); err != nil {
			fmt.Println(fmt.Sprintf("Delete queue with open state failed for %v", err))
		}
		if err := VolcanoClientSet.SchedulingV1alpha2().Queues().Delete(closedStateForDelete.Name, &v1.DeleteOptions{}); err != nil {
			fmt.Println(fmt.Sprintf("Delete queue with closed state failed for %v", err))
		}
	}()

	testCases := []struct {
		Name           string
		AR             v1beta1.AdmissionReview
		reviewResponse *v1beta1.AdmissionResponse
	}{
		{
			Name: "Normal Case State Not Set During Creating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "normal-case-not-set",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: stateNotSetJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Set State of Open During Creating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "normal-case-set-open",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: openStateJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Set State of Closed During Creating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "normal-case-set-closed",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: closedStateJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Abnormal Case Wrong State Configured During Creating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "abnormal-case",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: wrongStateJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("state"),
						"wrong", fmt.Sprintf("queue state must be in %v", []v1alpha2.QueueState{
							v1alpha2.QueueStateOpen,
							v1alpha2.QueueStateClosed,
						})).Error(),
				},
			},
		},
		{
			Name: "Normal Case Changing State From Open to Closed During Updating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "normal-case-open-to-close-updating",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: openStateJSON,
					},
					Object: runtime.RawExtension{
						Raw: closedStateJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Changing State From Closed to Open During Updating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "normal-case-closed-to-open-updating",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: closedStateJSON,
					},
					Object: runtime.RawExtension{
						Raw: openStateJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Abnormal Case Changing State From Open to Wrong State During Updating",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "abnormal-case-open-to-wrong-state-updating",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: openStateJSON,
					},
					Object: runtime.RawExtension{
						Raw: wrongStateJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("state"),
						"wrong", fmt.Sprintf("queue state must be in %v", []v1alpha2.QueueState{
							v1alpha2.QueueStateOpen,
							v1alpha2.QueueStateClosed,
						})).Error(),
				},
			},
		},
		{
			Name: "Normal Case Queue With Closed State Can Be Deleted",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "closed-state-for-delete",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: closedStateForDeleteJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Abnormal Case Queue With Open State Can Not Be Deleted",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "open-state-for-delete",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: openStateForDeleteJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("only queue with state %s can be deleted", v1alpha2.QueueStateClosed),
				},
			},
		},
		{
			Name: "Abnormal Case default Queue Can Not Be Deleted",
			AR: v1beta1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &v1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.sigs.dev",
						Version: "v1alpha2",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.sigs.dev",
						Version:  "v1alpha2",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: openStateForDeleteJSON,
					},
				},
			},
			reviewResponse: &v1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("%s queue can not be deleted", "default"),
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			reviewResponse := AdmitQueues(testCase.AR)
			if false == reflect.DeepEqual(reviewResponse, testCase.reviewResponse) {
				t.Errorf("Test case %s failed, expect %v, got %v", testCase.Name,
					reviewResponse, testCase.reviewResponse)
			}
		})
	}
}
