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

package framework

import (
	"math/rand"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/apis/pkg/apis/scheduling"
)

// deterministic TimeJitterAfter with zero jitter
func TestTimeJitterAfter_NoJitter(t *testing.T) {
	old := time.Unix(1000, 0)
	duration := 5 * time.Second

	newT := old.Add(duration)
	if TimeJitterAfter(newT, old, duration, 0) {
		t.Error("expected false when new == old+duration and maxJitter=0")
	}

	newT = old.Add(duration).Add(time.Nanosecond)
	if !TimeJitterAfter(newT, old, duration, 0) {
		t.Error("expected true when new > old+duration and maxJitter=0")
	}
}

// With jitter>0 we can at least ensure it doesn't panic
func TestTimeJitterAfter_WithJitter(t *testing.T) {
	rand.Seed(42)
	old := time.Now()
	duration := 100 * time.Millisecond
	// should return bool without panic
	_ = TimeJitterAfter(old.Add(duration), old, duration, 50*time.Millisecond)
}

func makeCond(id, msg string, ts time.Time) scheduling.PodGroupCondition {
	return scheduling.PodGroupCondition{
		Reason:             msg,
		TransitionID:       id,
		LastTransitionTime: metav1.Time{Time: ts},
	}
}

func TestIsPodGroupConditionsUpdated(t *testing.T) {
	t0 := time.Now()
	a := []scheduling.PodGroupCondition{
		makeCond("id1", "r1", t0),
		makeCond("id2", "r2", t0),
	}
	b := []scheduling.PodGroupCondition{
		makeCond("id1", "r1", t0),
		makeCond("id2", "r2", t0),
	}

	if isPodGroupConditionsUpdated(a, b) {
		t.Error("expected no update when conditions equal")
	}

	if !isPodGroupConditionsUpdated(append(a, makeCond("id3", "r3", t0)), b) {
		t.Error("expected update when lengths differ")
	}

	b2 := []scheduling.PodGroupCondition{
		makeCond("id1", "r1", t0),
		makeCond("id2", "DIFFERENT", t0),
	}
	if !isPodGroupConditionsUpdated(a, b2) {
		t.Error("expected update when Reason differs")
	}
}

func TestIsPodGroupStatusUpdated(t *testing.T) {
	t0 := time.Now()
	cond1 := makeCond("id1", "r1", t0)

	s1 := scheduling.PodGroupStatus{
		Conditions: []scheduling.PodGroupCondition{cond1},
	}
	if isPodGroupStatusUpdated(s1, s1) {
		t.Error("expected no update when statuses are identical")
	}

	s2 := scheduling.PodGroupStatus{
		Conditions: []scheduling.PodGroupCondition{
			makeCond("id1", "r1", t0),
			makeCond("id2", "r2", t0),
		},
	}
	if !isPodGroupStatusUpdated(s2, s1) {
		t.Error("expected update when statuses differ")
	}
}
