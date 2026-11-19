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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

const testGPU v1.ResourceName = "nvidia.com/gpu"

func mapGPU(rName v1.ResourceName) bool { return rName == testGPU }

func TestExcludeDRADelegatedResources(t *testing.T) {
	dramNode := buildNode("dra", nil, BuildResourceList("4", "8Gi", ScalarResource{Name: "pods", Value: "10"}))
	pluginNode := buildNode("plugin", nil, BuildResourceList("4", "8Gi",
		ScalarResource{Name: "pods", Value: "10"}, ScalarResource{Name: string(testGPU), Value: "2"}))

	tests := []struct {
		name         string
		resolver     DRAExtendedResourceResolver
		node         *v1.Node
		req          *Resource
		wantGPU      bool
		wantSameResp bool
	}{
		{
			name:         "resolver not installed keeps the request as is",
			node:         dramNode,
			req:          NewResource(BuildResourceList("1", "1Gi", ScalarResource{Name: string(testGPU), Value: "1"})),
			wantGPU:      true,
			wantSameResp: true,
		},
		{
			name:     "DRA-only node and mapped resource is excluded",
			resolver: mapGPU,
			node:     dramNode,
			req:      NewResource(BuildResourceList("1", "1Gi", ScalarResource{Name: string(testGPU), Value: "1"})),
		},
		{
			name:         "node advertising the resource in allocatable keeps it",
			resolver:     mapGPU,
			node:         pluginNode,
			req:          NewResource(BuildResourceList("1", "1Gi", ScalarResource{Name: string(testGPU), Value: "1"})),
			wantGPU:      true,
			wantSameResp: true,
		},
		{
			name:         "resource without a DeviceClass mapping is kept",
			resolver:     func(v1.ResourceName) bool { return false },
			node:         dramNode,
			req:          NewResource(BuildResourceList("1", "1Gi", ScalarResource{Name: string(testGPU), Value: "1"})),
			wantGPU:      true,
			wantSameResp: true,
		},
		{
			name:         "native resource is never excluded",
			resolver:     func(v1.ResourceName) bool { return true },
			node:         dramNode,
			req:          NewResource(BuildResourceList("1", "1Gi", ScalarResource{Name: "ephemeral-storage", Value: "1Gi"})),
			wantSameResp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { SetDRAExtendedResourceResolver(nil) })
			SetDRAExtendedResourceResolver(tt.resolver)

			before := tt.req.Clone()
			got := ExcludeDRADelegatedResources(tt.req, tt.node)

			_, hasGPU := got.ScalarResources[testGPU]
			assert.Equal(t, tt.wantGPU, hasGPU)
			assert.Equal(t, tt.wantSameResp, got == tt.req, "unchanged requests must not be copied")
			assert.Equal(t, before.MilliCPU, got.MilliCPU)
			assert.Equal(t, before.Memory, got.Memory)
			// the input request must never be mutated
			assert.Equal(t, before.ScalarResources, tt.req.ScalarResources)
		})
	}
}
