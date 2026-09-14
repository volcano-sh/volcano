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

import "math"

// SaturatingAdd returns a + b, clamped to [math.MinInt64, math.MaxInt64] on
// overflow instead of wrapping around.
//
// DRA device counts (ResourceClaim ...Exactly.Count) and gang sizes are
// user-controlled and effectively unbounded (the apiserver only rejects
// count <= 0), so accumulating them with a plain += can wrap a large positive
// sum into a negative value. A negative count then defeats the "requested >
// capability" quota check in checkDRAAllocatable, admitting a job that should
// be rejected. Saturating keeps the running total a large positive value so
// the quota check still rejects it.
func SaturatingAdd(a, b int64) int64 {
	s := a + b
	if a > 0 && b > 0 && s < 0 {
		return math.MaxInt64
	}
	if a < 0 && b < 0 && s >= 0 {
		return math.MinInt64
	}
	return s
}

// SaturatingMul returns a * b, clamped to [math.MinInt64, math.MaxInt64] on
// overflow instead of wrapping around. It guards the per-device-class
// count * gangSize product in GetMinDRAResources.
func SaturatingMul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	// math.MinInt64 * -1 overflows to math.MinInt64, and math.MinInt64 / -1 also
	// wraps back to math.MinInt64, so the s/b overflow check below would miss it;
	// handle that single pair explicitly.
	if a == math.MinInt64 && b == -1 {
		return math.MaxInt64
	}
	s := a * b
	if s/b != a {
		if (a > 0) == (b > 0) {
			return math.MaxInt64
		}
		return math.MinInt64
	}
	return s
}
