/*
Copyright 2019 The Volcano Authors.

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
	"strings"
	"testing"
)

type fakeAction struct{}

func (fakeAction) Name() string     { return "FakeAction" }
func (fakeAction) Initialize()      {}
func (fakeAction) Execute(*Session) {}
func (fakeAction) UnInitialize()    {}

func TestGetActionCaseInsensitive(t *testing.T) {
	RegisterAction(fakeAction{})
	t.Cleanup(func() {
		pluginMutex.Lock()
		defer pluginMutex.Unlock()
		delete(actionMap, strings.ToLower(fakeAction{}.Name()))
	})

	cases := []string{"fakeaction", "fakeAction", "FakeAction", "FAKEACTION"}
	for _, name := range cases {
		if _, found := GetAction(name); !found {
			t.Errorf("expected to find action for lookup name %q", name)
		}
	}

	if _, found := GetAction("notregistered"); found {
		t.Errorf("expected not to find action for unregistered name")
	}
}

func TestGetPluginName(t *testing.T) {
	cases := []struct {
		pluginPath string
		pluginName string
	}{
		{
			pluginPath: "magic.so",
			pluginName: "magic",
		},
		{
			pluginPath: "./magic.so",
			pluginName: "magic",
		},
		{
			pluginPath: "./plugins/magic.so",
			pluginName: "magic",
		},
		{
			pluginPath: "/plugins/magic.so",
			pluginName: "magic",
		},
		{
			pluginPath: "a/b/c/plugins/magic.so",
			pluginName: "magic",
		},
	}

	for index, c := range cases {
		pluginName := getPluginName(c.pluginPath)
		if pluginName != c.pluginName {
			t.Errorf("index %d value should be %v, but not %v", index, c.pluginName, pluginName)
		}
	}
}
