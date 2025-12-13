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

package capacitycard

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestHasCardResource(t *testing.T) {
	p := &Plugin{
		cardNameToResourceName: map[corev1.ResourceName]corev1.ResourceName{
			"Tesla-V100": "nvidia.com/gpu",
		},
	}

	tests := []struct {
		name           string
		cardResource   *api.Resource
		scalarResource *api.Resource
		expected       bool
	}{
		{
			name:           "both resources nil",
			cardResource:   nil,
			scalarResource: nil,
			expected:       false,
		},
		{
			name: "card resource has scalar resources",
			cardResource: &api.Resource{
				ScalarResources: map[corev1.ResourceName]float64{
					"Tesla-V100": 1.0,
				},
			},
			scalarResource: nil,
			expected:       true,
		},
		{
			name:         "scalar resource has card resource",
			cardResource: nil,
			scalarResource: &api.Resource{
				ScalarResources: map[corev1.ResourceName]float64{
					"nvidia.com/gpu": 1.0,
				},
			},
			expected: true,
		},
		{
			name:         "scalar resource has non-card resource",
			cardResource: nil,
			scalarResource: &api.Resource{
				ScalarResources: map[corev1.ResourceName]float64{
					"cpu": 1.0,
				},
			},
			expected: false,
		},
		{
			name:         "scalar resource empty",
			cardResource: nil,
			scalarResource: &api.Resource{
				ScalarResources: map[corev1.ResourceName]float64{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.HasCardResource(tt.cardResource, tt.scalarResource)
			if result != tt.expected {
				t.Errorf("HasCardResource() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsCardUnlimitedCpuMemory(t *testing.T) {
	p := &Plugin{}

	tests := []struct {
		name     string
		tiers    []conf.Tier
		expected bool
	}{
		{
			name:     "no tiers",
			tiers:    []conf.Tier{},
			expected: false,
		},
		{
			name: "tiers without capacity-card plugin",
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: "other-plugin",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "tiers with capacity-card plugin but no cardUnlimitedCpuMemory argument",
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: PluginName,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "tiers with capacity-card plugin and cardUnlimitedCpuMemory false",
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: PluginName,
							Arguments: map[string]interface{}{
								cardUnlimitedCpuMemory: false,
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "tiers with capacity-card plugin and cardUnlimitedCpuMemory true",
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: PluginName,
							Arguments: map[string]interface{}{
								cardUnlimitedCpuMemory: true,
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "multiple tiers with capacity-card plugin in second tier",
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: "other-plugin",
						},
					},
				},
				{
					Plugins: []conf.PluginOption{
						{
							Name: PluginName,
							Arguments: map[string]interface{}{
								cardUnlimitedCpuMemory: true,
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "cardUnlimitedCpuMemory argument is not bool",
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: PluginName,
							Arguments: map[string]interface{}{
								cardUnlimitedCpuMemory: "true",
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := &framework.Session{
				Tiers: tt.tiers,
			}
			result := p.IsCardUnlimitedCpuMemory(ssn)
			if result != tt.expected {
				t.Errorf("IsCardUnlimitedCpuMemory() = %v, want %v", result, tt.expected)
			}
		})
	}
}
