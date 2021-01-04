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

import "testing"

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
