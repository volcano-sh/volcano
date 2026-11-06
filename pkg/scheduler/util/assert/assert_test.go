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

package assert

import (
	"testing"
)

func TestAssert(t *testing.T) {
	tests := []struct {
		name         string
		condition    bool
		message      string
		panicOnError bool
		expectPanic  bool
	}{
		{
			name:         "condition true, panicOnError true",
			condition:    true,
			message:      "no panic expected",
			panicOnError: true,
			expectPanic:  false,
		},
		{
			name:         "condition true, panicOnError false",
			condition:    true,
			message:      "no panic expected",
			panicOnError: false,
			expectPanic:  false,
		},
		{
			name:         "condition false, panicOnError true",
			condition:    false,
			message:      "panic expected",
			panicOnError: true,
			expectPanic:  true,
		},
		{
			name:         "condition false, panicOnError false",
			condition:    false,
			message:      "no panic expected",
			panicOnError: false,
			expectPanic:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := panicOnError
			panicOnError = tt.panicOnError
			t.Cleanup(func() {
				panicOnError = old
			})

			defer func() {
				r := recover()
				if tt.expectPanic {
					if r == nil {
						t.Errorf("expected panic, got none")
					} else if r != tt.message {
						t.Errorf("expected panic message %q, got %q", tt.message, r)
					}
				} else if r != nil {
					t.Errorf("expected no panic, got %q", r)
				}
			}()

			Assert(tt.condition, tt.message)
		})
	}
}

func TestAssertf(t *testing.T) {
	tests := []struct {
		name         string
		condition    bool
		format       string
		args         []interface{}
		panicOnError bool
		expectPanic  bool
		expectMsg    string
	}{
		{
			name:         "condition true, panicOnError true",
			condition:    true,
			format:       "no panic %s",
			args:         []interface{}{"expected"},
			panicOnError: true,
			expectPanic:  false,
		},
		{
			name:         "condition true, panicOnError false",
			condition:    true,
			format:       "no panic %s",
			args:         []interface{}{"expected"},
			panicOnError: false,
			expectPanic:  false,
		},
		{
			name:         "condition false, panicOnError true",
			condition:    false,
			format:       "panic %s",
			args:         []interface{}{"expected"},
			panicOnError: true,
			expectPanic:  true,
			expectMsg:    "panic expected",
		},
		{
			name:         "condition false, panicOnError false",
			condition:    false,
			format:       "no panic %s",
			args:         []interface{}{"expected"},
			panicOnError: false,
			expectPanic:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := panicOnError
			panicOnError = tt.panicOnError
			t.Cleanup(func() {
				panicOnError = old
			})

			defer func() {
				r := recover()
				if tt.expectPanic {
					if r == nil {
						t.Errorf("expected panic, got none")
					} else if r != tt.expectMsg {
						t.Errorf("expected panic message %q, got %q", tt.expectMsg, r)
					}
				} else if r != nil {
					t.Errorf("expected no panic, got %q", r)
				}
			}()

			Assertf(tt.condition, tt.format, tt.args...)
		})
	}
}
