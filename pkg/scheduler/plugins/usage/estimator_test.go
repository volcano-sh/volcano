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

func TestCalcDynamicSigma(t *testing.T) {
	tests := []struct {
		name            string
		sigmaBase       float64
		watermark       float64
		sensitivity     float64
		nodeUtilization float64
		expectedMin     float64
		expectedMax     float64
	}{
		{
			name:            "node completely idle - sigma near sigmaBase",
			sigmaBase:       0.15,
			watermark:       0.5,
			sensitivity:     12.0,
			nodeUtilization: 0.0,
			expectedMin:     0.15,
			expectedMax:     0.16, // very close to sigmaBase when far below watermark
		},
		{
			name:            "node at watermark - sigma at midpoint",
			sigmaBase:       0.15,
			watermark:       0.5,
			sensitivity:     12.0,
			nodeUtilization: 0.5,
			expectedMin:     0.55, // sigmaBase + (1-sigmaBase)*0.5 = 0.15 + 0.425 = 0.575
			expectedMax:     0.60,
		},
		{
			name:            "node fully loaded - sigma near 1.0",
			sigmaBase:       0.15,
			watermark:       0.5,
			sensitivity:     12.0,
			nodeUtilization: 1.0,
			expectedMin:     0.99,
			expectedMax:     1.0,
		},
		{
			name:            "node at 30% utilization - sigma still relatively low",
			sigmaBase:       0.15,
			watermark:       0.5,
			sensitivity:     12.0,
			nodeUtilization: 0.3,
			expectedMin:     0.15,
			expectedMax:     0.25,
		},
		{
			name:            "node at 70% utilization - sigma relatively high",
			sigmaBase:       0.15,
			watermark:       0.5,
			sensitivity:     12.0,
			nodeUtilization: 0.7,
			expectedMin:     0.90,
			expectedMax:     1.0,
		},
		{
			name:            "low sensitivity - smoother transition",
			sigmaBase:       0.15,
			watermark:       0.5,
			sensitivity:     2.0,
			nodeUtilization: 0.5,
			expectedMin:     0.55,
			expectedMax:     0.60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcDynamicSigma(tt.sigmaBase, tt.watermark, tt.sensitivity, tt.nodeUtilization)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("CalcDynamicSigma(%v, %v, %v, %v) = %v, expected in [%v, %v]",
					tt.sigmaBase, tt.watermark, tt.sensitivity, tt.nodeUtilization,
					result, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestCalcDynamicSigma_Monotonicity(t *testing.T) {
	// Sigma should be monotonically increasing with node utilization
	sigmaBase := 0.15
	watermark := 0.5
	sensitivity := 12.0

	prevSigma := 0.0
	for u := 0.0; u <= 1.0; u += 0.05 {
		sigma := CalcDynamicSigma(sigmaBase, watermark, sensitivity, u)
		if sigma < prevSigma-testEps {
			t.Errorf("Sigma not monotonically increasing: at u=%v, sigma=%v < prevSigma=%v", u, sigma, prevSigma)
		}
		prevSigma = sigma
	}
}

func TestCalcNodeRealUtilization(t *testing.T) {
	tests := []struct {
		name           string
		realCPUPercent float64
		realMemPercent float64
		cpuWeight      int
		memWeight      int
		expected       float64
	}{
		{
			name:           "equal weights, equal utilization",
			realCPUPercent: 50,
			realMemPercent: 50,
			cpuWeight:      1,
			memWeight:      1,
			expected:       0.5,
		},
		{
			name:           "equal weights, different utilization",
			realCPUPercent: 60,
			realMemPercent: 40,
			cpuWeight:      1,
			memWeight:      1,
			expected:       0.5,
		},
		{
			name:           "cpu weight dominant",
			realCPUPercent: 80,
			realMemPercent: 20,
			cpuWeight:      3,
			memWeight:      1,
			expected:       0.65, // (0.8*3 + 0.2*1) / 4 = 2.6/4 = 0.65
		},
		{
			name:           "zero weights",
			realCPUPercent: 80,
			realMemPercent: 20,
			cpuWeight:      0,
			memWeight:      0,
			expected:       0,
		},
		{
			name:           "zero utilization",
			realCPUPercent: 0,
			realMemPercent: 0,
			cpuWeight:      1,
			memWeight:      1,
			expected:       0,
		},
		{
			name:           "full utilization",
			realCPUPercent: 100,
			realMemPercent: 100,
			cpuWeight:      1,
			memWeight:      1,
			expected:       1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcNodeRealUtilization(tt.realCPUPercent, tt.realMemPercent, tt.cpuWeight, tt.memWeight)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("CalcNodeRealUtilization(%v, %v, %v, %v) = %v, expected %v",
					tt.realCPUPercent, tt.realMemPercent, tt.cpuWeight, tt.memWeight, result, tt.expected)
			}
		})
	}
}

func TestEstimatePodResource(t *testing.T) {
	tests := []struct {
		name            string
		request         float64
		limit           float64
		sigmaDynamic    float64
		nodeCap         float64
		beRatio         float64
		bePenalty       float64
		bestEffortCount int
		isBestEffort    bool
		expected        float64
	}{
		{
			name:            "Guaranteed pod (limit == request), sigma=0.5",
			request:         1000,
			limit:           1000,
			sigmaDynamic:    0.5,
			nodeCap:         8000,
			beRatio:         0.1,
			bePenalty:       1.2,
			bestEffortCount: 0,
			isBestEffort:    false,
			expected:        500, // request * sigmaDynamic = 1000 * 0.5
		},
		{
			name:            "Burstable pod (limit > request), sigma=0.3",
			request:         500,
			limit:           2000,
			sigmaDynamic:    0.3,
			nodeCap:         8000,
			beRatio:         0.1,
			bePenalty:       1.2,
			bestEffortCount: 0,
			isBestEffort:    false,
			expected:        950, // 500 + (2000-500)*0.3 = 500 + 450 = 950
		},
		{
			name:            "Burstable pod, sigma=1.0 (fully conservative)",
			request:         500,
			limit:           2000,
			sigmaDynamic:    1.0,
			nodeCap:         8000,
			beRatio:         0.1,
			bePenalty:       1.2,
			bestEffortCount: 0,
			isBestEffort:    false,
			expected:        2000, // 500 + (2000-500)*1.0 = 2000
		},
		{
			name:            "BestEffort pod, first on node",
			request:         0,
			limit:           0,
			sigmaDynamic:    0.5,
			nodeCap:         8000,
			beRatio:         0.1,
			bePenalty:       1.2,
			bestEffortCount: 0,
			isBestEffort:    true,
			expected:        800, // 8000 * 0.1 * 1.2^0 = 800
		},
		{
			name:            "BestEffort pod, 3 already on node",
			request:         0,
			limit:           0,
			sigmaDynamic:    0.5,
			nodeCap:         8000,
			beRatio:         0.1,
			bePenalty:       1.2,
			bestEffortCount: 3,
			isBestEffort:    true,
			expected:        8000 * 0.1 * math.Pow(1.2, 3), // 800 * 1.728 = 1382.4
		},
		{
			name:            "BestEffort pod, 10 already on node (exponential penalty)",
			request:         0,
			limit:           0,
			sigmaDynamic:    0.5,
			nodeCap:         8000,
			beRatio:         0.1,
			bePenalty:       1.2,
			bestEffortCount: 10,
			isBestEffort:    true,
			expected:        8000 * 0.1 * math.Pow(1.2, 10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimatePodResource(tt.request, tt.limit, tt.sigmaDynamic, tt.nodeCap,
				tt.beRatio, tt.bePenalty, tt.bestEffortCount, tt.isBestEffort)
			if math.Abs(result-tt.expected) > testEps {
				t.Errorf("EstimatePodResource() = %v, expected %v", result, tt.expected)
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
