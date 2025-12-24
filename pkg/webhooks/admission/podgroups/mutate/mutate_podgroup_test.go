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

package mutate

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"volcano.sh/volcano/pkg/webhooks/router"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func Test_createPodGroupPatch(t *testing.T) {
	tests := []struct {
		name          string
		podgroup      *schedulingv1beta1.PodGroup
		nsAnnotations map[string]string
		wantPatch     []patchOperation
		wantErr       bool
	}{
		{
			name: "podgroup with non-default queue",
			podgroup: &schedulingv1beta1.PodGroup{
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "custom-queue",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
			},
			nsAnnotations: nil,
			wantPatch:     nil,
			wantErr:       false,
		},
		{
			name: "podgroup with default queue and namespace with queue annotation",
			podgroup: &schedulingv1beta1.PodGroup{
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: schedulingv1beta1.DefaultQueue,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
			},
			nsAnnotations: map[string]string{
				schedulingv1beta1.QueueNameAnnotationKey: "ns-queue",
			},
			wantPatch: []patchOperation{
				{
					Op:    "add",
					Path:  "/spec/queue",
					Value: "ns-queue",
				},
			},
			wantErr: false,
		},
		{
			name: "podgroup with default queue and namespace without queue annotation",
			podgroup: &schedulingv1beta1.PodGroup{
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: schedulingv1beta1.DefaultQueue,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
			},
			nsAnnotations: map[string]string{},
			wantPatch:     nil,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client
			client := fake.NewSimpleClientset()
			if tt.nsAnnotations != nil {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-ns",
						Annotations: tt.nsAnnotations,
					},
				}
				_, err := client.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Failed to create test namespace: %v", err)
				}
			}

			config = &router.AdmissionServiceConfig{
				KubeClient: client,
			}

			got, err := createPodGroupPatch(tt.podgroup)
			if (err != nil) != tt.wantErr {
				t.Errorf("createPodGroupPatch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantPatch == nil {
				if got != nil {
					t.Errorf("createPodGroupPatch() got = %v, want nil", string(got))
				}
				return
			}

			var gotPatch []patchOperation
			if err := json.Unmarshal(got, &gotPatch); err != nil {
				t.Errorf("Failed to unmarshal patch: %v", err)
				return
			}

			if !reflect.DeepEqual(gotPatch, tt.wantPatch) {
				t.Errorf("createPodGroupPatch() got = %v, want %v", gotPatch, tt.wantPatch)
			}
		})
	}
}
