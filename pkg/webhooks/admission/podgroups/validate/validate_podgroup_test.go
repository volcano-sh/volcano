/*
Copyright 2026 The Volcano Authors.

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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/features"
)

func TestValidatePodGroup(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	highestTierAllowed := 1
	tests := []struct {
		name           string
		podGroup       *schedulingv1beta1.PodGroup
		queue          *schedulingv1beta1.Queue
		namespaceQueue *schedulingv1beta1.NamespaceQueue
		expectError    bool
		// msgContains lists substrings that must all be present in the
		// rejection message, used to assert that multiple validation errors
		// are reported and properly separated.
		msgContains []string
	}{
		{
			name: "valid podgroup with open queue",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "test-queue",
				},
			},
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue",
				},
				Status: schedulingv1beta1.QueueStatus{
					State: schedulingv1beta1.QueueStateOpen,
				},
			},
			expectError: false,
		},
		{
			name: "invalid podgroup with closed queue",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "test-queue",
				},
			},
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue",
				},
				Status: schedulingv1beta1.QueueStatus{
					State: schedulingv1beta1.QueueStateClosed,
				},
			},
			expectError: true,
		},
		{
			name: "valid podgroup with empty queue",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "",
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: false,
		},
		{
			name: "invalid podgroup with a queue that does not exist",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "test-queue",
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: true,
		},
		{
			name: "valid podgroup with ready namespace queue",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "team-a",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "namespace/training",
				},
			},
			queue: &schedulingv1beta1.Queue{},
			namespaceQueue: &schedulingv1beta1.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "training",
					Namespace:  "team-a",
					Generation: 2,
				},
				Status: schedulingv1beta1.NamespaceQueueStatus{
					State: schedulingv1beta1.QueueStateOpen,
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
						},
					},
				},
			},
		},
		{
			name: "invalid podgroup with closed namespace queue",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-podgroup", Namespace: "team-a"},
				Spec:       schedulingv1beta1.PodGroupSpec{Queue: "namespace/training"},
			},
			queue: &schedulingv1beta1.Queue{},
			namespaceQueue: &schedulingv1beta1.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Name: "training", Namespace: "team-a"},
				Status: schedulingv1beta1.NamespaceQueueStatus{
					State: schedulingv1beta1.QueueStateClosed,
				},
			},
			expectError: true,
			msgContains: []string{"NamespaceQueue with state `Open`", "team-a/training", "Closed"},
		},
		{
			name: "invalid podgroup with desired closed namespace queue before status reconciliation",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-podgroup", Namespace: "team-a"},
				Spec:       schedulingv1beta1.PodGroupSpec{Queue: "namespace/training"},
			},
			queue: &schedulingv1beta1.Queue{},
			namespaceQueue: &schedulingv1beta1.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Name: "training", Namespace: "team-a"},
				Spec: schedulingv1beta1.NamespaceQueueSpec{
					State: schedulingv1beta1.QueueStateClosed,
				},
				Status: schedulingv1beta1.NamespaceQueueStatus{
					State: schedulingv1beta1.QueueStateOpen,
				},
			},
			expectError: true,
			msgContains: []string{"NamespaceQueue with desired state `Open`", "team-a/training", "Closed"},
		},
		{
			name: "valid podgroup with open namespace queue that is not ready yet",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-podgroup", Namespace: "team-a"},
				Spec:       schedulingv1beta1.PodGroupSpec{Queue: "namespace/training"},
			},
			queue: &schedulingv1beta1.Queue{},
			namespaceQueue: &schedulingv1beta1.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Name: "training", Namespace: "team-a", Generation: 1},
				Status: schedulingv1beta1.NamespaceQueueStatus{
					State: schedulingv1beta1.QueueStateOpen,
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionFalse, ObservedGeneration: 1},
					},
				},
			},
		},
		{
			name: "invalid podgroup with namespace queue that does not exist",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-podgroup", Namespace: "team-a"},
				Spec:       schedulingv1beta1.PodGroupSpec{Queue: "namespace/training"},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: true,
			msgContains: []string{"unable to find NamespaceQueue", "training"},
		},
		{
			name: "invalid podgroup with malformed namespace queue reference",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-podgroup", Namespace: "team-a"},
				Spec:       schedulingv1beta1.PodGroupSpec{Queue: "namespace/department/training"},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: true,
			msgContains: []string{"invalid queue reference"},
		},
		{
			name: "valid podgroup configured with SubGroupPolicy containing HighestTierName",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					SubGroupPolicy: []schedulingv1beta1.SubGroupPolicySpec{
						{
							Name: "test-policy",
							NetworkTopology: &schedulingv1beta1.NetworkTopologySpec{
								Mode:            schedulingv1beta1.HardNetworkTopologyMode,
								HighestTierName: "volcano.sh/hypernode",
							},
						},
					},
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: false,
		},
		{
			name: "valid podgroup configured with SubGroupPolicy containing HighestTierAllowed",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					SubGroupPolicy: []schedulingv1beta1.SubGroupPolicySpec{
						{
							Name: "test-policy",
							NetworkTopology: &schedulingv1beta1.NetworkTopologySpec{
								Mode:               schedulingv1beta1.HardNetworkTopologyMode,
								HighestTierAllowed: &highestTierAllowed,
							},
						},
					},
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: false,
		},
		{
			name: "invalid podgroup configured with SubGroupPolicy containing HighestTierAllowed and HighestTierName",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					SubGroupPolicy: []schedulingv1beta1.SubGroupPolicySpec{
						{
							Name: "test-policy",
							NetworkTopology: &schedulingv1beta1.NetworkTopologySpec{
								Mode:               schedulingv1beta1.HardNetworkTopologyMode,
								HighestTierAllowed: &highestTierAllowed,
								HighestTierName:    "volcano.sh/hypernode",
							},
						},
					},
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: true,
		},
		{
			name: "invalid podgroup configured with NetworkTopology containing HighestTierAllowed and HighestTierName",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					NetworkTopology: &schedulingv1beta1.NetworkTopologySpec{
						Mode:               schedulingv1beta1.HardNetworkTopologyMode,
						HighestTierAllowed: &highestTierAllowed,
						HighestTierName:    "volcano.sh/hypernode",
					},
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: true,
		},
		{
			name: "invalid podgroup failing both queue and networkTopology checks reports a separated message",
			podGroup: &schedulingv1beta1.PodGroup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "PodGroup",
					APIVersion: "scheduling.volcano.sh/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-podgroup",
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "test-queue",
					NetworkTopology: &schedulingv1beta1.NetworkTopologySpec{
						Mode:               schedulingv1beta1.HardNetworkTopologyMode,
						HighestTierAllowed: &highestTierAllowed,
						HighestTierName:    "volcano.sh/hypernode",
					},
				},
			},
			queue:       &schedulingv1beta1.Queue{},
			expectError: true,
			msgContains: []string{"unable to find queue", "; ", "must not specify 'highestTierAllowed' and 'highestTierName'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.VolcanoClient = fakeclient.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(config.VolcanoClient, 0)
			queueInformer := informerFactory.Scheduling().V1beta1().Queues()
			config.QueueLister = queueInformer.Lister()
			err := queueInformer.Informer().GetIndexer().Add(tt.queue)
			assert.Nil(t, err)

			namespaceQueueInformer := informerFactory.Scheduling().V1beta1().NamespaceQueues()
			config.NamespaceQueueLister = namespaceQueueInformer.Lister()
			if tt.namespaceQueue != nil {
				err := namespaceQueueInformer.Informer().GetIndexer().Add(tt.namespaceQueue)
				assert.Nil(t, err)
			}

			pgJson, _ := json.Marshal(tt.podGroup)
			// Create an AdmissionReview object
			ar := admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1beta1",
				},
				Request: &admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "scheduling.volcano.sh",
						Version: schedulingv1beta1.SchemeGroupVersion.Version,
						Kind:    "PodGroup",
					},
					Operation: admissionv1.Create,
					Name:      tt.podGroup.Name,
					Object:    runtime.RawExtension{Raw: pgJson},
					Resource: metav1.GroupVersionResource{
						Group:    schedulingv1beta1.SchemeGroupVersion.Group,
						Version:  schedulingv1beta1.SchemeGroupVersion.Version,
						Resource: "podgroups",
					},
				},
			}

			response := Validate(ar)
			if tt.expectError && response.Allowed {
				t.Errorf("Expected error but got allowed response")
			} else if !tt.expectError && !response.Allowed {
				t.Errorf("Expected allowed response but got error: %v", response.Result.Message)
			}

			if len(tt.msgContains) > 0 {
				if assert.NotNil(t, response.Result) {
					for _, want := range tt.msgContains {
						assert.Contains(t, response.Result.Message, want)
					}
				}
			}
		})
	}
}
