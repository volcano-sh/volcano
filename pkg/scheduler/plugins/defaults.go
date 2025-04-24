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

package plugins

import (
	"volcano.sh/volcano/pkg/scheduler/conf"
)

// ApplyPluginConfDefaults sets option's filed to its default value if not set
func ApplyPluginConfDefaults(option *conf.PluginOption) {
	setDefaultIfNil(&option.EnabledJobOrder)
	setDefaultIfNil(&option.EnabledJobReady)
	setDefaultIfNil(&option.EnabledJobPipelined)
	setDefaultIfNil(&option.EnabledJobEnqueued)
	setDefaultIfNil(&option.EnabledTaskOrder)
	setDefaultIfNil(&option.EnabledPreemptable)
	setDefaultIfNil(&option.EnabledReclaimable)
	setDefaultIfNil(&option.EnablePreemptive)
	setDefaultIfNil(&option.EnabledQueueOrder)
	setDefaultIfNil(&option.EnabledPredicate)
	setDefaultIfNil(&option.EnabledBestNode)
	setDefaultIfNil(&option.EnabledNodeOrder)
	setDefaultIfNil(&option.EnabledTargetJob)
	setDefaultIfNil(&option.EnabledReservedNodes)
	setDefaultIfNil(&option.EnabledVictim)
	setDefaultIfNil(&option.EnabledJobStarving)
	setDefaultIfNil(&option.EnabledOverused)
	setDefaultIfNil(&option.EnabledAllocatable)
}

func setDefaultIfNil(field **bool) {
	defaultValue := true
	if *field == nil {
		*field = &defaultValue
	}
}
