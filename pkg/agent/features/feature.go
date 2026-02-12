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

package features

type Feature string

const (
	// CPUQoSFeature is the feature gate for CPU Quality of Service.
	// It only works on OpenEuler OS.
	CPUQoSFeature Feature = "CPUQoS"

	// CPUBurstFeature is the feature gate for CPU Burst.
	// It requires Linux kernel 5.14+.
	CPUBurstFeature Feature = "CPUBurst"

	// CPUThrottleFeature is the feature gate for CPU throttling.
	CPUThrottleFeature Feature = "CPUThrottle"

	// MemoryQoSFeature is the feature gate for Memory Quality of Service.
	// It only works on OpenEuler OS.
	MemoryQoSFeature Feature = "MemoryQoS"

	// MemoryQoSV2Feature is the feature gate for Memory Quality of Service for common Linux OS.
	// It requires Linux kernel with cgroup v2 enabled.
	MemoryQoSV2Feature Feature = "MemoryQoSV2"

	// NetworkQoSFeature is the feature gate for Network Quality of Service.
	// It only works on OpenEuler OS.
	NetworkQoSFeature Feature = "NetworkQoS"

	// OverSubscriptionFeature is the feature gate for resource oversubscription.
	OverSubscriptionFeature Feature = "OverSubscription"

	// EvictionFeature is the feature gate for pod eviction.
	EvictionFeature Feature = "Eviction"

	// ResourcesFeature is the feature gate for extend resource management.
	ResourcesFeature Feature = "Resources"
)
