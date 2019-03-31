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

// SchedulerConfiguration defines the configuration of scheduler.
type SchedulerConfiguration struct {
	// Actions defines the actions list of scheduler in order
	Actions string `yaml:"actions"`
	// Tiers defines plugins in different tiers
	Tiers []Tier `yaml:"tiers"`
}

// Tier defines plugin tier
type Tier struct {
	Plugins []PluginOption `yaml:"plugins"`
}

// PluginOption defines the options of plugin
type PluginOption struct {
	// The name of Plugin
	Name string `yaml:"name"`
	// JobOrderDisabled defines whether jobOrderFn is disabled
	JobOrderDisabled bool `yaml:"disableJobOrder"`
	// JobReadyDisabled defines whether jobReadyFn is disabled
	JobReadyDisabled bool `yaml:"disableJobReady"`
	// TaskOrderDisabled defines whether taskOrderFn is disabled
	TaskOrderDisabled bool `yaml:"disableTaskOrder"`
	// PreemptableDisabled defines whether preemptableFn is disabled
	PreemptableDisabled bool `yaml:"disablePreemptable"`
	// ReclaimableDisabled defines whether reclaimableFn is disabled
	ReclaimableDisabled bool `yaml:"disableReclaimable"`
	// QueueOrderDisabled defines whether queueOrderFn is disabled
	QueueOrderDisabled bool `yaml:"disableQueueOrder"`
	// PredicateDisabled defines whether predicateFn is disabled
	PredicateDisabled bool `yaml:"disablePredicate"`
	// NodeOrderDisabled defines whether NodeOrderFn is disabled
	NodeOrderDisabled bool `yaml:"disableNodeOrder"`
	// Arguments defines the different arguments that can be given to different plugins
	Arguments map[string]string `yaml:"arguments"`
}
