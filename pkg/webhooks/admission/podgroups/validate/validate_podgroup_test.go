package validate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
)

func TestValidatePodGroup(t *testing.T) {
	tests := []struct {
		name        string
		podGroup    *schedulingv1beta1.PodGroup
		queue       *schedulingv1beta1.Queue
		expectError bool
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.VolcanoClient = fakeclient.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(config.VolcanoClient, 0)
			queueInformer := informerFactory.Scheduling().V1beta1().Queues()
			config.QueueLister = queueInformer.Lister()
			err := queueInformer.Informer().GetIndexer().Add(tt.queue)
			assert.Nil(t, err)

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
		})
	}
}
