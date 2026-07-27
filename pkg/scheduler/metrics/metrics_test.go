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
)

func TestRegisterEvictionTransaction(t *testing.T) {
	actions := []string{
		evictionActionPreempt,
		evictionActionReclaim,
		evictionActionGangPreempt,
		evictionActionGangReclaim,
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			counter := evictionTransactions.WithLabelValues(action)
			before := testutil.ToFloat64(counter)

			RegisterEvictionTransaction(action)

			if got := testutil.ToFloat64(counter); got != before+1 {
				t.Fatalf("eviction transaction counter = %v, want %v", got, before+1)
			}
		})
	}

	before := testutil.CollectAndCount(evictionTransactions)
	RegisterEvictionTransaction("unsupported-action")
	if got := testutil.CollectAndCount(evictionTransactions); got != before {
		t.Fatalf("metric count after unsupported action = %d, want %d", got, before)
	}
}
