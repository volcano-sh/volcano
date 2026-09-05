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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPriorityQueue_ReheapifyAfterInPlaceKeyChange(t *testing.T) {
	type item struct {
		name  string
		score int
	}

	// Compare against a live pointer so callers can mutate score in place.
	lessFn := func(l, r interface{}) bool {
		return l.(*item).score < r.(*item).score
	}

	q := NewPriorityQueue(lessFn)
	a := &item{name: "a", score: 3}
	b := &item{name: "b", score: 1}
	c := &item{name: "c", score: 2}
	q.Push(a)
	q.Push(b)
	q.Push(c)

	// Sanity: initial pop order follows the initial scores (min-first).
	assert.Equal(t, "b", q.Pop().(*item).name)
	// Re-push b so the queue still has three items for the reheapify check.
	q.Push(b)

	// Mutate b's score in place so it should now sink to the bottom, and
	// mutate c so it should rise. Without Reheapify, Pop returns the stale
	// order established when the items were pushed.
	b.score = 10
	c.score = 0
	q.Reheapify()

	assert.Equal(t, "c", q.Pop().(*item).name, "reheapify must expose the updated min")
	assert.Equal(t, "a", q.Pop().(*item).name)
	assert.Equal(t, "b", q.Pop().(*item).name, "b sank to the bottom after its score grew")
	assert.True(t, q.Empty())
}
