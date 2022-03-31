/*
Copyright 2018 The Volcano Authors.

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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func TestAdmitQueues(t *testing.T) {

	stateNotSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-not-set",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	stateNotSetJSON, err := json.Marshal(stateNotSet)
	if err != nil {
		t.Errorf("Marshal queue without state set failed for %v.", err)
	}

	openState := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-set-open",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}

	openStateJSON, err := json.Marshal(openState)
	if err != nil {
		t.Errorf("Marshal queue with open state failed for %v.", err)
	}

	closedState := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-set-closed",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateClosed,
		},
	}

	closedStateJSON, err := json.Marshal(closedState)
	if err != nil {
		t.Errorf("Marshal queue with closed state failed for %v.", err)
	}

	wrongState := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "abnormal-case",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: "wrong",
		},
	}

	wrongStateJSON, err := json.Marshal(wrongState)
	if err != nil {
		t.Errorf("Marshal queue with wrong state failed for %v.", err)
	}

	openStateForDelete := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-state-for-delete",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}

	openStateForDeleteJSON, err := json.Marshal(openStateForDelete)
	if err != nil {
		t.Errorf("Marshal queue for delete with open state failed for %v.", err)
	}

	closedStateForDelete := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "closed-state-for-delete",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateClosed,
		},
	}

	closedStateForDeleteJSON, err := json.Marshal(closedStateForDelete)
	if err != nil {
		t.Errorf("Marshal queue for delete with closed state failed for %v.", err)
	}

	weightNotSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "weight-not-set",
		},
		Spec: schedulingv1beta1.QueueSpec{},
	}

	weightNotSetJSON, err := json.Marshal(weightNotSet)
	if err != nil {
		t.Errorf("Marshal queue with no weight failed for %v.", err)
	}

	negativeWeight := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "negative-weight",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: -1,
		},
	}

	negativeWeightJSON, err := json.Marshal(negativeWeight)
	if err != nil {
		t.Errorf("Marshal queue with negative weight failed for %v.", err)
	}

	positiveWeightForUpdate := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "positive-weight-for-update",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}
	positiveWeightForUpdateJSON, err := json.Marshal(positiveWeightForUpdate)
	if err != nil {
		t.Errorf("Marshal queue with positive weight failed for %v.", err)
	}

	negativeWeightForUpdate := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "positive-weight-for-update",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: -1,
		},
	}

	negativeWeightForUpdateJSON, err := json.Marshal(negativeWeightForUpdate)
	if err != nil {
		t.Errorf("Marshal queue with negative weight failed for %v.", err)

	}

	hierarchyWeightsDontMatch := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hierarchy-weights-dont-match",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/a/b",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/2/3/4",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	hierarchyWeightsDontMatchJSON, err := json.Marshal(hierarchyWeightsDontMatch)
	if err != nil {
		t.Errorf("Marshal hierarchyWeightsDontMatch failed for %v.", err)
	}

	hierarchyWeightsNegative := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hierarchy-weights-dont-match",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/a/b",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/-1/3",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}
	hierarchyWeightsNegativeJSON, err := json.Marshal(hierarchyWeightsNegative)
	if err != nil {
		t.Errorf("Marshal weightsFormatNegative failed for %v.", err)
	}

	weightsFormatIllegal := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hierarchy-weights-dont-match",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/a/b",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/a/3",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	weightsFormatIllegalJSON, err := json.Marshal(weightsFormatIllegal)
	if err != nil {
		t.Errorf("Marshal weightsFormatIllegal failed for %v.", err)
	}

	ordinaryHierchicalQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ordinary-hierarchical-queue",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/node1/node2",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/2/3",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	hierarchicalQueueInSubPathOfAnotherQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hierarchical-queue-in-sub-path-of-another-queue",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/node1",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/4",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}
	hierarchicalQueueInSubPathOfAnotherQueueJSON, err := json.Marshal(hierarchicalQueueInSubPathOfAnotherQueue)
	if err != nil {
		t.Errorf("Marshal hierarchicalQueueInSubPathOfAnotherQueue failed for %v.", err)
	}

	config.VolcanoClient = fakeclient.NewSimpleClientset()
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &openStateForDelete, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create queue with open state failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &closedStateForDelete, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create queue with closed state failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &ordinaryHierchicalQueue, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create hierarchical queue failed for %v.", err)
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &positiveWeightForUpdate, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Crate queue with positive weight failed for %v.", err)
	}

	defer func() {
		if err := config.VolcanoClient.SchedulingV1beta1().Queues().Delete(context.TODO(), openStateForDelete.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Delete queue with open state failed for %v.\n", err)
		}
		if err := config.VolcanoClient.SchedulingV1beta1().Queues().Delete(context.TODO(), closedStateForDelete.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Delete queue with closed state failed for %v.\n", err)
		}
		if err := config.VolcanoClient.SchedulingV1beta1().Queues().Delete(context.TODO(), ordinaryHierchicalQueue.Name, metav1.DeleteOptions{}); err != nil {
			t.Errorf("Delete hierarchical queue failed for %v.", err)
		}
	}()

	testCases := []struct {
		Name           string
		AR             admissionv1.AdmissionReview
		reviewResponse *admissionv1.AdmissionResponse
	}{
		{
			Name: "Normal Case State Not Set During Creating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "normal-case-not-set",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: stateNotSetJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Set State of Open During Creating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "normal-case-set-open",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: openStateJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Set State of Closed During Creating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "normal-case-set-closed",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: closedStateJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Abnormal Case Wrong State Configured During Creating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "abnormal-case",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: wrongStateJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("state"),
						"wrong", fmt.Sprintf("queue state must be in %v", []schedulingv1beta1.QueueState{
							schedulingv1beta1.QueueStateOpen,
							schedulingv1beta1.QueueStateClosed,
						})).Error(),
				},
			},
		},
		{
			Name: "Normal Case Changing State From Open to Closed During Updating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
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
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Changing State From Closed to Open During Updating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
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
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Abnormal Case Changing State From Open to Wrong State During Updating",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
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
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("state"),
						"wrong", fmt.Sprintf("queue state must be in %v", []schedulingv1beta1.QueueState{
							schedulingv1beta1.QueueStateOpen,
							schedulingv1beta1.QueueStateClosed,
						})).Error(),
				},
			},
		},
		{
			Name: "Normal Case Queue With Closed State Can Be Deleted",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "closed-state-for-delete",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: closedStateForDeleteJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Normal Case Queue With Open State Can Be Deleted (Until close queue in kubectl supported)",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "open-state-for-delete",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: openStateForDeleteJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Abnormal Case default Queue Can Not Be Deleted",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: openStateForDeleteJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("`%s` queue can not be deleted", "default"),
				},
			},
		},
		{
			Name: "Invalid Action",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "Invalid",
					Object: runtime.RawExtension{
						Raw: openStateForDeleteJSON,
					},
				},
			},
			reviewResponse: util.ToAdmissionResponse(fmt.Errorf("invalid operation `%s`, "+
				"expect operation to be `CREATE`, `UPDATE` or `DELETE`", "Invalid")),
		},
		{
			Name: "Create queue without weight",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "weight-not-set",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: weightNotSetJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("weight"),
						0, "queue weight must be a positive integer").Error(),
				},
			},
		},
		{
			Name: "Create queue with negative weight",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "negative-weight",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: negativeWeightJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("weight"),
						-1, "queue weight must be a positive integer").Error(),
				},
			},
		},
		{
			Name: "Update queue with negative weight",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "positive-weight-for-update",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: positiveWeightForUpdateJSON,
					},
					Object: runtime.RawExtension{
						Raw: negativeWeightForUpdateJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("weight"),
						-1, "queue weight must be a positive integer").Error(),
				},
			},
		},

		{
			Name: "Abnormal Case Hierarchy And Weights Do Not Match",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: hierarchyWeightsDontMatchJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("metadata").Child("annotations"),
						"root/a/b", fmt.Sprintf("%s must have the same length with %s",
							schedulingv1beta1.KubeHierarchyAnnotationKey,
							schedulingv1beta1.KubeHierarchyWeightAnnotationKey,
						)).Error(),
				},
			},
		},
		{
			Name: "Abnormal Case Weights Is Negative",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: hierarchyWeightsNegativeJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("metadata").Child("annotations"),
						"1/-1/3",
						fmt.Sprintf("%s in the %s must be larger than 0",
							"-1", "1/-1/3",
						)).Error(),
				},
			},
		},
		{
			Name: "Abnormal Case Weights Is Format Illegal",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: weightsFormatIllegalJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("metadata").Child("annotations"),
						"1/a/3",
						fmt.Sprintf("%s in the %s is invalid number: strconv.ParseFloat: parsing \"a\": invalid syntax",
							"a", "1/a/3",
						)).Error(),
				},
			},
		},
		{
			Name: "Abnormal Case Hierarchy Is In Sub Path of Another Queue",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: "v1beta1",
						Kind:    "Queue",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "scheduling.volcano.sh",
						Version:  "v1beta1",
						Resource: "queues",
					},
					Name:      "default",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: hierarchicalQueueInSubPathOfAnotherQueueJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("metadata").Child("annotations"),
						"root/node1",
						fmt.Sprintf("%s is not allowed to be in the sub path of %s of queue %s",
							"root/node1", "root/node1/node2", ordinaryHierchicalQueue.Name,
						)).Error(),
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			reviewResponse := AdmitQueues(testCase.AR)
			if !reflect.DeepEqual(reviewResponse, testCase.reviewResponse) {
				t.Errorf("Test case %s failed, expect %v, got %v", testCase.Name,
					testCase.reviewResponse, reviewResponse)
			}
		})
	}
}
