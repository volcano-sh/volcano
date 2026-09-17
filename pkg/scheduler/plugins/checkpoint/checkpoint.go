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

package checkpoint

import (
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "checkpoint"

// DefaultCheckpointTimeKey is the pod annotation key the workload updates after
// every successful checkpoint. The value must be an RFC3339 timestamp (UTC).
//
// Annotations, not labels, are used deliberately: the value changes on every
// checkpoint and is never used for object selection. Labels are indexed by the
// API server for querying, so mutating them frequently adds unnecessary etcd
// write overhead; annotations are the correct carrier for a frequently-updated,
// non-selectable value.
const DefaultCheckpointTimeKey = "volcano.sh/last-checkpoint-time"

// CheckpointTimeKeyArg is the plugin argument to override the annotation key.
const CheckpointTimeKeyArg = "checkpointTimeKey"

type checkpointPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments

	checkpointTimeKey string
}

// New returns a checkpoint plugin.
func New(arguments framework.Arguments) framework.Plugin {
	key := DefaultCheckpointTimeKey
	if raw, ok := arguments[CheckpointTimeKeyArg]; ok {
		if s, ok := raw.(string); ok && s != "" {
			key = s
		}
	}
	return &checkpointPlugin{pluginArguments: arguments, checkpointTimeKey: key}
}

func (cp *checkpointPlugin) Name() string {
	return PluginName
}

// checkpointTime returns the last-checkpoint time parsed from the task's pod
// annotation. Tasks that have never checkpointed (missing or unparseable
// annotation) get the zero time, which sorts as the oldest possible checkpoint
// so they are evicted last (they have the most progress to lose).
func (cp *checkpointPlugin) checkpointTime(task *api.TaskInfo) time.Time {
	if task == nil || task.Pod == nil {
		return time.Time{}
	}
	raw, ok := task.Pod.Annotations[cp.checkpointTimeKey]
	if !ok || raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		klog.V(4).Infof("Checkpoint VictimOrder: task <%s/%s> has invalid %s=%q: %v; treating as never checkpointed",
			task.Namespace, task.Name, cp.checkpointTimeKey, raw, err)
		return time.Time{}
	}
	return t
}

func (cp *checkpointPlugin) OnSessionOpen(ssn *framework.Session) {
	// victimOrderFn orders victim tasks by checkpoint recency. The returned int
	// follows the keep-order convention shared by task/victim order plugins:
	// a negative value means l should be preserved (evicted later).
	//
	// A more recent checkpoint means less work is lost on eviction, so such a
	// task should be evicted first (kept last) -> return positive. Tasks with an
	// older (or zero/never) checkpoint are preserved -> return negative. Equal
	// checkpoint times defer to the next victim-order key.
	victimOrderFn := func(l, r interface{}) int {
		lv := l.(*api.TaskInfo)
		rv := r.(*api.TaskInfo)

		lt := cp.checkpointTime(lv)
		rt := cp.checkpointTime(rv)

		klog.V(4).Infof("Checkpoint VictimOrder: <%s/%s> checkpoint %v, <%s/%s> checkpoint %v",
			lv.Namespace, lv.Name, lt, rv.Namespace, rv.Name, rt)

		if lt.Equal(rt) {
			return 0
		}
		if lt.After(rt) {
			return 1
		}
		return -1
	}

	ssn.AddVictimOrderFn(cp.Name(), victimOrderFn)
}

func (cp *checkpointPlugin) OnSessionClose(ssn *framework.Session) {}
