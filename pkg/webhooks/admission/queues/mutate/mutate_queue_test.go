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

package mutate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	webconfig "volcano.sh/volcano/pkg/webhooks/config"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func TestMutateQueues(t *testing.T) {
	trueValue := true
	admissionConfigData := &webconfig.AdmissionConfiguration{}
	config.ConfigData = admissionConfigData
	stateNotSetReclaimableNotSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-refresh-default-state",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	stateNotSetJSON, err := json.Marshal(stateNotSetReclaimableNotSet)
	if err != nil {
		t.Errorf("Marshal queue without state set failed for %v.", err)
	}

	openStateReclaimableSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-case-set-open",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:      1,
			Reclaimable: &trueValue,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}

	openStateJSON, err := json.Marshal(openStateReclaimableSet)
	if err != nil {
		t.Errorf("Marshal queue with open state failed for %v.", err)
	}

	pt := admissionv1.PatchTypeJSONPatch

	var refreshPatch []patchOperation
	refreshPatch = append(refreshPatch, patchOperation{
		Op:    "add",
		Path:  "/spec/reclaimable",
		Value: &trueValue,
	})

	refreshPatchJSON, err := json.Marshal(refreshPatch)
	if err != nil {
		t.Errorf("Marshal queue patch failed for %v.", err)
	}

	hierarchyWithoutRoot := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-without-root",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "a/b/c",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "2/3/4",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Reclaimable: &trueValue,
			Weight:      1,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}

	hierarchyWithoutRootJSON, err := json.Marshal(hierarchyWithoutRoot)
	if err != nil {
		t.Errorf("Marshal hierarchy without root failed for %v.", err)
	}
	var appendRootPatch []patchOperation
	appendRootPatch = append(appendRootPatch, patchOperation{
		Op:    "add",
		Path:  fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(schedulingv1beta1.KubeHierarchyAnnotationKey, "/", "~1")),
		Value: fmt.Sprintf("root/%s", hierarchyWithoutRoot.Annotations[schedulingv1beta1.KubeHierarchyAnnotationKey]),
	})
	appendRootPatch = append(appendRootPatch, patchOperation{
		Op:    "add",
		Path:  fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(schedulingv1beta1.KubeHierarchyWeightAnnotationKey, "/", "~1")),
		Value: fmt.Sprintf("1/%s", hierarchyWithoutRoot.Annotations[schedulingv1beta1.KubeHierarchyWeightAnnotationKey]),
	})
	appendRootPatchJSON, err := json.Marshal(appendRootPatch)
	if err != nil {
		t.Errorf("Marshal appendRootPatch failed for %v", err)
	}

	testCases := []struct {
		Name           string
		AR             admissionv1.AdmissionReview
		reviewResponse *admissionv1.AdmissionResponse
	}{
		{
			Name: "Normal Case Refresh Default Open State and Reclaimable For Queue",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "normal-case-refresh-default-state",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: stateNotSetJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed:   true,
				PatchType: &pt,
				Patch:     refreshPatchJSON,
			},
		},
		{
			Name: "Invalid Action",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Operation: "Invalid",
					Object: runtime.RawExtension{
						Raw: openStateJSON,
					},
				},
			},
			reviewResponse: util.ToAdmissionResponse(fmt.Errorf("invalid operation `%s`, "+
				"expect operation to be `CREATE`", "Invalid")),
		},
		{
			Name: "Normal Case Append Default Root to The HDRF Attributes",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "qeueu-hierarchy-without-root",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: hierarchyWithoutRootJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed:   true,
				PatchType: &pt,
				Patch:     appendRootPatchJSON,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			reviewResponse := Queues(testCase.AR)
			if !equality.Semantic.DeepEqual(reviewResponse, testCase.reviewResponse) {
				t.Errorf("Test case '%s' failed, expect: %v, got: %v", testCase.Name,
					testCase.reviewResponse, reviewResponse)
			}
		})
	}
}

func TestMutateHierarchicalQueues(t *testing.T) {
	admissionConfigData := &webconfig.AdmissionConfiguration{EnableHierarchyCapacity: true}
	config.ConfigData = admissionConfigData
	config.VolcanoClient = fakeclient.NewSimpleClientset()
	trueValue := true

	// case 0: Normal Case Append Parent Queue Label to The Capacity Attributes
	parentLabel := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-label-queue",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent:      "root",
			Weight:      1,
			Reclaimable: &trueValue,
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateClosing,
		},
	}
	parentLabelJSON, err := json.Marshal(parentLabel)
	if err != nil {
		t.Errorf("Marshal queue failed for %v.", err)
	}

	pt := admissionv1.PatchTypeJSONPatch

	var parentLablePatch []patchOperation
	parentLablePatch = append(parentLablePatch, patchOperation{
		Op:   "replace",
		Path: "/metadata/labels",
		Value: map[string]string{
			KubeParentQueueLabelKey: "root",
		},
	})

	parentLablePatchJSON, err := json.Marshal(parentLablePatch)
	if err != nil {
		t.Errorf("Marshal queue patch failed for %v.", err)
	}

	rootqueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "root",
		},
		Spec: schedulingv1beta1.QueueSpec{},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &rootqueue, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create root queue failed for %v.", err)
	}

	// case 1: Normal Case Close Queue
	closeState := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "close-state-queue",
			Labels: map[string]string{
				"volcano.sh/parent-queue": "root",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateClosing,
		},
	}

	closeStateJSON, err := json.Marshal(closeState)
	if err != nil {
		t.Errorf("Marshal queue with close state failed for %v.", err)
	}

	var closeStatePatch []patchOperation
	closeStatePatch = append(closeStatePatch, patchOperation{
		Op:    "replace",
		Path:  fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(KubeParentQueueLabelKey, "/", "~1")),
		Value: "root",
	})

	closeStatePatchJSON, err := json.Marshal(closeStatePatch)
	if err != nil {
		t.Errorf("Marshal queue patch failed for %v.", err)
	}

	// case 2: Abnormal Case Close Root Queue
	rootQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "root",
		},
		Spec: schedulingv1beta1.QueueSpec{},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateClosing,
		},
	}
	closeRootJSON, err := json.Marshal(rootQueue)
	if err != nil {
		t.Errorf("Marshal root queue with close state failed for %v.", err)
	}

	// case 3: Normal Case Open Queue
	openParentQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-parent-queue",
			Labels: map[string]string{
				"volcano.sh/parent-queue": "root",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &openParentQueue, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue failed for %v.", err)
	}

	openState := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-state-queue",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:      1,
			Reclaimable: &trueValue,
			Parent:      "root",
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}

	openStateJSON, err := json.Marshal(openState)
	if err != nil {
		t.Errorf("Marshal queue with open state failed for %v.", err)
	}

	var openStatePatch []patchOperation
	openStatePatch = append(openStatePatch, patchOperation{
		Op:   "replace",
		Path: "/metadata/labels",
		Value: map[string]string{
			KubeParentQueueLabelKey: "root",
		},
	})

	openStatePatchJSON, err := json.Marshal(openStatePatch)
	if err != nil {
		t.Errorf("Marshal queue patch failed for %v.", err)
	}

	// case 4: Abnormal Case Open Queue with Closing Parent Queue
	closingParentQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "closing-parent-queue",
			Labels: map[string]string{
				"volcano.sh/parent-queue": "root",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
	}
	queue, err := config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &closingParentQueue, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue failed for %v.", err)
	}
	queue.Status.State = schedulingv1beta1.QueueStateClosing
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), queue, metav1.UpdateOptions{})
	if err != nil {
		t.Errorf("Update parent queue failed for %v.", err)
	}

	openStateWithClosingParent := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "abnormal-open-state-queue",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "closing-parent-queue",
		},
		Status: schedulingv1beta1.QueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
		},
	}

	openStateWithClosingParentJSON, err := json.Marshal(openStateWithClosingParent)
	if err != nil {
		t.Errorf("Marshal queue with open state failed for %v.", err)
	}

	testCases := []struct {
		Name           string
		AR             admissionv1.AdmissionReview
		reviewResponse *admissionv1.AdmissionResponse
	}{
		{
			Name: "Normal Case Append Parent Queue Label to The Capacity Attributes",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "normal-case-set-parent-label",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: parentLabelJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed:   true,
				PatchType: &pt,
				Patch:     parentLablePatchJSON,
			},
		},
		{
			Name: "Normal Case Close queue",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "normal-case-close-queue",
					Operation: "UPDATE",
					Object: runtime.RawExtension{
						Raw: closeStateJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed:   true,
				PatchType: &pt,
				Patch:     closeStatePatchJSON,
			},
		},
		{
			Name: "Abnormal Case Close Root Queue",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "abnormal-case-close-root-queue",
					Operation: "UPDATE",
					Object: runtime.RawExtension{
						Raw: closeRootJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "root queue can not be closed",
				},
			},
		},
		{
			Name: "Normal Case Open Queue",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "normal-case-open-queue",
					Operation: "UPDATE",
					Object: runtime.RawExtension{
						Raw: openStateJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed:   true,
				PatchType: &pt,
				Patch:     openStatePatchJSON,
			},
		},
		{
			Name: "Abnormal Case Open Queue with Closing Parent Queue",
			AR: admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
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
					Name:      "abnormal-case-open-queue",
					Operation: "UPDATE",
					Object: runtime.RawExtension{
						Raw: openStateWithClosingParentJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "failed to create/update open queue abnormal-open-state-queue because of its closed/closing parent queue closing-parent-queue",
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			reviewResponse := Queues(testCase.AR)
			if !equality.Semantic.DeepEqual(reviewResponse, testCase.reviewResponse) {
				t.Errorf("Test case '%s' failed, expect: %v, got: %v", testCase.Name,
					testCase.reviewResponse, reviewResponse)
			}
		})
	}

}
