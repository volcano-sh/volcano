package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNodeStatus(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		expected bool
	}{
		{
			name: "no conditions",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status:     v1.NodeStatus{},
				Spec:       v1.NodeSpec{},
			},
			expected: false,
		},
		{
			name: "ready",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node2"},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{Type: v1.NodeReady, Status: v1.ConditionTrue},
					},
				},
				Spec: v1.NodeSpec{},
			},
			expected: true,
		},
		{
			name: "not ready",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node3"},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{Type: v1.NodeReady, Status: v1.ConditionFalse},
					},
				},
				Spec: v1.NodeSpec{},
			},
			expected: false,
		},
		{
			name: "ready and unschedulable",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node4"},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{Type: v1.NodeReady, Status: v1.ConditionTrue},
					},
				},
				Spec: v1.NodeSpec{
					Unschedulable: true,
				},
			},
			expected: false,
		},
		{
			name: "unknown and unschedulable",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node5"},
				Status:     v1.NodeStatus{},
				Spec: v1.NodeSpec{
					Unschedulable: true,
				},
			},
			expected: false,
		},
		{
			name: "not ready and unschedulable",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node6"},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{Type: v1.NodeReady, Status: v1.ConditionFalse},
					},
				},
				Spec: v1.NodeSpec{
					Unschedulable: true,
				},
			},
			expected: false,
		},
		{
			name: "multiple conditions including ready",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node7"},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{Type: v1.NodeMemoryPressure, Status: v1.ConditionTrue},
						{Type: v1.NodeReady, Status: v1.ConditionTrue},
					},
				},
				Spec: v1.NodeSpec{},
			},
			expected: true,
		},
		{
			name: "multiple conditions without ready",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node8"},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{Type: v1.NodeMemoryPressure, Status: v1.ConditionTrue},
						{Type: v1.NodeDiskPressure, Status: v1.ConditionFalse},
					},
				},
				Spec: v1.NodeSpec{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nodeIsNotReady(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}
