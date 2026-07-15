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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestGangPreemptionMetrics(t *testing.T) {
	// Simulate an attempt
	RegisterGangPreemptionAttempts()

	// Read the metric from memory
	attempts := testutil.ToFloat64(gangPreemptionAttempts)
	assert.Equal(t, float64(1), attempts, "gangPreemptionAttempts should be 1 after one registration")

	// Simulate selecting 5 victims
	UpdateGangPreemptionVictimsCount(5)

	// Read the gauge from memory
	victims := testutil.ToFloat64(gangPreemptionVictims)
	assert.Equal(t, float64(5), victims, "gangPreemptionVictims should be 5 after update")
}

func TestGangReclaimMetrics(t *testing.T) {
	// Simulate an attempt
	RegisterGangReclaimAttempts()

	// Read the metric from memory
	attempts := testutil.ToFloat64(gangReclaimAttempts)
	assert.Equal(t, float64(1), attempts, "gangReclaimAttempts should be 1 after one registration")

	// Simulate selecting 3 victims
	UpdateGangReclaimVictimsCount(3)

	// Read the gauge from memory
	victims := testutil.ToFloat64(gangReclaimVictims)
	assert.Equal(t, float64(3), victims, "gangReclaimVictims should be 3 after update")
}
