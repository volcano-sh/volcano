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

package api

import (
	"errors"
	"reflect"
	"testing"

	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

func TestTaskStatus_String(t *testing.T) {
	tests := []struct {
		name     string
		status   TaskStatus
		expected string
	}{
		{"Pending", Pending, "Pending"},
		{"Allocated", Allocated, "Allocated"},
		{"Pipelined", Pipelined, "Pipelined"},
		{"Binding", Binding, "Binding"},
		{"Bound", Bound, "Bound"},
		{"Running", Running, "Running"},
		{"Releasing", Releasing, "Releasing"},
		{"Succeeded", Succeeded, "Succeeded"},
		{"Failed", Failed, "Failed"},
		{"Unknown", TaskStatus(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("TaskStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNodePhase_String(t *testing.T) {
	tests := []struct {
		name     string
		phase    NodePhase
		expected string
	}{
		{"Ready", Ready, "Ready"},
		{"NotReady", NotReady, "NotReady"},
		{"Unknown", NodePhase(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.phase.String(); got != tt.expected {
				t.Errorf("NodePhase.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	successStatus := &Status{Code: Success}
	errorStatus := &Status{Code: Error, Reason: "error reason"}
	waitStatus := &Status{Code: Wait}
	skipStatus := &Status{Code: Skip}

	t.Run("IsSuccess", func(t *testing.T) {
		if !successStatus.IsSuccess() {
			t.Errorf("expected success status to be success")
		}
		if errorStatus.IsSuccess() {
			t.Errorf("expected error status not to be success")
		}
		var nilStatus *Status
		if !nilStatus.IsSuccess() {
			t.Errorf("expected nil status to be success")
		}
	})

	t.Run("IsWait", func(t *testing.T) {
		if !waitStatus.IsWait() {
			t.Errorf("expected wait status to be wait")
		}
		if successStatus.IsWait() {
			t.Errorf("expected success status not to be wait")
		}
	})

	t.Run("IsSkip", func(t *testing.T) {
		if !skipStatus.IsSkip() {
			t.Errorf("expected skip status to be skip")
		}
		if successStatus.IsSkip() {
			t.Errorf("expected success status not to be skip")
		}
	})

	t.Run("AsError", func(t *testing.T) {
		if err := successStatus.AsError(); err != nil {
			t.Errorf("expected success status to return nil error, but got %v", err)
		}
		if err := waitStatus.AsError(); err != nil {
			t.Errorf("expected wait status to return nil error, but got %v", err)
		}
		if err := errorStatus.AsError(); err == nil {
			t.Errorf("expected error status to return non-nil error")
		} else if err.Error() != "error reason" {
			t.Errorf("expected error message 'error reason', but got '%s'", err.Error())
		}
	})
}

func TestAsStatus(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if status := AsStatus(nil); status != nil {
			t.Errorf("expected nil status for nil error, but got %v", status)
		}
	})

	t.Run("non-nil error", func(t *testing.T) {
		err := errors.New("test error")
		status := AsStatus(err)
		if status == nil {
			t.Fatal("expected non-nil status for non-nil error")
		}
		if status.Code != Error {
			t.Errorf("expected status code Error, but got %d", status.Code)
		}
		if status.Reason != "test error" {
			t.Errorf("expected reason 'test error', but got '%s'", status.Reason)
		}
	})
}

func TestStatusSets(t *testing.T) {
	ss := StatusSets{
		{Code: Success},
		{Code: Unschedulable, Reason: "reason 1"},
		{Code: Error, Reason: "reason 2"},
		nil,
	}

	if !ss.ContainsUnschedulable() {
		t.Errorf("expected StatusSets to contain Unschedulable")
	}

	if ss.ContainsUnschedulableAndUnresolvable() {
		t.Errorf("expected StatusSets not to contain UnschedulableAndUnresolvable")
	}

	if !ss.ContainsErrorSkipOrWait() {
		t.Errorf("expected StatusSets to contain Error")
	}

	expectedMsg := "reason 1,reason 2"
	if msg := ss.Message(); msg != expectedMsg {
		t.Errorf("expected message '%s', but got '%s'", expectedMsg, msg)
	}

	expectedReasons := []string{"reason 1", "reason 2"}
	if reasons := ss.Reasons(); !reflect.DeepEqual(reasons, expectedReasons) {
		t.Errorf("expected reasons %v, but got %v", expectedReasons, reasons)
	}
}

func TestConvertPredicateStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   *k8sframework.Status
		expected *Status
	}{
		{
			name:   "Success",
			status: k8sframework.NewStatus(k8sframework.Success),
			expected: &Status{
				Code:   Success,
				Reason: "",
			},
		},
		{
			name:   "Error",
			status: k8sframework.NewStatus(k8sframework.Error, "internal error"),
			expected: &Status{
				Code:   Error,
				Reason: "internal error",
			},
		},
		{
			name:   "Unschedulable",
			status: k8sframework.NewStatus(k8sframework.Unschedulable, "node does not have enough resource"),
			expected: &Status{
				Code:   Unschedulable,
				Reason: "node does not have enough resource",
			},
		},
		{
			name:   "nil status",
			status: nil,
			expected: &Status{
				Code: Success,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertPredicateStatus(tt.status)
			// Plugin name can vary, so we only compare Code and Reason.
			if got.Code != tt.expected.Code || got.Reason != tt.expected.Reason {
				t.Errorf("ConvertPredicateStatus() got = %+v, want %+v", got, tt.expected)
			}
		})
	}
}
