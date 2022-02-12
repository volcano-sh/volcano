package helpers

import (
	v1 "k8s.io/api/core/v1"
	"reflect"
	"testing"
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
	if !reflect.DeepEqual(expected, re) {
		t.Errorf("expected: %#v, got: %#v", expected, re)
	}
}
