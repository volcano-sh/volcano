/*
Copyright 2018 The Kubernetes Authors.

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
	Actions string `yaml:"actions"`
	// Tiers defines plugins in different tiers
	Tiers []Tier `yaml:"tiers"`
	// Configurations is configuration for actions
	Configurations       []Configuration   `yaml:"configurations"`
	MetricsConfiguration map[string]string `yaml:"metrics"`
}

// Tier defines plugin tier
type Tier struct {
	Plugins []PluginOption `yaml:"plugins"`
}

// Configuration is configuration of action
type Configuration struct {
	// Name is name of action
	Name string `yaml:"name"`
	// Arguments defines the different arguments that can be given to specified action
	Arguments map[string]interface{} `yaml:"arguments"`
}

// PluginOption defines the options of plugin
type PluginOption struct {
	// The name of Plugin
	Name string `yaml:"name"`
	// EnabledJobOrder defines whether jobOrderFn is enabled
	EnabledJobOrder *bool `yaml:"enableJobOrder"`
	// EnabledHierarchy defines whether hierarchical sharing is enabled
	EnabledHierarchy *bool `yaml:"enableHierarchy"`
	// EnabledJobReady defines whether jobReadyFn is enabled
	EnabledJobReady *bool `yaml:"enableJobReady"`
	// EnabledJobPipelined defines whether jobPipelinedFn is enabled
	EnabledJobPipelined *bool `yaml:"enableJobPipelined"`
	// EnabledTaskOrder defines whether taskOrderFn is enabled
	EnabledTaskOrder *bool `yaml:"enableTaskOrder"`
	// EnabledPreemptable defines whether preemptableFn is enabled
	EnabledPreemptable *bool `yaml:"enablePreemptable"`
	// EnabledReclaimable defines whether reclaimableFn is enabled
	EnabledReclaimable *bool `yaml:"enableReclaimable"`
	// EnablePreemptive defines whether preemptiveFn is enabled
	EnablePreemptive *bool `yaml:"enablePreemptive"`
	// EnabledQueueOrder defines whether queueOrderFn is enabled
	EnabledQueueOrder *bool `yaml:"enableQueueOrder"`
	// EnableClusterOrder defines whether clusterOrderFn is enabled
	EnabledClusterOrder *bool `yaml:"EnabledClusterOrder"`
	// EnabledPredicate defines whether predicateFn is enabled
	EnabledPredicate *bool `yaml:"enablePredicate"`
	// EnabledBestNode defines whether bestNodeFn is enabled
	EnabledBestNode *bool `yaml:"enableBestNode"`
	// EnabledNodeOrder defines whether NodeOrderFn is enabled
	EnabledNodeOrder *bool `yaml:"enableNodeOrder"`
	// EnabledTargetJob defines whether targetJobFn is enabled
	EnabledTargetJob *bool `yaml:"enableTargetJob"`
	// EnabledReservedNodes defines whether reservedNodesFn is enabled
	EnabledReservedNodes *bool `yaml:"enableReservedNodes"`
	// EnabledJobEnqueued defines whether jobEnqueuedFn is enabled
	EnabledJobEnqueued *bool `yaml:"enableJobEnqueued"`
	// EnabledVictim defines whether victimsFn is enabled
	EnabledVictim *bool `yaml:"enabledVictim"`
	// EnabledJobStarving defines whether jobStarvingFn is enabled
	EnabledJobStarving *bool `yaml:"enableJobStarving"`
	// EnabledOverused defines whether overusedFn is enabled
	EnabledOverused *bool `yaml:"enabledOverused"`
	// EnabledAllocatable defines whether allocatable is enabled
	EnabledAllocatable *bool `yaml:"enabledAllocatable"`
	// EnabledHyperNodeOrder defines whether hyperNode is enabled
	EnabledHyperNodeOrder *bool `yaml:"enabledHyperNodeOrder"`
	// Arguments defines the different arguments that can be given to different plugins
	Arguments map[string]interface{} `yaml:"arguments"`
}
