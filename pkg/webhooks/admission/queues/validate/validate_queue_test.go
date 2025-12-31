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
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/cache"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	volcanoversioned "volcano.sh/apis/pkg/client/clientset/versioned"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
	schedulingv1beta1informers "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/webhooks/router"
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

	// Note: weight validation test cases removed as validation is now enforced by CRD schema.
	// However, we still need positiveWeightForUpdate for test setup.
	positiveWeightForUpdate := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "positive-weight-for-update",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	resourceNotSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resource-not-set",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}

	resourceNotSetJSON, err := json.Marshal(resourceNotSet)
	if err != nil {
		t.Errorf("Marshal resourceNotSet failed for %v.", err)

	}

	onlyDeservedSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "only-deserved-set",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
			Deserved: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}

	onlyDeservedSetJSON, err := json.Marshal(onlyDeservedSet)
	if err != nil {
		t.Errorf("Marshal onlyDeservedSet failed for %v.", err)

	}

	onlyGuaranteeSet := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "only-guarantee-set",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
	}

	onlyGuaranteeSetJSON, err := json.Marshal(onlyGuaranteeSet)
	if err != nil {
		t.Errorf("Marshal onlyGuaranteeSet failed for %v.", err)
	}

	capabilityLessDeserved := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capability-less-deserved",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
			Capability: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Deserved: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}

	capabilityLessDeservedJSON, err := json.Marshal(capabilityLessDeserved)
	if err != nil {
		t.Errorf("Marshal capabilityLessDeserved failed for %v.", err)
	}

	deservedLessGuarantee := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "deserved-less-guarantee",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
			Deserved: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
		},
	}

	deservedLessGuaranteeJSON, err := json.Marshal(deservedLessGuarantee)
	if err != nil {
		t.Errorf("Marshal deservedLessGuarantee failed for %v.", err)
	}

	capabilityLessGuarantee := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capability-less-guarantee",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
			Capability: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
		},
	}

	capabilityLessGuaranteeJSON, err := json.Marshal(capabilityLessGuarantee)
	if err != nil {
		t.Errorf("Marshal capabilityLessGuarantee failed for %v.", err)
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

	hierarchicalQueueWithNameThatIsSubstringOfOtherQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hierarchical-queue-with-name-that-is-substring-of-other-queue",
			Annotations: map[string]string{
				schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/node",
				schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/4",
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 1,
		},
	}
	hierarchicalQueueWithNameThatIsSubstringOfOtherQueueJSON, err := json.Marshal(hierarchicalQueueWithNameThatIsSubstringOfOtherQueue)
	if err != nil {
		t.Errorf("Marshal  hierarchicalQueueWithNameThatIsSubstringOfOtherQueue failed for %v.", err)
	}
	config.VolcanoClient = fakeclient.NewSimpleClientset()
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
			Name: "Normal Case Hierarchy Is A Substring of Another Queue",
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
						Raw: hierarchicalQueueWithNameThatIsSubstringOfOtherQueueJSON,
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
		// Note: weight validation (weight >= 1) is now enforced by CRD schema (minimum: 1).
		// These test cases are removed as the validation happens before the webhook is called.
		// In a real environment, CRD validation would reject these requests before they reach the webhook.
		{
			Name: "Create queue without resource",
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
						Raw: resourceNotSetJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Create queue with deserved resource",
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
						Raw: onlyDeservedSetJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Create queue with guarantee but no deserved should be rejected",
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
						Raw: onlyGuaranteeSetJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("guarantee"),
						api.NewResource(
							v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("1Gi"),
							}).String(),
						"guarantee should less equal than deserved").Error(),
				},
			},
		},
		{
			Name: "Create queue with capability less deserved",
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
						Raw: capabilityLessDeservedJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("deserved"),
						api.NewResource(
							v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2"),
								v1.ResourceMemory: resource.MustParse("1Gi"),
							}).String(),
						"deserved should less equal than capability").Error(),
				},
			},
		},
		{
			Name: "Create queue with capability less guarantee",
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
						Raw: capabilityLessGuaranteeJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("guarantee"),
						api.NewResource(
							v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2"),
								v1.ResourceMemory: resource.MustParse("3Gi"),
							}).String(),
						"guarantee should less equal than capability").Error(),
				},
			},
		},
		{
			Name: "Create queue with deserved less guarantee",
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
						Raw: deservedLessGuaranteeJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: field.Invalid(field.NewPath("requestBody").Child("spec").Child("guarantee"),
						api.NewResource(
							v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2"),
								v1.ResourceMemory: resource.MustParse("3Gi"),
							}).String(),
						"guarantee should less equal than deserved").Error(),
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
			if !equality.Semantic.DeepEqual(reviewResponse, testCase.reviewResponse) {
				t.Errorf("Test case %s failed, expect %v, got %v", testCase.Name,
					testCase.reviewResponse, reviewResponse)
			}
		})
	}
	close(stopCh)
}

func TestAdmitHierarchicalQueues(t *testing.T) {
	parentQueueWithJobs := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-with-jobs",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "queue-with-jobs",
			Weight: 1,
		},
	}

	parentQueueWithJobsJSON, err := json.Marshal(parentQueueWithJobs)
	if err != nil {
		t.Errorf("Marshal queue with parent queue failed for %v.", err)
	}

	parentQueueWithoutJobs := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-without-jobs",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "queue-without-jobs",
			Weight: 1,
		},
	}

	parentQueueWithoutJobsJSON, err := json.Marshal(parentQueueWithoutJobs)
	if err != nil {
		t.Errorf("Marshal queue with parent queue failed for %v.", err)
	}

	queueWithChild := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-with-child-queues",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
	}
	queueWithChildJSON, err := json.Marshal(queueWithChild)
	if err != nil {
		t.Errorf("Marshal queue with child queue failed for %v.", err)
	}

	queueWithoutChild := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-without-child-queues",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
	}
	queueWithoutChildJSON, err := json.Marshal(queueWithoutChild)
	if err != nil {
		t.Errorf("Marshal queue with child queue failed for %v.", err)
	}

	config.VolcanoClient = fakeclient.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(config.VolcanoClient, 0)

	// Setup queue informer with parent index
	queueInformer := setupQueueInformerWithIndex(informerFactory)
	config.QueueInformer = queueInformer
	config.QueueLister = informerFactory.Scheduling().V1beta1().Queues().Lister()

	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	for informerType, ok := range informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			panic(fmt.Errorf("failed to sync cache: %v", informerType))
		}
	}

	queueWithJobs := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-with-jobs",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
		Status: schedulingv1beta1.QueueStatus{
			Allocated: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("1"),
			},
		},
	}

	queueWithoutJobs := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-without-jobs",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
		Status: schedulingv1beta1.QueueStatus{
			Allocated: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("0"),
			},
		},
	}

	childQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-queue",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "queue-with-child-queues",
		},
	}

	parentQueueWithChildWithJobs := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-has-child-with-jobs",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
		},
		Status: schedulingv1beta1.QueueStatus{
			Allocated: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("1"),
			},
		},
	}

	childQueueAWithJobs := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-queue-a-with-jobs",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-has-child-with-jobs",
			Weight: 1,
		},
		Status: schedulingv1beta1.QueueStatus{
			Allocated: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("1"),
			},
		},
	}

	childQueueB := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-queue-b",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-has-child-with-jobs",
			Weight: 1,
		},
	}

	childQueueBJSON, err := json.Marshal(childQueueB)
	if err != nil {
		t.Errorf("Marshal queue with parent queue failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &queueWithJobs, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create queue with jobs failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &queueWithoutJobs, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create queue without jobs failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &childQueue, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create child queue failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &queueWithChild, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create queue failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &parentQueueWithChildWithJobs, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &childQueueAWithJobs, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create child queue with jobs failed for %v.", err)
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &queueWithoutChild, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create queue failed for %v.", err)
	}

	selfReferencingQueue := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "self-referencing-queue",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "self-referencing-queue",
			Weight: 1,
		},
	}

	selfReferencingQueueJSON, err := json.Marshal(selfReferencingQueue)
	if err != nil {
		t.Errorf("Marshal self-referencing queue failed for %v.", err)
	}

	// Test queues with negative resource values
	queueWithNegativeCapability := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-negative-capability",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("-10"),
			},
		},
	}
	queueWithNegativeCapabilityJSON, err := json.Marshal(queueWithNegativeCapability)
	if err != nil {
		t.Errorf("Marshal queue with negative capability failed for %v.", err)
	}

	queueWithNegativeDeserved := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-negative-deserved",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
			Weight: 1,
			Deserved: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("-5"),
			},
		},
	}
	queueWithNegativeDeservedJSON, err := json.Marshal(queueWithNegativeDeserved)
	if err != nil {
		t.Errorf("Marshal queue with negative deserved failed for %v.", err)
	}

	queueWithNegativeGuarantee := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue-negative-guarantee",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
			Weight: 1,
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("-2"),
				},
			},
		},
	}
	queueWithNegativeGuaranteeJSON, err := json.Marshal(queueWithNegativeGuarantee)
	if err != nil {
		t.Errorf("Marshal queue with negative guarantee failed for %v.", err)
	}

	// Create parent queue for resource validation tests
	parentQueueForResourceTest := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-resource-test",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("10"),
				v1.ResourceMemory: resource.MustParse("20Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("8"),
					v1.ResourceMemory: resource.MustParse("16Gi"),
				},
			},
		},
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &parentQueueForResourceTest, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue for resource test failed for %v.", err)
	}

	// Create a sibling queue for resource validation tests
	siblingQueueForResourceTest := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sibling-queue-resource-test",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-resource-test",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("3"),
				v1.ResourceMemory: resource.MustParse("6Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
	}

	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &siblingQueueForResourceTest, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create sibling queue for resource test failed for %v.", err)
	}

	// Test queue objects
	childCapabilityExceedsParent := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-capability-exceeds-parent",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-resource-test",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("20"),
				v1.ResourceMemory: resource.MustParse("40Gi"),
			},
		},
	}
	childCapabilityExceedsParentJSON, err := json.Marshal(childCapabilityExceedsParent)
	if err != nil {
		t.Errorf("Marshal child capability exceeds parent failed for %v.", err)
	}

	siblingsGuaranteeExceedsParent := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "siblings-guarantee-exceeds",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-resource-test",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("7"),
				v1.ResourceMemory: resource.MustParse("14Gi"),
			},
			Deserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("7"),
				v1.ResourceMemory: resource.MustParse("14Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("7"),
					v1.ResourceMemory: resource.MustParse("14Gi"),
				},
			},
		},
	}
	siblingsGuaranteeExceedsParentJSON, err := json.Marshal(siblingsGuaranteeExceedsParent)
	if err != nil {
		t.Errorf("Marshal siblings guarantee exceeds parent failed for %v.", err)
	}

	// Create another parent queue with smaller capability for testing parent's capability validation
	parentQueueSmallCapability := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-small-capability",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("5"),
				v1.ResourceMemory: resource.MustParse("10Gi"),
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &parentQueueSmallCapability, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue with small capability failed for %v.", err)
	}

	// Create a child queue with larger capability than parent
	childQueueLargerCapability := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-queue-larger-capability",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-small-capability",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("10"),
				v1.ResourceMemory: resource.MustParse("20Gi"),
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &childQueueLargerCapability, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create child queue with larger capability failed for %v.", err)
	}

	// Create a queue that will be updated to have capability less than its children
	parentQueueToUpdate := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-to-update",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "root",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("20"),
				v1.ResourceMemory: resource.MustParse("40Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("15"),
					v1.ResourceMemory: resource.MustParse("30Gi"),
				},
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &parentQueueToUpdate, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue to update failed for %v.", err)
	}

	// Create child queues under parentQueueToUpdate
	childQueueA := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-queue-a",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-to-update",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("12"),
				v1.ResourceMemory: resource.MustParse("24Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("8"),
					v1.ResourceMemory: resource.MustParse("16Gi"),
				},
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &childQueueA, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create child queue A failed for %v.", err)
	}

	childQueueBForUpdate := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-queue-b-for-update",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-to-update",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("8"),
				v1.ResourceMemory: resource.MustParse("16Gi"),
			},
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("5"),
					v1.ResourceMemory: resource.MustParse("10Gi"),
				},
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &childQueueBForUpdate, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create child queue B for update failed for %v.", err)
	}

	parentQueueUpdatedSmallerCapability := parentQueueToUpdate.DeepCopy()
	parentQueueUpdatedSmallerCapability.Spec.Capability = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("10"),
		v1.ResourceMemory: resource.MustParse("20Gi"),
	}
	parentQueueUpdatedSmallerCapability.Spec.Deserved = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("10"),
		v1.ResourceMemory: resource.MustParse("20Gi"),
	}
	parentQueueUpdatedSmallerCapability.Spec.Guarantee = schedulingv1beta1.Guarantee{
		Resource: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("8"),
			v1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}
	parentQueueUpdatedSmallerCapabilityJSON, err := json.Marshal(parentQueueUpdatedSmallerCapability)
	if err != nil {
		t.Errorf("Marshal parent queue updated smaller capability failed for %v.", err)
	}

	// Update the parent queue to have smaller guarantee than sum of children's guarantee
	// Keep capability >= all children's capability, but guarantee < sum of children's guarantee
	parentQueueUpdatedSmallerGuarantee := parentQueueToUpdate.DeepCopy()
	parentQueueUpdatedSmallerGuarantee.Spec.Capability = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("20"), // Still >= child-queue-a (12)
		v1.ResourceMemory: resource.MustParse("40Gi"),
	}
	parentQueueUpdatedSmallerGuarantee.Spec.Deserved = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("20"),
		v1.ResourceMemory: resource.MustParse("40Gi"),
	}
	parentQueueUpdatedSmallerGuarantee.Spec.Guarantee = schedulingv1beta1.Guarantee{
		Resource: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("10"),   // < 8 + 5 = 13
			v1.ResourceMemory: resource.MustParse("20Gi"), // < 16 + 10 = 26
		},
	}
	parentQueueUpdatedSmallerGuaranteeJSON, err := json.Marshal(parentQueueUpdatedSmallerGuarantee)
	if err != nil {
		t.Errorf("Marshal parent queue updated smaller guarantee failed for %v.", err)
	}

	parentQueueToUpdateOldJSON, err := json.Marshal(parentQueueToUpdate)
	if err != nil {
		t.Errorf("Marshal parent queue to update old failed for %v.", err)
	}

	// Test queue with parent="" (empty parent)
	// This queue should still validate child constraints when updated
	parentQueueEmptyParent := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent-queue-empty-parent",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "", // Empty parent (equivalent to root)
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100"),
				v1.ResourceMemory: resource.MustParse("100Gi"),
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &parentQueueEmptyParent, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create parent queue with empty parent failed for %v.", err)
	}

	childQueueOfEmptyParent := schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child-of-empty-parent",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Parent: "parent-queue-empty-parent",
			Weight: 1,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("50"),
				v1.ResourceMemory: resource.MustParse("50Gi"),
			},
		},
	}
	_, err = config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &childQueueOfEmptyParent, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Create child queue of empty parent failed for %v.", err)
	}

	// Update parent queue to have capability less than children (should FAIL)
	updatedParentEmptyParent := parentQueueEmptyParent.DeepCopy()
	updatedParentEmptyParent.Spec.Capability = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("30"),
		v1.ResourceMemory: resource.MustParse("30Gi"),
	}
	updatedParentEmptyParentJSON, err := json.Marshal(updatedParentEmptyParent)
	if err != nil {
		t.Errorf("Marshal updated parent queue with empty parent failed for %v.", err)
	}
	parentQueueEmptyParentJSON, err := json.Marshal(parentQueueEmptyParent)
	if err != nil {
		t.Errorf("Marshal original parent queue with empty parent failed for %v.", err)
	}

	testCases := []struct {
		Name           string
		AR             admissionv1.AdmissionReview
		reviewResponse *admissionv1.AdmissionResponse
	}{
		{
			Name: "Queue cannot use itself as parent",
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
					Name:      "self-referencing-queue",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: selfReferencingQueueJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue self-referencing-queue cannot use itself as parent",
				},
			},
		},
		{
			Name: "Parent Queue has jobs",
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
					Name:      "parent-queue-with-jobs",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: parentQueueWithJobsJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue queue-with-jobs cannot be the parent queue of queue parent-queue-with-jobs because it has allocated Pods: 1",
				},
			},
		},
		{
			Name: "Parent Queue has no jobs",
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
					Name:      "parent-queue-without-jobs",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: parentQueueWithoutJobsJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Parent Queue has child with jobs",
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
					Name:      "child-queue-b",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: childQueueBJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Delete queue with child queue",
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
					Name:      "queue-with-child-queues",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: queueWithChildJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue queue-with-child-queues can not be deleted because it has 1 child queues: child-queue",
				},
			},
		},
		{
			Name: "Delete queue without child queue",
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
					Name:      "queue-without-child-queues",
					Operation: "DELETE",
					Object: runtime.RawExtension{
						Raw: queueWithoutChildJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			Name: "Child capability exceeds parent capability",
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
					Name:      "child-capability-exceeds-parent",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: childCapabilityExceedsParentJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue child-capability-exceeds-parent capability (cpu 20000.00, memory 42949672960.00) exceeds parent queue parent-queue-resource-test capability (cpu 10000.00, memory 21474836480.00)",
				},
			},
		},
		{
			Name: "Siblings guarantee sum exceeds parent guarantee",
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
					Name:      "siblings-guarantee-exceeds",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: siblingsGuaranteeExceedsParentJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "parent queue parent-queue-resource-test validation failed: sum of children's guarantee (cpu 9000.00, memory 19327352832.00) exceeds parent's guarantee limit (cpu 8000.00, memory 17179869184.00)",
				},
			},
		},
		{
			Name: "Parent capability less than child capability on UPDATE",
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
					Name:      "parent-queue-to-update",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: parentQueueToUpdateOldJSON,
					},
					Object: runtime.RawExtension{
						Raw: parentQueueUpdatedSmallerCapabilityJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue parent-queue-to-update capability (cpu 10000.00, memory 21474836480.00) is less than child queue child-queue-a capability (cpu 12000.00, memory 25769803776.00)",
				},
			},
		},
		{
			Name: "Sum of children's guarantee exceeds parent guarantee on UPDATE",
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
					Name:      "parent-queue-to-update",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: parentQueueToUpdateOldJSON,
					},
					Object: runtime.RawExtension{
						Raw: parentQueueUpdatedSmallerGuaranteeJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue parent-queue-to-update validation failed: sum of children's guarantee (cpu 13000.00, memory 27917287424.00) exceeds parent's guarantee limit (cpu 10000.00, memory 21474836480.00)",
				},
			},
		},
		{
			Name: "Update queue with empty parent - should validate child constraints",
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
					Name:      "parent-queue-empty-parent",
					Operation: "UPDATE",
					OldObject: runtime.RawExtension{
						Raw: parentQueueEmptyParentJSON,
					},
					Object: runtime.RawExtension{
						Raw: updatedParentEmptyParentJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "queue parent-queue-empty-parent capability (cpu 30000.00, memory 32212254720.00) is less than child queue child-of-empty-parent capability (cpu 50000.00, memory 53687091200.00)",
				},
			},
		},
		{
			Name: "Queue with negative capability should be rejected",
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
					Name:      "queue-negative-capability",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: queueWithNegativeCapabilityJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "[requestBody.spec.capability.cpu: Invalid value: \"-10\": must be greater than or equal to 0, requestBody.spec.deserved: Invalid value: \"cpu 0.00, memory 0.00\": deserved should less equal than capability]",
				},
			},
		},
		{
			Name: "Queue with negative deserved should be rejected",
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
					Name:      "queue-negative-deserved",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: queueWithNegativeDeservedJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "requestBody.spec.deserved.cpu: Invalid value: \"-5\": must be greater than or equal to 0",
				},
			},
		},
		{
			Name: "Queue with negative guarantee should be rejected",
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
					Name:      "queue-negative-guarantee",
					Operation: "CREATE",
					Object: runtime.RawExtension{
						Raw: queueWithNegativeGuaranteeJSON,
					},
				},
			},
			reviewResponse: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "requestBody.spec.guarantee.resource.cpu: Invalid value: \"-2\": must be greater than or equal to 0",
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			reviewResponse := AdmitQueues(testCase.AR)
			if !equality.Semantic.DeepEqual(reviewResponse, testCase.reviewResponse) {
				t.Errorf("Test case %s failed, expect %v, got %v", testCase.Name,
					testCase.reviewResponse, reviewResponse)
			}
		})
	}
	close(stopCh)
}

// setupQueueInformerWithIndex creates a queue informer with parent index for testing
func setupQueueInformerWithIndex(factory informers.SharedInformerFactory) cache.SharedIndexInformer {
	queueInformer := factory.InformerFor(&schedulingv1beta1.Queue{},
		func(c volcanoversioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
			return schedulingv1beta1informers.NewQueueInformer(
				c,
				resyncPeriod,
				cache.Indexers{
					cache.NamespaceIndex:        cache.MetaNamespaceIndexFunc,
					router.QueueParentIndexName: router.QueueParentIndexFunc,
				},
			)
		})
	return queueInformer
}
