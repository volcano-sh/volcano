/*
Copyright 2018 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added extensive plugin configuration options
- Enhanced configuration with action-specific arguments and metrics support

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

package conf

// EnabledActionMap check if a action exist in scheduler configmap. If not exist the value is false.
var EnabledActionMap map[string]bool

// SchedulerConfiguration defines the configuration of scheduler.
type SchedulerConfiguration struct {
	// Actions defines the actions list of scheduler in order
	Actions string `yaml:"actions" json:"actions,omitempty"`
	// Tiers defines plugins in different tiers
	Tiers []Tier `yaml:"tiers" json:"tiers,omitempty"`
	// Configurations is configuration for actions
	Configurations       []Configuration   `yaml:"configurations" json:"configurations,omitempty"`
	MetricsConfiguration map[string]string `yaml:"metrics" json:"metrics,omitempty"`
}

// Tier defines plugin tier
type Tier struct {
	Plugins []PluginOption `yaml:"plugins" json:"plugins,omitempty"`
}

// Configuration is configuration of action
type Configuration struct {
	// Name is name of action
	Name string `yaml:"name" json:"name,omitempty"`
	// Arguments defines the different arguments that can be given to specified action
	Arguments map[string]interface{} `yaml:"arguments" json:"arguments,omitempty"`
}

// PluginOption defines the options of plugin
type PluginOption struct {
	// The name of Plugin
	Name string `yaml:"name" json:"name,omitempty"`
	// EnabledJobOrder defines whether jobOrderFn is enabled
	EnabledJobOrder *bool `yaml:"enableJobOrder" json:"enableJobOrder,omitempty"`
	// EnabledHierarchy defines whether hierarchical sharing is enabled
	EnabledHierarchy *bool `yaml:"enableHierarchy" json:"enableHierarchy,omitempty"`
	// EnabledJobReady defines whether jobReadyFn is enabled
	EnabledJobReady *bool `yaml:"enableJobReady" json:"enableJobReady,omitempty"`
	// EnabledJobPipelined defines whether jobPipelinedFn is enabled
	EnabledJobPipelined *bool `yaml:"enableJobPipelined" json:"enableJobPipelined,omitempty"`
	// EnabledTaskOrder defines whether taskOrderFn is enabled
	EnabledTaskOrder *bool `yaml:"enableTaskOrder" json:"enableTaskOrder,omitempty"`
	// EnabledPreemptable defines whether preemptableFn is enabled
	EnabledPreemptable *bool `yaml:"enablePreemptable" json:"enablePreemptable,omitempty"`
	// EnabledReclaimable defines whether reclaimableFn is enabled
	EnabledReclaimable *bool `yaml:"enableReclaimable" json:"enableReclaimable,omitempty"`
	// EnablePreemptive defines whether preemptiveFn is enabled
	EnablePreemptive *bool `yaml:"enablePreemptive" json:"enablePreemptive,omitempty"`
	// EnabledQueueOrder defines whether queueOrderFn is enabled
	EnabledQueueOrder *bool `yaml:"enableQueueOrder" json:"enableQueueOrder,omitempty"`
	// EnableClusterOrder defines whether clusterOrderFn is enabled
	EnabledClusterOrder *bool `yaml:"enabledClusterOrder" json:"enabledClusterOrder,omitempty"`
	// EnabledPredicate defines whether predicateFn is enabled
	EnabledPredicate *bool `yaml:"enablePredicate" json:"enablePredicate,omitempty"`
	// EnabledBestNode defines whether bestNodeFn is enabled
	EnabledBestNode *bool `yaml:"enableBestNode" json:"enableBestNode,omitempty"`
	// EnabledNodeOrder defines whether NodeOrderFn is enabled
	EnabledNodeOrder *bool `yaml:"enableNodeOrder" json:"enableNodeOrder,omitempty"`
	// EnabledTargetJob defines whether targetJobFn is enabled
	EnabledTargetJob *bool `yaml:"enableTargetJob" json:"enableTargetJob,omitempty"`
	// EnabledReservedNodes defines whether reservedNodesFn is enabled
	EnabledReservedNodes *bool `yaml:"enableReservedNodes" json:"enableReservedNodes,omitempty"`
	// EnabledJobEnqueued defines whether jobEnqueuedFn is enabled
	EnabledJobEnqueued *bool `yaml:"enableJobEnqueued" json:"enableJobEnqueued,omitempty"`
	// EnabledVictim defines whether victimsFn is enabled
	EnabledVictim *bool `yaml:"enabledVictim" json:"enabledVictim,omitempty"`
	// EnabledJobStarving defines whether jobStarvingFn is enabled
	EnabledJobStarving *bool `yaml:"enableJobStarving" json:"enableJobStarving,omitempty"`
	// EnabledOverused defines whether overusedFn is enabled
	EnabledOverused *bool `yaml:"enabledOverused" json:"enabledOverused,omitempty"`
	// EnabledAllocatable defines whether allocatable is enabled
	EnabledAllocatable *bool `yaml:"enabledAllocatable" json:"enabledAllocatable,omitempty"`
	// EnabledHyperNodeOrder defines whether hyperNode is enabled
	EnabledHyperNodeOrder *bool `yaml:"enabledHyperNodeOrder" json:"enabledHyperNodeOrder,omitempty"`
	// EnabledSubJobReady defines whether subJobReadyFn is enabled
	EnabledSubJobReady *bool `yaml:"enabledSubJobReady" json:"enabledSubJobReady,omitempty"`
	// EnabledSubJobPipelined defines whether subJobPipelinedFn is enabled
	EnabledSubJobPipelined *bool `yaml:"enabledSubJobPipelined" json:"enabledSubJobPipelined,omitempty"`
	// EnabledSubJobOrder defines whether subJobOrderFn is enabled
	EnabledSubJobOrder *bool `yaml:"enabledSubJobOrder" json:"enabledSubJobOrder,omitempty"`
	// EnabledHyperNodeGradient defines whether hyperNodeGradientFn is enabled
	EnabledHyperNodeGradient *bool `yaml:"enabledHyperNodeGradient" json:"enabledHyperNodeGradient,omitempty"`
	// Arguments defines the different arguments that can be given to different plugins
	Arguments map[string]interface{} `yaml:"arguments" json:"arguments,omitempty"`
}
