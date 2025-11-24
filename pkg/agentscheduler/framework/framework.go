/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2023 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced session initialization with configuration support

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

package framework

import (
	"time"

	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// StartSchedulingCycle start a scheduling cycle
func StartSchedulingCycle(cache cache.Cache, plugins map[string]Plugin) *ScheduleCycle {
	sc := startSchedulingCycle(cache, plugins)
	for _, plugin := range plugins {
		plugin.OnSchedulingStart(sc)
	}
	return sc
}

// EndchedulingCycle end the scheduling cycle
func EndSchedulingCycle(sc *ScheduleCycle) {
	for _, plugin := range sc.plugins {
		onSessionCloseStart := time.Now()
		plugin.OnSchedulingEnd(sc)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionClose, metrics.Duration(onSessionCloseStart))
	}

	// endSchedulingCycle(ssn)
}
