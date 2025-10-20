package capacitycard

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestBuildTotalResourceFromNodes tests the buildTotalResourceFromNodes function
func TestBuildTotalResourceFromNodes(t *testing.T) {
	tests := []struct {
		name           string
		nodes          []*corev1.Node
		expectedCPU    string
		expectedMemory string
		expectedCards  map[corev1.ResourceName]string
	}{
		{
			name: "single node with CPU and memory",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards:  map[corev1.ResourceName]string{},
		},
		{
			name: "multiple nodes with CPU and memory",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("8"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				},
			},
			expectedCPU:    "12",
			expectedMemory: "24Gi",
			expectedCards:  map[corev1.ResourceName]string{},
		},
		{
			name: "node with GPU card resources",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "Tesla-V100",
							"nvidia.com/gpu.count":   "2",
							"nvidia.com/gpu.memory":  "32768",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							"nvidia.com/gpu":      resource.MustParse("2"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-V100": "2",
			},
		},
		{
			name: "node with MPS resources",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product":  "Tesla-V100",
							"nvidia.com/gpu.count":    "4",
							"nvidia.com/gpu.memory":   "32768",
							"nvidia.com/gpu.replicas": "4",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:      resource.MustParse("4"),
							corev1.ResourceMemory:   resource.MustParse("8Gi"),
							"nvidia.com/gpu":        resource.MustParse("2"),
							"nvidia.com/gpu.shared": resource.MustParse("8"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-V100":             "2",
				"Tesla-V100/mps-32g*1/4": "8",
			},
		},
		{
			name: "node with MIG resources",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/mig-1g.10gb.count":     "4",
							"nvidia.com/mig-2g.20gb.count":     "2",
							"nvidia.com/mig-1g.10gb.memory":    "10240",
							"nvidia.com/mig-2g.20gb.memory":    "20480",
							"nvidia.com/mig-1g.10gb.slices.ci": "1",
							"nvidia.com/mig-2g.20gb.slices.ci": "2",
							"nvidia.com/mig-1g.10gb.slices.gi": "1",
							"nvidia.com/mig-2g.20gb.slices.gi": "2",
							"nvidia.com/gpu.product":           "Tesla-A100",
							"nvidia.com/gpu.count":             "1",
							"nvidia.com/gpu.memory":            "40960",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:       resource.MustParse("4"),
							corev1.ResourceMemory:    resource.MustParse("8Gi"),
							"nvidia.com/mig-1g.10gb": resource.MustParse("4"),
							"nvidia.com/mig-2g.20gb": resource.MustParse("2"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-A100/mig-1g.10gb-mixed": "4",
				"Tesla-A100/mig-2g.20gb-mixed": "2",
			},
		},
		{
			name:           "empty node list",
			nodes:          []*corev1.Node{},
			expectedCPU:    "0",
			expectedMemory: "0",
			expectedCards:  map[corev1.ResourceName]string{},
		},
		{
			name: "node with invalid GPU count",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "Tesla-V100",
							"nvidia.com/gpu.count":   "a",
							"nvidia.com/gpu.memory":  "32768",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							"nvidia.com/gpu":      resource.MustParse("2"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-V100": "2",
			},
		},
		{
			name: "node with invalid GPU memory",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "Tesla-V100",
							"nvidia.com/gpu.count":   "2",
							"nvidia.com/gpu.memory":  "a",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							"nvidia.com/gpu":      resource.MustParse("2"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-V100": "2",
			},
		},
		{
			name: "duplicate node name",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "Tesla-V100",
							"nvidia.com/gpu.count":   "2",
							"nvidia.com/gpu.memory":  "32768",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							"nvidia.com/gpu":      resource.MustParse("2"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "Tesla-V100",
							"nvidia.com/gpu.count":   "2",
							"nvidia.com/gpu.memory":  "32768",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							"nvidia.com/gpu":      resource.MustParse("2"),
						},
					},
				},
			},
			expectedCPU:    "8",
			expectedMemory: "16Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-V100": "4",
			},
		},
		{
			name: "node with invalid MPS label",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product":  "Tesla-V100",
							"nvidia.com/gpu.count":    "4",
							"nvidia.com/gpu.memory":   "32768",
							"nvidia.com/gpu.replicas": "a",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:      resource.MustParse("4"),
							corev1.ResourceMemory:   resource.MustParse("8Gi"),
							"nvidia.com/gpu":        resource.MustParse("2"),
							"nvidia.com/gpu.shared": resource.MustParse("8"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards: map[corev1.ResourceName]string{
				"Tesla-V100": "2",
			},
		},
		{
			name: "node with invalid GPU card name",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia-invalid.com/gpu.product": "Tesla-V100",
							"nvidia-invalid.com/gpu.count":   "2",
							"nvidia-invalid.com/gpu.memory":  "32768",
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							"nvidia.com/gpu":      resource.MustParse("2"),
						},
					},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "8Gi",
			expectedCards:  map[corev1.ResourceName]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{
				nodeCardInfos:          make(map[string]NodeCardResourceInfo),
				cardNameToResourceName: make(map[corev1.ResourceName]corev1.ResourceName),
				totalResource:          api.EmptyResource(),
			}

			plugin.buildTotalResourceFromNodes(tt.nodes)

			// Check CPU resource
			if tt.expectedCPU != "0" {
				expectedCPU := resource.MustParse(tt.expectedCPU)
				actualCPU := plugin.totalResource.Get(corev1.ResourceCPU)
				if actualCPU != float64(expectedCPU.MilliValue()) {
					t.Errorf("expected CPU %v, got %v", expectedCPU.MilliValue(), actualCPU)
				}
			} else {
				actualCPU := plugin.totalResource.Get(corev1.ResourceCPU)
				if actualCPU != 0 {
					t.Errorf("expected CPU 0, got %v", actualCPU)
				}
			}

			// Check Memory resource
			if tt.expectedMemory != "0" {
				expectedMemory := resource.MustParse(tt.expectedMemory)
				actualMemory := plugin.totalResource.Get(corev1.ResourceMemory)
				if actualMemory != float64(expectedMemory.Value()) {
					t.Errorf("expected Memory %v, got %v", expectedMemory.Value(), actualMemory)
				}
			} else {
				actualMemory := plugin.totalResource.Get(corev1.ResourceMemory)
				if actualMemory != 0 {
					t.Errorf("expected Memory 0, got %v", actualMemory)
				}
			}

			// Check card resources
			for cardName, expectedValue := range tt.expectedCards {
				expectedQuantity := resource.MustParse(expectedValue)
				actualValue := plugin.totalResource.Get(cardName)
				if actualValue != float64(expectedQuantity.MilliValue()) {
					t.Errorf("expected card %s value %v, got %v", cardName, expectedQuantity.MilliValue(), actualValue)
				}
			}

			// Check that no unexpected card resources exist
			if len(tt.expectedCards) == 0 {
				if len(plugin.totalResource.ScalarResources) > 0 {
					t.Errorf("expected no card resources, but found %v", plugin.totalResource.ScalarResources)
				}
			}
		})
	}
}

// TestBuildTotalResourceFromNodesEdgeCases tests edge cases for buildTotalResourceFromNodes
func TestBuildTotalResourceFromNodesEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []*corev1.Node
		expectedError bool
	}{
		{
			name: "node with zero allocatable resources",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "node with missing allocatable section",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status:     corev1.NodeStatus{},
				},
			},
			expectedError: false,
		},
		{
			name: "node with invalid card labels but no card resources",
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"nvidia.com/gpu.product": "Tesla-V100",
							// Missing count and memory labels
						},
					},
					Status: corev1.NodeStatus{
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{
				nodeCardInfos:          make(map[string]NodeCardResourceInfo),
				cardNameToResourceName: make(map[corev1.ResourceName]corev1.ResourceName),
				totalResource:          api.EmptyResource(),
			}

			// This should not panic
			plugin.buildTotalResourceFromNodes(tt.nodes)

			// Basic sanity check - plugin should still be in a valid state
			if plugin.totalResource == nil {
				t.Error("totalResource should not be nil after buildTotalResourceFromNodes")
			}
		})
	}
}
