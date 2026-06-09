/*
Copyright 2026 The Volcano Authors.

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

package svc

import (
	"testing"

	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestNetworkPolicyStrategyFlag(t *testing.T) {
	tests := []struct {
		name                     string
		params                   []string
		networkPolicyStrategy    string
		disableNetworkPolicy     bool
		publishNotReadyAddresses bool
	}{
		{
			name:                  "no params specified",
			networkPolicyStrategy: "",
		},
		{
			name:                  "network-policy-strategy allow",
			params:                []string{"--network-policy-strategy=allow"},
			networkPolicyStrategy: "allow",
		},
		{
			name:                  "network-policy-strategy allow-related",
			params:                []string{"--network-policy-strategy=allow-related"},
			networkPolicyStrategy: "allow-related",
		},
		{
			name:                     "disable network policy",
			params:                   []string{"--disable-network-policy=true", "--publish-not-ready-addresses=true"},
			disableNetworkPolicy:     true,
			publishNotReadyAddresses: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pluginInterface := New(pluginsinterface.PluginClientset{}, test.params)
			plugin := pluginInterface.(*servicePlugin)

			if plugin.networkPolicyStrategy != test.networkPolicyStrategy {
				t.Errorf("Expected networkPolicyStrategy=%q, got %q", test.networkPolicyStrategy, plugin.networkPolicyStrategy)
			}
			if plugin.disableNetworkPolicy != test.disableNetworkPolicy {
				t.Errorf("Expected disableNetworkPolicy=%v, got %v", test.disableNetworkPolicy, plugin.disableNetworkPolicy)
			}
			if plugin.publishNotReadyAddresses != test.publishNotReadyAddresses {
				t.Errorf("Expected publishNotReadyAddresses=%v, got %v", test.publishNotReadyAddresses, plugin.publishNotReadyAddresses)
			}
		})
	}
}
