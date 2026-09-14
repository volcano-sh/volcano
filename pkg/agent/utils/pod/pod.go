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

package pod

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientset "k8s.io/client-go/kubernetes"
	helpers "k8s.io/component-helpers/resource"
	v1qos "k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	k8sfeature "k8s.io/kubernetes/pkg/features"

	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/utils"
)

// ActivePods returns pods bound to the kubelet that are active (i.e. non-terminal state)
type ActivePods func() ([]*v1.Pod, error)

type KillPod func(ctx context.Context, client clientset.Interface, gracePeriodSeconds *int64, pod *v1.Pod, evictionVersion string) error

type FilterPodsFunc func(*v1.Pod, v1.ResourceName) bool

type filterPodsFunc func(bool) FilterPodsFunc

var IncludeOversoldPodFn filterPodsFunc = func(b bool) FilterPodsFunc {
	f := func(pod *v1.Pod, resType v1.ResourceName) bool {
		return !b && IsPreemptablePod(pod)
	}
	return f
}

var IncludeGuaranteedPodsFn filterPodsFunc = func(b bool) FilterPodsFunc {
	f := func(pod *v1.Pod, resType v1.ResourceName) bool {
		return !b && resType == v1.ResourceCPU && IsGuaranteedAndNonPreemptablePods(pod)
	}
	return f
}

// SortedPodsByRequestCPU sort pods by cpu request value.
type SortedPodsByRequestCPU []*v1.Pod

func (s SortedPodsByRequestCPU) Len() int      { return len(s) }
func (s SortedPodsByRequestCPU) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s SortedPodsByRequestCPU) Less(i, j int) bool {
	left := getEffectivePodRequestByResource(s[i], v1.ResourceCPU)
	right := getEffectivePodRequestByResource(s[j], v1.ResourceCPU)
	return left.Cmp(right) > 0
}

// SortedPodsByRequestMemory sort pods by memory request value.
type SortedPodsByRequestMemory []*v1.Pod

func (s SortedPodsByRequestMemory) Len() int      { return len(s) }
func (s SortedPodsByRequestMemory) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s SortedPodsByRequestMemory) Less(i, j int) bool {
	left := getEffectivePodRequestByResource(s[i], v1.ResourceMemory)
	right := getEffectivePodRequestByResource(s[j], v1.ResourceMemory)
	return left.Cmp(right) > 0
}

// GetTotalRequest return the total resource of pods
func GetTotalRequest(pods []*v1.Pod, fns []FilterPodsFunc, resTypes []v1.ResourceName) v1.ResourceList {
	total := make(v1.ResourceList)
	for _, resType := range resTypes {
		total[resType] = getTotalRequestByType(pods, fns, resType)
	}
	return total
}

// IsGuaranteedAndNonPreemptablePods return whether pod is guaranteed, but preemptable guaranteed pods are excluded.
func IsGuaranteedAndNonPreemptablePods(pod *v1.Pod) bool {
	return v1qos.GetPodQOS(pod) == v1.PodQOSGuaranteed && !IsPreemptablePod(pod)
}

// IsPreemptablePod check if a pod an offline pod.
func IsPreemptablePod(pod *v1.Pod) bool {
	return extension.GetQosLevel(pod) < 0
}

func IncludeGuaranteedPods() bool {
	policy := utils.GetCPUManagerPolicy()
	return policy == "" || policy == "none"
}

func GuaranteedPodsCPURequest(pods []*v1.Pod) int64 {
	if IncludeGuaranteedPods() {
		return 0
	}
	guaranteedPods := []*v1.Pod{}
	for idx := range pods {
		if IsGuaranteedAndNonPreemptablePods(pods[idx]) {
			guaranteedPods = append(guaranteedPods, pods[idx])
		}
	}

	fns := []FilterPodsFunc{IncludeOversoldPodFn(true), IncludeGuaranteedPodsFn(true)}
	quantity := GetTotalRequest(guaranteedPods, fns, []v1.ResourceName{v1.ResourceCPU})[v1.ResourceCPU]
	return quantity.MilliValue()
}

// FilterOutPreemptablePods return pods those are not preemptable
func FilterOutPreemptablePods(pods []*v1.Pod) ([]*v1.Pod, []*v1.Pod) {
	normalPods := make([]*v1.Pod, 0)
	preemptablePods := make([]*v1.Pod, 0)
	for _, pod := range pods {
		if IsPreemptablePod(pod) {
			preemptablePods = append(preemptablePods, pod)
		} else {
			normalPods = append(normalPods, pod)
		}
	}
	return normalPods, preemptablePods
}

// CPU resource will be ignored for guaranteed and non preemptable pods if cpu manager policy not none.
func getTotalRequestByType(pods []*v1.Pod, fns []FilterPodsFunc, resType v1.ResourceName) resource.Quantity {
	totalRes := resource.MustParse("0")
	for _, pod := range pods {
		skip := false
		for _, fn := range fns {
			if fn(pod, resType) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		totalRes.Add(getEffectivePodRequestByResource(pod, resType))
	}

	return totalRes
}

// getEffectivePodRequestByResource returns the Kubernetes effective request for a Pod resource.
func getEffectivePodRequestByResource(pod *v1.Pod, resType v1.ResourceName) resource.Quantity {
	requests := helpers.PodRequests(pod, helpers.PodResourcesOptions{
		SkipPodLevelResources: !utilfeature.DefaultFeatureGate.Enabled(k8sfeature.PodLevelResources),
	})
	return requests[resType]
}

// IsPodTerminated return true if pod is terminated.
func IsPodTerminated(pod *v1.Pod) bool {
	return pod.DeletionTimestamp != nil || pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}
