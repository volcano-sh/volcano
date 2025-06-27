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

package node

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/config"
)

func makeNode() (*v1.Node, error) {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}, nil
}

func Test_updater_Update(t *testing.T) {
	fakeNode, err := makeNode()
	assert.NoError(t, err)
	fakeClient := fakeclientset.NewSimpleClientset(fakeNode)

	tests := []struct {
		name          string
		config        *config.Configuration
		nodeModifiers []Modifier
		expectedNode  *v1.Node
	}{
		{
			name: "update node status",
			config: &config.Configuration{GenericConfiguration: &config.VolcanoAgentConfiguration{
				KubeClient:   fakeClient,
				KubeNodeName: "test-node",
				NodeHasSynced: func() bool {
					return false
				},
			}},
			expectedNode: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Status: v1.NodeStatus{Allocatable: map[v1.ResourceName]resource.Quantity{
					apis.GetExtendResourceCPU():    *resource.NewQuantity(100, resource.DecimalSI),
					apis.GetExtendResourceMemory(): *resource.NewQuantity(100, resource.BinarySI),
				}, Capacity: map[v1.ResourceName]resource.Quantity{
					apis.GetExtendResourceCPU():    *resource.NewQuantity(100, resource.DecimalSI),
					apis.GetExtendResourceMemory(): *resource.NewQuantity(100, resource.BinarySI),
				}}},
			nodeModifiers: []Modifier{updateNodeOverSoldStatus(apis.Resource{
				v1.ResourceCPU:    100,
				v1.ResourceMemory: 100,
			})},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, update(tt.config, tt.nodeModifiers))
			node, err := tt.config.GenericConfiguration.KubeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
			assert.NoError(t, err)
			assert.Equalf(t, tt.expectedNode, node, "update")
		})
	}
}
