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

package api

import (
	"math"
	"testing"
)

func TestSaturatingAdd(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want int64
	}{
		{"normal sum", 2, 3, 5},
		{"positive overflow", math.MaxInt64, math.MaxInt64, math.MaxInt64},
		{"positive overflow by one", math.MaxInt64, 1, math.MaxInt64},
		{"negative overflow", math.MinInt64, math.MinInt64, math.MinInt64},
		{"mixed signs", math.MaxInt64, math.MinInt64, -1},
	}
	for _, tc := range cases {
		if got := SaturatingAdd(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: SaturatingAdd(%d, %d) = %d, want %d", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSaturatingMul(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want int64
	}{
		{"normal product", 6, 7, 42},
		{"zero", 0, math.MaxInt64, 0},
		{"positive overflow", math.MaxInt64, 2, math.MaxInt64},
		{"large square", math.MaxInt64, math.MaxInt64, math.MaxInt64},
		{"negative overflow", math.MinInt64, 2, math.MinInt64},
		{"min times minus one saturates high", math.MinInt64, -1, math.MaxInt64},
	}
	for _, tc := range cases {
		if got := SaturatingMul(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: SaturatingMul(%d, %d) = %d, want %d", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

// DRAResource.Add aggregates device counts across jobs/claims into the queue's
// inqueue/allocated totals; the sum must saturate rather than wrap negative.
func TestDRAResourceAdd_saturates(t *testing.T) {
	a := &DRAResource{Count: math.MaxInt64}
	a.Add(&DRAResource{Count: math.MaxInt64})
	if a.Count != math.MaxInt64 {
		t.Fatalf("DRAResource.Add Count = %d, want %d (saturated, non-negative)", a.Count, int64(math.MaxInt64))
	}
}
