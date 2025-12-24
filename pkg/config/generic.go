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

package config

import (
	clientset "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

type VolcanoAgentConfiguration struct {
	// HealthzAddress is the health check server address
	HealthzAddress string

	//HealthzPort is the health check server port
	HealthzPort int

	// KubeCgroupRoot is the root cgroup to use for pods.
	// same with kubelet configuration "cgroup-root"
	KubeCgroupRoot string

	// KubeClient is the client to visit k8s
	KubeClient clientset.Interface

	// KubeNodeName is the name of the node which pod is running.
	KubeNodeName string

	// KubePodName is the name of the pod.
	KubePodName string

	// KubePodName is the namespace of the pod.
	KubePodNamespace string

	// NodeLister list current node.
	NodeLister v1.NodeLister

	// PodLister list pods belong to current node.
	PodLister v1.PodLister

	// NodeHasSynced indicates whether nodes have been sync'd at least once.
	// Check this before trusting a response from the node lister.
	NodeHasSynced cache.InformerSynced

	// PodsHasSynced indicates whether pods have been sync'd at least once.
	// Check this before trusting a response from the pod lister.
	PodsHasSynced cache.InformerSynced

	// EventRecorder is the event sink
	Recorder record.EventRecorder

	// List of supported features, '*' supports all on-by-default features.
	SupportedFeatures []string

	// OverSubscriptionPolicy defines overSubscription policy.
	OverSubscriptionPolicy string

	// OverSubscriptionRatio is the over subscription ratio of idle resources, default to 60, which means 60%.
	OverSubscriptionRatio int

	// IncludeSystemUsage determines whether considering system usage when calculate overSubscription resource and evict.
	IncludeSystemUsage bool

	// ExtendResourceCPUName is the extend resource cpu, which is used to calculate overSubscription resources.
	ExtendResourceCPUName string

	// ExtendResourceMemoryName is the extend resource memory, which is used to calculate overSubscription resources.
	ExtendResourceMemoryName string
}
