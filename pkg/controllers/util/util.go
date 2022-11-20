package util

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	quotacore "k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
)

func GetPodQuotaUsage(pod *v1.Pod) *v1.ResourceList {
	res, _ := quotacore.PodUsageFunc(pod, clock.RealClock{})
	for name, quantity := range res {
		if !helper.IsNativeResource(name) && strings.HasPrefix(string(name), v1.DefaultResourceRequestsPrefix) {
			res[v1.ResourceName(strings.TrimPrefix(string(name), v1.DefaultResourceRequestsPrefix))] = quantity
		}
	}
	return &res
}
