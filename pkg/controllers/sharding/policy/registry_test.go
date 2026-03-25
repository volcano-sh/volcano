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

package policy

import (
	"testing"
)

// mockPolicy is a simple test policy
type mockPolicy struct {
	name string
}

func (m *mockPolicy) Name() string                               { return m.name }
func (m *mockPolicy) Initialize(args Arguments) error            { return nil }
func (m *mockPolicy) Calculate(ctx *PolicyContext) (*PolicyResult, error) { return nil, nil }
func (m *mockPolicy) Cleanup()                                   {}

func TestRegisterAndGetPolicy(t *testing.T) {
	// Register a test policy
	testPolicyName := "test-policy"
	builder := func() ShardPolicy {
		return &mockPolicy{name: testPolicyName}
	}

	RegisterPolicy(testPolicyName, builder)

	// Try to get the policy
	retrievedBuilder, err := GetPolicy(testPolicyName)
	if err != nil {
		t.Fatalf("GetPolicy() error = %v", err)
	}

	if retrievedBuilder == nil {
		t.Fatal("GetPolicy() returned nil builder")
	}

	// Create a policy instance and verify it
	policy := retrievedBuilder()
	if policy.Name() != testPolicyName {
		t.Errorf("policy.Name() = %v, want %v", policy.Name(), testPolicyName)
	}
}

func TestGetPolicyNotFound(t *testing.T) {
	_, err := GetPolicy("non-existent-policy")
	if err == nil {
		t.Error("GetPolicy() expected error for non-existent policy, got nil")
	}
}

func TestListPolicies(t *testing.T) {
	// Note: This test assumes policies from factory.go are registered
	policies := ListPolicies()

	if len(policies) == 0 {
		t.Error("ListPolicies() returned empty list")
	}

	// Check that known policies are registered
	expectedPolicies := []string{"allocation-rate", "capability", "warmup"}
	for _, expected := range expectedPolicies {
		found := false
		for _, registered := range policies {
			if registered == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected policy %s not found in registered policies: %v", expected, policies)
		}
	}
}

func TestRegisterPolicyPanic(t *testing.T) {
	// Attempting to register the same policy twice should panic
	testPolicyName := "duplicate-test-policy"
	builder := func() ShardPolicy {
		return &mockPolicy{name: testPolicyName}
	}

	// First registration should succeed
	RegisterPolicy(testPolicyName, builder)

	// Second registration should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("RegisterPolicy() expected panic for duplicate registration, but didn't panic")
		}
	}()

	RegisterPolicy(testPolicyName, builder)
}
