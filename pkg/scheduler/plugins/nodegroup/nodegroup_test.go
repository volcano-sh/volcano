package nodegroup

import (
	"reflect"
	"testing"
	"volcano.sh/volcano/pkg/scheduler/util/assert"
)

func TestCalculateQueueGroupAffinity(t *testing.T) {

	m1 := map[string][]string{"q1": {"g1", "g2"}, "q2": {"g2"}}
	mc1 := CalculateQueueGroupAffinity("q1 = g1,g2 ; q2 = g2")
	assert.Assert(reflect.DeepEqual(m1, mc1), "CalculateQueueGroupAffinity err pass")

	m2 := map[string][]string{"q1": {"g1", "g2"}}
	mc2 := CalculateQueueGroupAffinity("q1 = g1,g2 ; q2 = ")
	assert.Assert(reflect.DeepEqual(m2, mc2), "CalculateQueueGroupAffinity err pass")

}
