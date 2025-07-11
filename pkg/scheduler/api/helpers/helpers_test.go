/*
Copyright 2021 The Volcano Authors.

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

package helpers

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestMax(t *testing.T) {
	l := &api.Resource{
		MilliCPU: 1,
		Memory:   1024,
		ScalarResources: map[v1.ResourceName]float64{
			"gpu":    1,
			"common": 4,
		},
	}
	r := &api.Resource{
		MilliCPU: 2,
		Memory:   1024,
		ScalarResources: map[v1.ResourceName]float64{
			"npu":    2,
			"common": 5,
		},
	}
	expected := &api.Resource{
		MilliCPU: 2,
		Memory:   1024,
		ScalarResources: map[v1.ResourceName]float64{
			"gpu":    1,
			"npu":    2,
			"common": 5,
		},
	}
	re := Max(l, r)
	if !equality.Semantic.DeepEqual(expected, re) {
		t.Errorf("expected: %#v, got: %#v", expected, re)
	}
}
