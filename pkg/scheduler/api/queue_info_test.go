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

package api

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestQueueInfo_Clone(t *testing.T) {
	annotations := map[string]string{
		v1beta1.KubeHierarchyAnnotationKey:       "root/child",
		v1beta1.KubeHierarchyWeightAnnotationKey: "1/2",
	}

	queue := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-queue",
			Annotations: annotations,
		},
		Spec: scheduling.QueueSpec{
			Weight: 15,
		},
	}

	original := NewQueueInfo(queue)
	cloned := original.Clone()

	// Verify all fields are copied correctly
	if cloned.UID != original.UID {
		t.Errorf("expected UID %s, but got %s", original.UID, cloned.UID)
	}
	if cloned.Name != original.Name {
		t.Errorf("expected Name %s, but got %s", original.Name, cloned.Name)
	}
	if cloned.Weight != original.Weight {
		t.Errorf("expected Weight %d, but got %d", original.Weight, cloned.Weight)
	}
	if cloned.Hierarchy != original.Hierarchy {
		t.Errorf("expected Hierarchy %s, but got %s", original.Hierarchy, cloned.Hierarchy)
	}
	if cloned.Weights != original.Weights {
		t.Errorf("expected Weights %s, but got %s", original.Weights, cloned.Weights)
	}
	if cloned.Queue != original.Queue {
		t.Errorf("expected Queue pointer to be the same")
	}

	// Verify it's a separate object (not the same pointer)
	if cloned == original {
		t.Error("cloned object should not be the same pointer as original")
	}
}
