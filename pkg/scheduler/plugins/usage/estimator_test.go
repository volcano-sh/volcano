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

package usage

import (
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const testEps = 1e-6

func TestCalcLoadCompositePercentage(t *testing.T) {
	tests := []struct {
		name         string
		cpuComposite float64
		memComposite float64
		cpuWeight    int
		memWeight    int
		expected     float64
	}{
		{
			name:         "equal weights",
			cpuComposite: 0.4,
			memComposite: 0.6,
			cpuWeight:    1,
			memWeight:    1,
			expected:     0.5,
		},
		{
			name:         "cpu weight dominant",
			cpuComposite: 0.8,
			memComposite: 0.2,
			cpuWeight:    3,
			memWeight:    1,
			expected:     0.65,
		},
		{
			name:         "zero weights",
			cpuComposite: 0.8,
			memComposite: 0.2,
			cpuWeight:    0,
			memWeight:    0,
			expected:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcLoadCompositePercentage(tt.cpuComposite, tt.memComposite, tt.cpuWeight, tt.memWeight)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("CalcLoadCompositePercentage(%v, %v, %v, %v) = %v, expected %v",
					tt.cpuComposite, tt.memComposite, tt.cpuWeight, tt.memWeight, result, tt.expected)
			}
		})
	}
}

func TestCalcAppliedRiskFactor(t *testing.T) {
	tests := []struct {
		name                    string
		loadCompositePercentage float64
		riskThreshold           float64
		riskFactor              float64
		expected                float64
	}{
		{
			name:                    "below threshold",
			loadCompositePercentage: 0.59,
			riskThreshold:           0.6,
			riskFactor:              1.2,
			expected:                1.0,
		},
		{
			name:                    "at threshold",
			loadCompositePercentage: 0.6,
			riskThreshold:           0.6,
			riskFactor:              1.2,
			expected:                1.2,
		},
		{
			name:                    "above threshold",
			loadCompositePercentage: 0.9,
			riskThreshold:           0.6,
			riskFactor:              1.2,
			expected:                1.2,
		},
		{
			name:                    "risk factor lower than one is ignored",
			loadCompositePercentage: 0.9,
			riskThreshold:           0.6,
			riskFactor:              0.8,
			expected:                1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcAppliedRiskFactor(tt.loadCompositePercentage, tt.riskThreshold, tt.riskFactor)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("CalcAppliedRiskFactor(%v, %v, %v) = %v, expected %v",
					tt.loadCompositePercentage, tt.riskThreshold, tt.riskFactor, result, tt.expected)
			}
		})
	}
}

func TestEstimatePodResource(t *testing.T) {
	tests := []struct {
		name              string
		request           float64
		limit             float64
		requestWeight     float64
		burstWeight       float64
		appliedRiskFactor float64
		expected          float64
	}{
		{
			name:              "default weights estimate request",
			request:           500,
			limit:             2000,
			requestWeight:     1.0,
			burstWeight:       0.0,
			appliedRiskFactor: 1.0,
			expected:          500,
		},
		{
			name:              "zero weights estimate zero",
			request:           500,
			limit:             2000,
			requestWeight:     0.0,
			burstWeight:       0.0,
			appliedRiskFactor: 1.0,
			expected:          0,
		},
		{
			name:              "request and burst weights cover full limit",
			request:           500,
			limit:             2000,
			requestWeight:     1.0,
			burstWeight:       1.0,
			appliedRiskFactor: 1.0,
			expected:          2000,
		},
		{
			name:              "custom burst weight",
			request:           500,
			limit:             2000,
			requestWeight:     0.5,
			burstWeight:       0.2,
			appliedRiskFactor: 1.0,
			expected:          550, // 500*0.5 + (2000-500)*0.2
		},
		{
			name:              "risk factor applies and clamps to limit",
			request:           500,
			limit:             600,
			requestWeight:     1.0,
			burstWeight:       0.0,
			appliedRiskFactor: 1.2,
			expected:          600,
		},
		{
			name:              "missing limit uses request as effective limit",
			request:           500,
			limit:             0,
			requestWeight:     1.0,
			burstWeight:       1.0,
			appliedRiskFactor: 1.2,
			expected:          500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimatePodResource(tt.request, tt.limit, tt.requestWeight, tt.burstWeight, tt.appliedRiskFactor)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("EstimatePodResource() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestEstimateBestEffortResource(t *testing.T) {
	tests := []struct {
		name              string
		configuredValue   float64
		appliedRiskFactor float64
		expected          float64
	}{
		{
			name:              "base configured value",
			configuredValue:   250,
			appliedRiskFactor: 1.0,
			expected:          250,
		},
		{
			name:              "risk factor applies",
			configuredValue:   250,
			appliedRiskFactor: 1.2,
			expected:          300,
		},
		{
			name:              "negative configured value is clamped to zero",
			configuredValue:   -1,
			appliedRiskFactor: 1.2,
			expected:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateBestEffortResource(tt.configuredValue, tt.appliedRiskFactor)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("EstimateBestEffortResource() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCalcCompositeUtilization(t *testing.T) {
	tests := []struct {
		name            string
		realLoadPercent float64
		shadowEstAbs    float64
		capacity        float64
		expected        float64
	}{
		{
			name:            "no shadow load, 50% real",
			realLoadPercent: 50,
			shadowEstAbs:    0,
			capacity:        8000,
			expected:        0.5,
		},
		{
			name:            "50% real + shadow brings to 75%",
			realLoadPercent: 50,
			shadowEstAbs:    2000,
			capacity:        8000,
			expected:        0.75, // (4000 + 2000) / 8000 = 0.75
		},
		{
			name:            "overflow clamped to 1.0",
			realLoadPercent: 80,
			shadowEstAbs:    5000,
			capacity:        8000,
			expected:        1.0, // (6400 + 5000) / 8000 > 1.0
		},
		{
			name:            "zero capacity returns 0",
			realLoadPercent: 50,
			shadowEstAbs:    1000,
			capacity:        0,
			expected:        0,
		},
		{
			name:            "zero everything",
			realLoadPercent: 0,
			shadowEstAbs:    0,
			capacity:        8000,
			expected:        0,
		},
		{
			name:            "100% real load, no shadow",
			realLoadPercent: 100,
			shadowEstAbs:    0,
			capacity:        8000,
			expected:        1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcCompositeUtilization(tt.realLoadPercent, tt.shadowEstAbs, tt.capacity)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("CalcCompositeUtilization(%v, %v, %v) = %v, expected %v",
					tt.realLoadPercent, tt.shadowEstAbs, tt.capacity, result, tt.expected)
			}
		})
	}
}

func TestCalcNodeScore(t *testing.T) {
	tests := []struct {
		name        string
		cpuComp     float64
		memComp     float64
		cpuWeight   int
		memWeight   int
		usageWeight int
		expected    float64
	}{
		{
			name:        "zero utilization - max score with usageWeight=1",
			cpuComp:     0,
			memComp:     0,
			cpuWeight:   1,
			memWeight:   1,
			usageWeight: 1,
			expected:    100, // MaxNodeScore = 100
		},
		{
			name:        "full utilization - zero score",
			cpuComp:     1.0,
			memComp:     1.0,
			cpuWeight:   1,
			memWeight:   1,
			usageWeight: 1,
			expected:    0,
		},
		{
			name:        "50% utilization both - score 250 with usageWeight=5",
			cpuComp:     0.5,
			memComp:     0.5,
			cpuWeight:   1,
			memWeight:   1,
			usageWeight: 5,
			expected:    250, // 50 * 5
		},
		{
			name:        "cpu 30%, mem 50%, equal weights, usageWeight=5",
			cpuComp:     0.3,
			memComp:     0.5,
			cpuWeight:   1,
			memWeight:   1,
			usageWeight: 5,
			expected:    300, // ((1-0.3)*1 + (1-0.5)*1) / 2 * 100 * 5 = 60 * 5 = 300
		},
		{
			name:        "cpu 60%, mem 50%, cpu weight=2, mem weight=8, usageWeight=5",
			cpuComp:     0.6,
			memComp:     0.5,
			cpuWeight:   2,
			memWeight:   8,
			usageWeight: 5,
			expected:    240, // ((1-0.6)*2 + (1-0.5)*8) / 10 * 100 * 5 = 48 * 5 = 240
		},
		{
			name:        "zero weights returns 0",
			cpuComp:     0.5,
			memComp:     0.5,
			cpuWeight:   0,
			memWeight:   0,
			usageWeight: 5,
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcNodeScore(tt.cpuComp, tt.memComp, tt.cpuWeight, tt.memWeight, tt.usageWeight)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("CalcNodeScore(%v, %v, %v, %v, %v) = %v, expected %v",
					tt.cpuComp, tt.memComp, tt.cpuWeight, tt.memWeight, tt.usageWeight, result, tt.expected)
			}
		})
	}
}

func TestGetPodCPURequestLimit(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		expectedReq float64
		expectedLim float64
	}{
		{
			name: "single container with request and limit",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("500m"),
								},
								Limits: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1000m"),
								},
							},
						},
					},
				},
			},
			expectedReq: 500,
			expectedLim: 1000,
		},
		{
			name: "multiple containers - requests/limits summed",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("200m")},
								Limits:   v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("300m")},
								Limits:   v1.ResourceList{v1.ResourceCPU: resource.MustParse("700m")},
							},
						},
					},
				},
			},
			expectedReq: 500,
			expectedLim: 1200,
		},
		{
			name: "init container takes max",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2000m")},
								Limits:   v1.ResourceList{v1.ResourceCPU: resource.MustParse("3000m")},
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
								Limits:   v1.ResourceList{v1.ResourceCPU: resource.MustParse("1000m")},
							},
						},
					},
				},
			},
			expectedReq: 2000, // max(500, 2000)
			expectedLim: 3000, // max(1000, 3000)
		},
		{
			name: "no resources specified (BestEffort)",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			expectedReq: 0,
			expectedLim: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, lim := getPodCPURequestLimit(tt.pod)
			if math.Abs(req-tt.expectedReq) > testEps {
				t.Errorf("getPodCPURequestLimit() request = %v, expected %v", req, tt.expectedReq)
			}
			if math.Abs(lim-tt.expectedLim) > testEps {
				t.Errorf("getPodCPURequestLimit() limit = %v, expected %v", lim, tt.expectedLim)
			}
		})
	}
}

func TestGetPodMemRequestLimit(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		expectedReq float64
		expectedLim float64
	}{
		{
			name: "single container with request and limit",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: v1.ResourceList{
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
			expectedReq: 1073741824, // 1Gi in bytes
			expectedLim: 2147483648, // 2Gi in bytes
		},
		{
			name: "no resources specified (BestEffort)",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			expectedReq: 0,
			expectedLim: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, lim := getPodMemRequestLimit(tt.pod)
			if math.Abs(req-tt.expectedReq) > testEps {
				t.Errorf("getPodMemRequestLimit() request = %v, expected %v", req, tt.expectedReq)
			}
			if math.Abs(lim-tt.expectedLim) > testEps {
				t.Errorf("getPodMemRequestLimit() limit = %v, expected %v", lim, tt.expectedLim)
			}
		})
	}
}
