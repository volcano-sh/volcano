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

package reservation

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestCalculateAllocatable(t *testing.T) {
	task := batch.TaskSpec{
		Replicas: 2,
		Template: v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "test-container",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("100m"),
								v1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			},
		},
	}

	reservationObj := &batch.Reservation{
		Spec: batch.ReservationSpec{
			Tasks: []batch.TaskSpec{task},
		},
	}

	expected := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("200m"),  // 100m * 2
		v1.ResourceMemory: resource.MustParse("256Mi"), // 128Mi * 2
	}

	alloc := calculateAllocatable(reservationObj)

	for resName, expectedQty := range expected {
		actualQty, ok := alloc[resName]
		if !ok {
			t.Errorf("resource %v not found in allocatable", resName)
			continue
		}
		if actualQty.Cmp(expectedQty) != 0 {
			t.Errorf("resource %v: expected %v, got %v", resName, expectedQty.String(), actualQty.String())
		}
	}
}
