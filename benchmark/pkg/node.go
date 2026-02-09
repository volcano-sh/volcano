package pkg

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeBuilder constructs KWOK fake nodes for benchmark testing.
type NodeBuilder struct {
	node *corev1.Node
}

// NewNodeBuilder creates a NodeBuilder with KWOK defaults.
func NewNodeBuilder() *NodeBuilder {
	return &NodeBuilder{
		node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"node.alpha.kubernetes.io/ttl": "0",
					"kwok.x-k8s.io/node":           "fake",
				},
				Labels: map[string]string{
					"beta.kubernetes.io/arch":       "amd64",
					"beta.kubernetes.io/os":         "linux",
					"kubernetes.io/arch":            "amd64",
					"kubernetes.io/os":              "linux",
					"kubernetes.io/role":            "agent",
					"node-role.kubernetes.io/agent": "",
					"type":                          "kwok",
				},
			},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{
						Key:    "kwok.x-k8s.io/node",
						Value:  "fake",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					"cpu":    resource.MustParse("32"),
					"memory": resource.MustParse("256Gi"),
					"pods":   resource.MustParse("110"),
				},
				Capacity: corev1.ResourceList{
					"cpu":    resource.MustParse("32"),
					"memory": resource.MustParse("256Gi"),
					"pods":   resource.MustParse("110"),
				},
			},
		},
	}
}

// WithName sets the node name.
func (b *NodeBuilder) WithName(name string) *NodeBuilder {
	b.node.Name = name
	b.node.Labels["kubernetes.io/hostname"] = name
	return b
}

// WithCPU sets the CPU capacity.
func (b *NodeBuilder) WithCPU(cpu string) *NodeBuilder {
	q := resource.MustParse(cpu)
	b.node.Status.Allocatable["cpu"] = q
	b.node.Status.Capacity["cpu"] = q
	return b
}

// WithMemory sets the memory capacity.
func (b *NodeBuilder) WithMemory(memory string) *NodeBuilder {
	q := resource.MustParse(memory)
	b.node.Status.Allocatable["memory"] = q
	b.node.Status.Capacity["memory"] = q
	return b
}

// WithFastReady marks the node as immediately Ready.
func (b *NodeBuilder) WithFastReady() *NodeBuilder {
	b.node.Status.Conditions = []corev1.NodeCondition{
		{
			Type:               corev1.NodeReady,
			Status:             corev1.ConditionTrue,
			Reason:             "KubeletReady",
			Message:            "kubelet is posting ready status",
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
		},
	}
	b.node.Status.Phase = corev1.NodeRunning
	return b
}

// Build returns a deep copy of the constructed node.
func (b *NodeBuilder) Build() *corev1.Node {
	return b.node.DeepCopy()
}
