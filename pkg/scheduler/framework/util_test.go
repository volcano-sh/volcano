package framework

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeNode(unschedulable bool, readyCondition v1.ConditionStatus, nodeIsNil bool) *v1.Node {
	if nodeIsNil {
		return nil
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Spec: v1.NodeSpec{
			Unschedulable: unschedulable,
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:   v1.NodeReady,
					Status: readyCondition,
				},
			},
		},
	}
	return node
}

func makeNodeWithoutReady() *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-no-ready",
		},
		Spec: v1.NodeSpec{
			Unschedulable: false,
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{},
		},
	}
}

func TestIsNodeUnschedulable(t *testing.T) {
	tests := []struct {
		name      string
		input     bool
		expected  bool
		nodeIsNil bool
	}{
		{"unschedulable true", true, true, false},
		{"unschedulable false", false, false, false},
		{"node is nil", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := makeNode(tt.input, v1.ConditionTrue, tt.nodeIsNil)
			result := isNodeUnschedulable(node)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsNodeNotReady(t *testing.T) {
	tests := []struct {
		name      string
		ready     v1.ConditionStatus
		expected  bool
		nodeIsNil bool
	}{
		{"NodeReady=True", v1.ConditionTrue, false, false},
		{"NodeReady=False", v1.ConditionFalse, true, false},
		{"NodeReady=Unknown", v1.ConditionUnknown, true, false},
		{"node is nil", "", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := makeNode(false, tt.ready, false)
			result := isNodeNotReady(node)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}

	t.Run("No NodeReady condition", func(t *testing.T) {
		node := makeNodeWithoutReady()
		result := isNodeNotReady(node)
		if result != true {
			t.Errorf("expected true when no NodeReady condition, got %v", result)
		}
	})
}
