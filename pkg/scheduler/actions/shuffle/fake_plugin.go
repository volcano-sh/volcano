/*
Copyright 2022 The Volcano Authors.

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

package shuffle

import (
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// FakePlugin indicates name of volcano scheduler plugin.
const FakePlugin = "fake"

type fakePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// NewFakePlugin return fake plugin
func NewFakePlugin(arguments framework.Arguments) framework.Plugin {
	return &fakePlugin{pluginArguments: arguments}
}

func (fp *fakePlugin) Name() string {
	return FakePlugin
}

func (fp *fakePlugin) OnSessionOpen(ssn *framework.Session) {
	lowPriority := 10

	victimTasksFn := func(candidates []*api.TaskInfo) []*api.TaskInfo {
		evicts := make([]*api.TaskInfo, 0)
		for _, task := range candidates {
			if task.Priority == int32(lowPriority) {
				evicts = append(evicts, task)
			}
		}
		return evicts
	}

	victimTasksFns := make([]api.VictimTasksFn, 0)
	victimTasksFns = append(victimTasksFns, victimTasksFn)
	ssn.AddVictimTasksFns(fp.Name(), victimTasksFns)
}

func (fp *fakePlugin) OnSessionClose(*framework.Session) {}
