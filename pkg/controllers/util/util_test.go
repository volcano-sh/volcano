package util

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGetPodQuotaUsage(t *testing.T) {
	resList := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1000m"),
		corev1.ResourceMemory: resource.MustParse("1000Mi"),
		"nvidia.com/gpu":      resource.MustParse("1"),
		"hugepages-test":      resource.MustParse("2000"),
	}

	container := corev1.Container{
		Resources: corev1.ResourceRequirements{
			Requests: resList,
			Limits:   resList,
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{container, container},
		},
	}

	expected := map[string]int64{
		"count/pods":              1,
		"cpu":                     2,
		"memory":                  1024 * 1024 * 2000,
		"nvidia.com/gpu":          2,
		"hugepages-test":          4000,
		"limits.cpu":              2,
		"limits.memory":           1024 * 1024 * 2000,
		"requests.memory":         1024 * 1024 * 2000,
		"requests.nvidia.com/gpu": 2,
		"requests.hugepages-test": 4000,
		"pods":                    1,
		"requests.cpu":            2,
	}

	res := *GetPodQuotaUsage(pod)
	for name, quantity := range expected {
		value, ok := res[corev1.ResourceName(name)]
		if !ok {
			t.Errorf("Resource %s should exists in pod resources", name)
		} else if quantity != value.Value() {
			t.Errorf("Resource %s 's value %d should equal to %d", name, quantity, value.Value())
		}
	}
}
