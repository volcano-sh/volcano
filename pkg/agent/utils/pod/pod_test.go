package pod

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	k8sfeature "k8s.io/kubernetes/pkg/features"
)

func makePodWithCPURequest(containerCPU, podLevelCPU string) *v1.Pod {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse(containerCPU),
						},
					},
				},
			},
		},
	}
	if podLevelCPU != "" {
		pod.Spec.Resources = &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse(podLevelCPU),
			},
		}
	}
	return pod
}

func makePodWithNativeSidecarCPU(containerCPU, sidecarCPU string) *v1.Pod {
	always := v1.ContainerRestartPolicyAlways
	return &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse(containerCPU),
				}},
			}},
			InitContainers: []v1.Container{{
				RestartPolicy: &always,
				Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse(sidecarCPU),
				}},
			}},
		},
	}
}

func TestSortedPodsByRequestCPU_Less(t *testing.T) {
	pod1 := makePodWithCPURequest("1", "") // container-only, cpu=1
	pod2 := makePodWithCPURequest("2", "") // container-only, cpu=2
	pod3 := makePodWithCPURequest("3", "") // container-only, cpu=3
	podWithSidecar := makePodWithNativeSidecarCPU("1", "2")
	// Pod-level resources value should override container-level sum when the
	// PodLevelResources feature gate is enabled.
	podLevel := makePodWithCPURequest("1", "5") // pod-level cpu=5

	tests := []struct {
		name              string
		s                 SortedPodsByRequestCPU
		i, j              int
		podLevelResources bool
		expected          bool
	}{
		{
			name:     "higher cpu request comes first, gate off",
			s:        SortedPodsByRequestCPU{pod2, pod1},
			i:        0,
			j:        1,
			expected: true, // 2 > 1
		},
		{
			name:     "lower cpu request does not come first, gate off",
			s:        SortedPodsByRequestCPU{pod1, pod2},
			i:        0,
			j:        1,
			expected: false, // 1 > 2 is false
		},
		{
			name:     "equal cpu requests are not less than each other, gate off",
			s:        SortedPodsByRequestCPU{pod1, pod1},
			i:        0,
			j:        1,
			expected: false, // 1 > 1 is false
		},
		{
			name:              "pod-level request overrides container request, gate on",
			s:                 SortedPodsByRequestCPU{podLevel, pod3},
			i:                 0,
			j:                 1,
			podLevelResources: true,
			expected:          true, // 5 > 3
		},
		{
			name:              "pod-level request ignored when gate off, falls back to container request",
			s:                 SortedPodsByRequestCPU{podLevel, pod3},
			i:                 0,
			j:                 1,
			podLevelResources: false,
			expected:          false, // 1 > 3 is false
		},
		{
			name:              "both pods with pod-level requests, compare by pod-level values, gate on",
			s:                 SortedPodsByRequestCPU{pod3, podLevel},
			i:                 0,
			j:                 1,
			podLevelResources: true,
			expected:          false, // 3 > 5 is false
		},
		{
			name:              "pod without pod-level set falls back to container request when gate on",
			s:                 SortedPodsByRequestCPU{pod3, pod1},
			i:                 0,
			j:                 1,
			podLevelResources: true,
			expected:          true, // 3 > 1
		},
		{
			name:     "native sidecar is included when sorting cpu requests",
			s:        SortedPodsByRequestCPU{podWithSidecar, pod2},
			i:        0,
			j:        1,
			expected: true, // 1 app + 2 sidecar > 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.PodLevelResources, tt.podLevelResources)

			got := tt.s.Less(tt.i, tt.j)
			if got != tt.expected {
				t.Fatalf("expected Less(%d, %d)=%v, got %v", tt.i, tt.j, tt.expected, got)
			}
		})
	}
}

func makePodWithMemoryRequest(containerMemory, podLevelMemory string) *v1.Pod {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceMemory: resource.MustParse(containerMemory),
						},
					},
				},
			},
		},
	}
	if podLevelMemory != "" {
		pod.Spec.Resources = &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse(podLevelMemory),
			},
		}
	}
	return pod
}

func TestSortedPodsByRequestMemory_Less(t *testing.T) {
	mem1 := makePodWithMemoryRequest("1Gi", "") // container-only, mem=1Gi
	mem2 := makePodWithMemoryRequest("2Gi", "") // container-only, mem=2Gi
	mem3 := makePodWithMemoryRequest("3Gi", "") // container-only, mem=3Gi
	// Pod-level resources value should override container-level sum when the
	// PodLevelResources feature gate is enabled.
	memLevel := makePodWithMemoryRequest("1Gi", "5Gi") // pod-level mem=5Gi

	tests := []struct {
		name              string
		s                 SortedPodsByRequestMemory
		i, j              int
		podLevelResources bool
		expected          bool
	}{
		{
			name:     "higher memory request comes first, gate off",
			s:        SortedPodsByRequestMemory{mem2, mem1},
			i:        0,
			j:        1,
			expected: true, // 2Gi > 1Gi
		},
		{
			name:     "lower memory request does not come first, gate off",
			s:        SortedPodsByRequestMemory{mem1, mem2},
			i:        0,
			j:        1,
			expected: false, // 1Gi > 2Gi is false
		},
		{
			name:     "equal memory requests are not less than each other, gate off",
			s:        SortedPodsByRequestMemory{mem1, mem1},
			i:        0,
			j:        1,
			expected: false, // 1Gi > 1Gi is false
		},
		{
			name:              "pod-level request overrides container request, gate on",
			s:                 SortedPodsByRequestMemory{memLevel, mem3},
			i:                 0,
			j:                 1,
			podLevelResources: true,
			expected:          true, // 5Gi > 3Gi
		},
		{
			name:              "pod-level request ignored when gate off, falls back to container request",
			s:                 SortedPodsByRequestMemory{memLevel, mem3},
			i:                 0,
			j:                 1,
			podLevelResources: false,
			expected:          false, // 1Gi > 3Gi is false
		},
		{
			name:              "both pods with pod-level requests, compare by pod-level values, gate on",
			s:                 SortedPodsByRequestMemory{mem3, memLevel},
			i:                 0,
			j:                 1,
			podLevelResources: true,
			expected:          false, // 3Gi > 5Gi is false
		},
		{
			name:              "pod without pod-level set falls back to container request when gate on",
			s:                 SortedPodsByRequestMemory{mem3, mem1},
			i:                 0,
			j:                 1,
			podLevelResources: true,
			expected:          true, // 3Gi > 1Gi
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.PodLevelResources, tt.podLevelResources)

			got := tt.s.Less(tt.i, tt.j)
			if got != tt.expected {
				t.Fatalf("expected Less(%d, %d)=%v, got %v", tt.i, tt.j, tt.expected, got)
			}
		})
	}
}

func TestGetTotalRequestByType(t *testing.T) {
	cpu2 := resource.MustParse("2")

	// A filter that skips pods whose container[0] CPU request equals the
	// given "skip" quantity (only meaningful for CPU). Used to exercise the
	// filter short-circuit without depending on package-level filter vars.
	skipIfCPUEquals := func(q resource.Quantity) FilterPodsFunc {
		return func(pod *v1.Pod, resType v1.ResourceName) bool {
			if resType != v1.ResourceCPU || len(pod.Spec.Containers) == 0 {
				return false
			}
			got, ok := pod.Spec.Containers[0].Resources.Requests[v1.ResourceCPU]
			return ok && got.Cmp(q) == 0
		}
	}

	cpuPod := func(q string) *v1.Pod {
		return &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse(q)},
					},
				}},
			},
		}
	}
	memPod := func(q string) *v1.Pod {
		return &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceMemory: resource.MustParse(q)},
					},
				}},
			},
		}
	}
	cpuPodWithInit := func(containerCPU, initCPU string) *v1.Pod {
		return &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse(containerCPU)},
					},
				}},
				InitContainers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse(initCPU)},
					},
				}},
			},
		}
	}
	multiContainerCPUPod := func(c1, c2 string) *v1.Pod {
		return &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse(c1)}}},
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse(c2)}}},
				},
			},
		}
	}
	cpuPodLevel := func(containerCPU, podLevelCPU string) *v1.Pod {
		p := cpuPod(containerCPU)
		p.Spec.Resources = &v1.ResourceRequirements{
			Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse(podLevelCPU)},
		}
		return p
	}
	podWithSidecarsAndOverhead := func() *v1.Pod {
		always := v1.ContainerRestartPolicyAlways
		return &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1"),
						v1.ResourceMemory: resource.MustParse("128Mi"),
					}},
				}},
				InitContainers: []v1.Container{
					{
						RestartPolicy: &always,
						Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("1"),
							v1.ResourceMemory: resource.MustParse("64Mi"),
						}},
					},
					{
						RestartPolicy: &always,
						Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("500m"),
							v1.ResourceMemory: resource.MustParse("32Mi"),
						}},
					},
					{
						Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("3"),
							v1.ResourceMemory: resource.MustParse("256Mi"),
						}},
					},
				},
				Overhead: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("500m"),
					v1.ResourceMemory: resource.MustParse("64Mi"),
				},
			},
		}
	}

	tests := []struct {
		name              string
		pods              []*v1.Pod
		fns               []FilterPodsFunc
		resType           v1.ResourceName
		podLevelResources bool
		expected          resource.Quantity
	}{
		{
			name:     "empty pods returns zero",
			pods:     []*v1.Pod{},
			fns:      nil,
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("0"),
		},
		{
			name:     "sums cpu across pods",
			pods:     []*v1.Pod{cpuPod("1"), cpuPod("2")},
			fns:      nil,
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("3"),
		},
		{
			name:     "sums memory across pods",
			pods:     []*v1.Pod{memPod("1Gi"), memPod("2Gi")},
			fns:      nil,
			resType:  v1.ResourceMemory,
			expected: resource.MustParse("3Gi"),
		},
		{
			name:     "sums containers within a pod",
			pods:     []*v1.Pod{multiContainerCPUPod("1", "2")},
			fns:      nil,
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("3"),
		},
		{
			name:     "init container that exceeds container sum is used as the pod minimum",
			pods:     []*v1.Pod{cpuPodWithInit("1", "3")},
			fns:      nil,
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("3"),
		},
		{
			name:     "init container smaller than container sum does not reduce it",
			pods:     []*v1.Pod{cpuPodWithInit("2", "1")},
			fns:      nil,
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("2"),
		},
		{
			name:     "filter skips matching pods",
			pods:     []*v1.Pod{cpuPod("1"), cpuPod("2"), cpuPod("3")},
			fns:      []FilterPodsFunc{skipIfCPUEquals(cpu2)},
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("4"),
		},
		{
			name:              "pod-level request overrides container sum when gate on",
			pods:              []*v1.Pod{cpuPodLevel("1", "5")},
			fns:               nil,
			resType:           v1.ResourceCPU,
			podLevelResources: true,
			expected:          resource.MustParse("5"),
		},
		{
			name:              "pod-level request ignored when gate off",
			pods:              []*v1.Pod{cpuPodLevel("1", "5")},
			fns:               nil,
			resType:           v1.ResourceCPU,
			podLevelResources: false,
			expected:          resource.MustParse("1"),
		},
		{
			name:              "mixed: pod-level overrides only the pod that sets it, gate on",
			pods:              []*v1.Pod{cpuPod("1"), cpuPodLevel("2", "10"), cpuPod("3")},
			fns:               nil,
			resType:           v1.ResourceCPU,
			podLevelResources: true,
			expected:          resource.MustParse("14"),
		},
		{
			name:     "includes native sidecars ordinary init containers and pod overhead for cpu",
			pods:     []*v1.Pod{podWithSidecarsAndOverhead()},
			resType:  v1.ResourceCPU,
			expected: resource.MustParse("5"),
		},
		{
			name:     "includes native sidecars ordinary init containers and pod overhead for memory",
			pods:     []*v1.Pod{podWithSidecarsAndOverhead()},
			resType:  v1.ResourceMemory,
			expected: resource.MustParse("416Mi"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.PodLevelResources, tt.podLevelResources)

			got := getTotalRequestByType(tt.pods, tt.fns, tt.resType)
			if got.Cmp(tt.expected) != 0 {
				t.Fatalf("expected %s, got %s", tt.expected.String(), got.String())
			}
		})
	}
}
