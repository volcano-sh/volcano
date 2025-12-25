/*
Copyright 2025 The Volcano Authors.

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

package v1alpha1

// NetworkTopologyModeAnnotationKey is the annotation key for specifying the network topology mode.
// Value should be a string representing the desired mode (e.g., "hard", "soft").
const NetworkTopologyModeAnnotationKey = "volcano.sh/network-topology-mode"

// NetworkTopologyHighestTierAnnotationKey is the annotation key for network topology highest tier
// Value should be an integer representing the highest tier allowed
const NetworkTopologyHighestTierAnnotationKey = "volcano.sh/network-topology-highest-tier"
