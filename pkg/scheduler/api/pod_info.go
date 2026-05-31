/*
Copyright 2019 The Kubernetes Authors.

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
	"encoding/json"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// Refer k8s.io/kubernetes/pkg/api/v1/resource/helpers.go#PodRequests.
//
// GetResourceRequest returns a *Resource that covers the largest width in each resource dimension.
// Because init-containers run sequentially, we collect the max in each dimension iteratively.
// In contrast, we sum the resource vectors for regular containers since they run simultaneously.
//
// To be consistent with kubernetes default scheduler, it is only used for predicates of actions(e.g.
// allocate, backfill, preempt, reclaim), please use GetPodResourceWithoutInitContainers for other cases.
//
// Example:
//
// Pod:
//   InitContainers
//     IC1:
//       CPU: 2
//       Memory: 1G
//     IC2:
//       CPU: 2
//       Memory: 3G
//   Containers
//     C1:
//       CPU: 2
//       Memory: 1G
//     C2:
//       CPU: 1
//       Memory: 1G
//
// Result: CPU: 3, Memory: 3G

// GetPodResourceRequest returns all the resource required for that pod
func GetPodResourceRequest(pod *v1.Pod) *Resource {
	// 获取 pod 的资源请求，不包含 init 容器
	result := GetPodResourceWithoutInitContainers(pod)

	restartableInitContainerReqs := EmptyResource()
	// init 容器的资源请求
	initContainerReqs := EmptyResource()
	for _, container := range pod.Spec.InitContainers {
		containerReq := NewResource(container.Resources.Requests)

		// init 容器根据重启策略分别处理

		// 普通 Init 容器：
		// 按顺序执行，取每个阶段的最大资源需求
		// 不会与普通容器同时运行

		// 可重启 Init 容器：
		// 可以与普通容器同时运行
		// 资源需求需要累加到总需求中

		if container.RestartPolicy != nil && *container.RestartPolicy == v1.ContainerRestartPolicyAlways {
			// 可重启的 init 容器走这个分支
			// 因为可重启的 init 容器可以与普通容器同时运行
			// 所以需要把资源需求累加到总需求中

			// Add the restartable container's req to the resulting cumulative container requests.
			result.Add(containerReq)

			// Track our cumulative restartable init container resources
			restartableInitContainerReqs.Add(containerReq)
			containerReq = restartableInitContainerReqs
		} else {
			// 不可重启的 init 容器走这个分支
			// 最终计算资源是所有 init 容器中最大的那个，因为 init 容器是按顺序执行的
			tmp := EmptyResource()
			tmp.Add(containerReq)
			tmp.Add(restartableInitContainerReqs)
			containerReq = tmp
		}
		// 取最大值
		// 比如 init 容器1 的资源需求是 100m，init 容器2 的资源需求是 200m
		// 那么最终计算资源是 200m，也就是这个 pod 只需要 200m 的资源就可以运行所有的容器了
		initContainerReqs.SetMaxResource(containerReq)
	}

	result.SetMaxResource(initContainerReqs)
	// 累加 pod 的资源需求
	// 每个节点可能对运行的 pod 数量有配额限制
	// 比如节点可以运行 100 个 pod，那么这个 pod 就需要占用 1 个 pod 资源
	result.AddScalar(v1.ResourcePods, 1)

	return result
}

// GetPodPreemptable return volcano.sh/preemptable value for pod
func GetPodPreemptable(pod *v1.Pod) bool {
	// check annotation first
	if len(pod.Annotations) > 0 {
		if value, found := pod.Annotations[v1beta1.PodPreemptable]; found {
			b, err := strconv.ParseBool(value)
			if err != nil {
				klog.Warningf("invalid %s=%s", v1beta1.PodPreemptable, value)
				return false
			}
			return b
		}
	}

	// it annotation does not exit, check label
	if len(pod.Labels) > 0 {
		if value, found := pod.Labels[v1beta1.PodPreemptable]; found {
			b, err := strconv.ParseBool(value)
			if err != nil {
				klog.Warningf("invalid %s=%s", v1beta1.PodPreemptable, value)
				return false
			}
			return b
		}
	}

	return true
}

// GetPodRevocableZone return volcano.sh/revocable-zone value for pod/podgroup
func GetPodRevocableZone(pod *v1.Pod) string {
	if len(pod.Annotations) > 0 {
		if value, found := pod.Annotations[v1beta1.RevocableZone]; found {
			if value != "*" {
				return ""
			}
			return value
		}

		if value, found := pod.Annotations[v1beta1.PodPreemptable]; found {
			if b, err := strconv.ParseBool(value); err == nil && b {
				return "*"
			}
		}
	}
	return ""
}

// GetPodTopologyInfo return volcano.sh/numa-topology-policy value for pod
func GetPodTopologyInfo(pod *v1.Pod) *TopologyInfo {
	info := TopologyInfo{
		ResMap: make(map[int]v1.ResourceList),
	}

	if len(pod.Annotations) > 0 {
		if value, found := pod.Annotations[v1beta1.NumaPolicyKey]; found {
			info.Policy = value
		}

		if value, found := pod.Annotations[topologyDecisionAnnotation]; found {
			decision := PodResourceDecision{}
			err := json.Unmarshal([]byte(value), &decision)
			if err == nil {
				info.ResMap = decision.NUMAResources
			}
		}
	}

	return &info
}

// GetPodResourceWithoutInitContainers returns Pod's resource request, it does not contain
// init containers' resource request.
func GetPodResourceWithoutInitContainers(pod *v1.Pod) *Resource {
	result := EmptyResource()
	// 累加所有普通容器的资源需求
	for _, container := range pod.Spec.Containers {
		result.Add(NewResource(container.Resources.Requests))
	}

	// if PodOverhead feature is supported, add overhead for running a pod
	// Pod Overhead 是 Kubernetes 在运行 Pod 时，除了容器本身声明的资源需求外，额外消耗的系统资源。这些开销来自于：
	// 容器运行时（如 containerd、Docker）
	// 网络插件（如 CNI 插件）
	// 存储插件（如 CSI 驱动）
	// 系统守护进程
	// Cgroup 管理开销
	//
	// 如果定义了，把这部分资源需求也累加进去
	if pod.Spec.Overhead != nil {
		result.Add(NewResource(pod.Spec.Overhead))
	}

	return result
}
