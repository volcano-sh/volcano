/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

	v1 "k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestGetCardResourceFromAnnotations tests the GetCardResourceFromAnnotations function
func TestGetCardResourceFromAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		key         string
		expected    *api.Resource
	}{
		{
			name: "valid card resource with single card type",
			annotations: map[string]string{
				"volcano.sh/card-request": `{"NVIDIA-GTX-4090": 2}`,
			},
			key: "volcano.sh/card-request",
			expected: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 2000, // 2 * cardCountQuantityMultiplier (1000)
				},
			},
		},
		{
			name: "valid card resource with multiple card types",
			annotations: map[string]string{
				"volcano.sh/card-request": `{"NVIDIA-GTX-4090": 2, "NVIDIA-H200": 4}`,
			},
			key: "volcano.sh/card-request",
			expected: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 2000,
					"NVIDIA-H200":     4000,
				},
			},
		},
		{
			name: "key not found in annotations",
			annotations: map[string]string{
				"other-key": `{"NVIDIA-GTX-4090": 2}`,
			},
			key: "volcano.sh/card-request",
			expected: &api.Resource{
				MilliCPU:        0,
				Memory:          0,
				ScalarResources: map[v1.ResourceName]float64{},
			},
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			key:         "volcano.sh/card-request",
			expected: &api.Resource{
				MilliCPU:        0,
				Memory:          0,
				ScalarResources: map[v1.ResourceName]float64{},
			},
		},
		{
			name:        "nil annotations",
			annotations: nil,
			key:         "volcano.sh/card-request",
			expected: &api.Resource{
				MilliCPU:        0,
				Memory:          0,
				ScalarResources: map[v1.ResourceName]float64{},
			},
		},
		{
			name: "invalid json format",
			annotations: map[string]string{
				"volcano.sh/card-request": `{"NVIDIA-GTX-4090": 2`,
			},
			key: "volcano.sh/card-request",
			expected: &api.Resource{
				MilliCPU:        0,
				Memory:          0,
				ScalarResources: map[v1.ResourceName]float64{},
			},
		},
		{
			name: "invalid json content - not a map",
			annotations: map[string]string{
				"volcano.sh/card-request": `["NVIDIA-GTX-4090"]`,
			},
			key: "volcano.sh/card-request",
			expected: &api.Resource{
				MilliCPU:        0,
				Memory:          0,
				ScalarResources: map[v1.ResourceName]float64{},
			},
		},
		{
			name: "zero card count",
			annotations: map[string]string{
				"volcano.sh/card-request": `{"NVIDIA-GTX-4090": 0}`,
			},
			key: "volcano.sh/card-request",
			expected: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCardResourceFromAnnotations(tt.annotations, tt.key)

			if result == nil {
				t.Fatalf("expected non-nil result")
			}

			if result.ScalarResources == nil {
				result.ScalarResources = map[v1.ResourceName]float64{}
			}

			if len(result.ScalarResources) != len(tt.expected.ScalarResources) {
				t.Errorf("expected %d scalar resources, got %d",
					len(tt.expected.ScalarResources), len(result.ScalarResources))
			}

			for name, expectedVal := range tt.expected.ScalarResources {
				if actualVal, ok := result.ScalarResources[name]; !ok {
					t.Errorf("expected scalar resource %s not found", name)
				} else if actualVal != expectedVal {
					t.Errorf("for scalar resource %s: expected %f, got %f",
						name, expectedVal, actualVal)
				}
			}
		})
	}
}

// TestCheckSingleScalarResource tests the CheckSingleScalarResource function
func TestCheckSingleScalarResource(t *testing.T) {
	tests := []struct {
		name             string
		scalarName       v1.ResourceName
		scalarQuant      float64
		toBeUsedResource *api.Resource
		queueCapability  *api.Resource
		expectedResult   CheckSingleScalarResourceResult
	}{
		{
			name:        "sufficient single card resource",
			scalarName:  "NVIDIA-GTX-4090",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 3000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   true,
				ToBeUsedScalarQuant:  3000,
				QueueCapabilityQuant: 5000,
			},
		},
		{
			name:        "insufficient single card resource",
			scalarName:  "NVIDIA-GTX-4090",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 6000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   false,
				NoEnoughScalarName:   "NVIDIA-GTX-4090",
				NoEnoughScalarCount:  2000,
				ToBeUsedScalarQuant:  6000,
				QueueCapabilityQuant: 5000,
			},
		},
		{
			name:        "multi-card request with first card sufficient",
			scalarName:  "NVIDIA-GTX-4090|NVIDIA-H200",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090|NVIDIA-H200": 2000,
					"NVIDIA-GTX-4090":             3000,
					"NVIDIA-H200":                 5000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 10000,
					"NVIDIA-H200":     8000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   true,
				ToBeUsedScalarQuant:  5000, // 3000 + 2000
				QueueCapabilityQuant: 10000,
			},
		},
		{
			name:        "multi-card request with second card sufficient",
			scalarName:  "NVIDIA-GTX-4090|NVIDIA-H200",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090|NVIDIA-H200": 2000,
					"NVIDIA-GTX-4090":             9000,
					"NVIDIA-H200":                 3000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 10000,
					"NVIDIA-H200":     8000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   true,
				ToBeUsedScalarQuant:  5000, // 3000 + 2000
				QueueCapabilityQuant: 8000,
			},
		},
		{
			name:        "multi-card request with all cards insufficient",
			scalarName:  "NVIDIA-GTX-4090|NVIDIA-H200",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090|NVIDIA-H200": 2000,
					"NVIDIA-GTX-4090":             9000,
					"NVIDIA-H200":                 7000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 10000,
					"NVIDIA-H200":     8000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                  false,
				NoEnoughScalarName:  "NVIDIA-GTX-4090|NVIDIA-H200",
				NoEnoughScalarCount: 2000,
			},
		},
		{
			name:        "zero scalar quantity request",
			scalarName:  "NVIDIA-GTX-4090",
			scalarQuant: 0,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 6000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   true,
				ToBeUsedScalarQuant:  6000,
				QueueCapabilityQuant: 5000,
			},
		},
		{
			name:        "resource not in toBeUsedResource",
			scalarName:  "NVIDIA-H200",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-H200": 5000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   true,
				ToBeUsedScalarQuant:  0,
				QueueCapabilityQuant: 5000,
			},
		},
		{
			name:        "resource not in queueCapability",
			scalarName:  "NVIDIA-H200",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-H200": 3000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   false,
				NoEnoughScalarName:   "NVIDIA-H200",
				NoEnoughScalarCount:  2000,
				ToBeUsedScalarQuant:  3000,
				QueueCapabilityQuant: 0,
			},
		},
		{
			name:        "boundary case - exactly at capacity",
			scalarName:  "NVIDIA-GTX-4090",
			scalarQuant: 2000,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5000,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   true,
				ToBeUsedScalarQuant:  5000,
				QueueCapabilityQuant: 5000,
			},
		},
		{
			name:        "boundary case - one unit over capacity",
			scalarName:  "NVIDIA-GTX-4090",
			scalarQuant: 1,
			toBeUsedResource: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5001,
				},
			},
			queueCapability: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					"NVIDIA-GTX-4090": 5000,
				},
			},
			expectedResult: CheckSingleScalarResourceResult{
				Ok:                   false,
				NoEnoughScalarName:   "NVIDIA-GTX-4090",
				NoEnoughScalarCount:  1,
				ToBeUsedScalarQuant:  5001,
				QueueCapabilityQuant: 5000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckSingleScalarResource(
				tt.scalarName,
				tt.scalarQuant,
				tt.toBeUsedResource,
				tt.queueCapability,
			)

			if result.Ok != tt.expectedResult.Ok {
				t.Errorf("expected Ok=%v, got Ok=%v", tt.expectedResult.Ok, result.Ok)
			}

			if result.NoEnoughScalarName != tt.expectedResult.NoEnoughScalarName {
				t.Errorf("expected NoEnoughScalarName=%s, got NoEnoughScalarName=%s",
					tt.expectedResult.NoEnoughScalarName, result.NoEnoughScalarName)
			}

			if result.NoEnoughScalarCount != tt.expectedResult.NoEnoughScalarCount {
				t.Errorf("expected NoEnoughScalarCount=%f, got NoEnoughScalarCount=%f",
					tt.expectedResult.NoEnoughScalarCount, result.NoEnoughScalarCount)
			}

			if tt.expectedResult.ToBeUsedScalarQuant > 0 || tt.expectedResult.QueueCapabilityQuant > 0 {
				if result.ToBeUsedScalarQuant != tt.expectedResult.ToBeUsedScalarQuant {
					t.Errorf("expected ToBeUsedScalarQuant=%f, got ToBeUsedScalarQuant=%f",
						tt.expectedResult.ToBeUsedScalarQuant, result.ToBeUsedScalarQuant)
				}

				if result.QueueCapabilityQuant != tt.expectedResult.QueueCapabilityQuant {
					t.Errorf("expected QueueCapabilityQuant=%f, got QueueCapabilityQuant=%f",
						tt.expectedResult.QueueCapabilityQuant, result.QueueCapabilityQuant)
				}
			}
		})
	}
}
