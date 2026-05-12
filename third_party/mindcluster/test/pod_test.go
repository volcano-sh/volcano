/*
Copyright 2024 The Volcano Authors.

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

package test

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestAddTestTaskLabelNilTask verifies the function handles nil task gracefully without panic.
func TestAddTestTaskLabelNilTask(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AddTestTaskLabel panicked with nil task: %v", r)
		}
	}()

	// Should not panic
	AddTestTaskLabel(nil, "key", "value")
}

// TestAddTestTaskLabelNilPod verifies the function handles nil Pod field gracefully without panic.
func TestAddTestTaskLabelNilPod(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AddTestTaskLabel panicked with nil Pod: %v", r)
		}
	}()

	task := &api.TaskInfo{
		Pod: nil,
	}

	// Should not panic
	AddTestTaskLabel(task, "key", "value")
}

// TestAddTestTaskLabelNilNodeSelector verifies proper initialization of nil NodeSelector.
func TestAddTestTaskLabelNilNodeSelector(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeSelector: nil, // Explicitly nil
		},
	}

	task := &api.TaskInfo{
		Pod: pod,
	}

	AddTestTaskLabel(task, "test-key", "test-value")

	if task.Pod.Spec.NodeSelector == nil {
		t.Error("NodeSelector should be initialized")
	}

	if task.Pod.Spec.NodeSelector["test-key"] != "test-value" {
		t.Errorf("expected NodeSelector['test-key']='test-value', got '%v'",
			task.Pod.Spec.NodeSelector["test-key"])
	}
}

// TestAddTestTaskLabelNilLabels verifies proper initialization of nil Labels map.
func TestAddTestTaskLabelNilLabels(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    nil, // Explicitly nil
		},
	}

	task := &api.TaskInfo{
		Pod: pod,
	}

	AddTestTaskLabel(task, "test-key", "test-value")

	if task.Pod.Labels == nil {
		t.Error("Labels should be initialized")
	}

	if task.Pod.Labels["test-key"] != "test-value" {
		t.Errorf("expected Labels['test-key']='test-value', got '%v'",
			task.Pod.Labels["test-key"])
	}
}

// TestAddTestTaskLabelNormalCase verifies correct behavior with valid populated maps.
func TestAddTestTaskLabelNormalCase(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"existing-key": "existing-value",
			},
		},
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{
				"node-existing-key": "node-existing-value",
			},
		},
	}

	task := &api.TaskInfo{
		Pod: pod,
	}

	AddTestTaskLabel(task, "new-key", "new-value")

	// Verify existing entries are preserved
	if task.Pod.Labels["existing-key"] != "existing-value" {
		t.Error("Existing label should be preserved")
	}

	if task.Pod.Spec.NodeSelector["node-existing-key"] != "node-existing-value" {
		t.Error("Existing node selector should be preserved")
	}

	// Verify new entries are added
	if task.Pod.Labels["new-key"] != "new-value" {
		t.Errorf("expected Labels['new-key']='new-value', got '%v'",
			task.Pod.Labels["new-key"])
	}

	if task.Pod.Spec.NodeSelector["new-key"] != "new-value" {
		t.Errorf("expected NodeSelector['new-key']='new-value', got '%v'",
			task.Pod.Spec.NodeSelector["new-key"])
	}
}

// TestAddTestTaskLabelIdempotent verifies the function can be called multiple times safely.
func TestAddTestTaskLabelIdempotent(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	task := &api.TaskInfo{
		Pod: pod,
	}

	// Call multiple times
	AddTestTaskLabel(task, "key1", "value1")
	AddTestTaskLabel(task, "key2", "value2")
	AddTestTaskLabel(task, "key1", "updated-value1") // Update existing

	if task.Pod.Labels["key1"] != "updated-value1" {
		t.Errorf("expected Labels['key1']='updated-value1', got '%v'",
			task.Pod.Labels["key1"])
	}

	if task.Pod.Labels["key2"] != "value2" {
		t.Errorf("expected Labels['key2']='value2', got '%v'",
			task.Pod.Labels["key2"])
	}

	if task.Pod.Spec.NodeSelector["key1"] != "updated-value1" {
		t.Errorf("expected NodeSelector['key1']='updated-value1', got '%v'",
			task.Pod.Spec.NodeSelector["key1"])
	}

	if task.Pod.Spec.NodeSelector["key2"] != "value2" {
		t.Errorf("expected NodeSelector['key2']='value2', got '%v'",
			task.Pod.Spec.NodeSelector["key2"])
	}
}

// TestAddTestTaskLabelEmptyKeyValue verifies proper handling of empty key/value strings.
func TestAddTestTaskLabelEmptyKeyValue(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	task := &api.TaskInfo{
		Pod: pod,
	}

	// Empty key and value should still be added
	AddTestTaskLabel(task, "", "")

	if task.Pod.Labels[""] != "" {
		t.Error("Empty key with empty value should be added to Labels")
	}

	if task.Pod.Spec.NodeSelector[""] != "" {
		t.Error("Empty key with empty value should be added to NodeSelector")
	}
}

// TestAddTestTaskLabelBothMapsNil verifies initialization of both nil maps simultaneously.
func TestAddTestTaskLabelBothMapsNil(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    nil,
		},
		Spec: v1.PodSpec{
			NodeSelector: nil,
		},
	}

	task := &api.TaskInfo{
		Pod: pod,
	}

	AddTestTaskLabel(task, "testkey", "testvalue")

	// Both maps should be initialized
	if task.Pod.Labels == nil {
		t.Error("Labels should be initialized")
	}

	if task.Pod.Spec.NodeSelector == nil {
		t.Error("NodeSelector should be initialized")
	}

	// Both maps should contain the label
	if task.Pod.Labels["testkey"] != "testvalue" {
		t.Error("Label not added correctly")
	}

	if task.Pod.Spec.NodeSelector["testkey"] != "testvalue" {
		t.Error("NodeSelector not added correctly")
	}
}
