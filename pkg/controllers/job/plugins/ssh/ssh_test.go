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

package ssh

import (
	"testing"

	"volcano.sh/volcano/pkg/controllers/job/plugins/env"
	"volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestSSHPlugin(t *testing.T) {

	tests := []struct {
		name           string
		params         []string
		noRoot         bool
		sshKeyFilePath string
	}{
		{
			name:           "no params specified",
			noRoot:         false,
			sshKeyFilePath: SSHAbsolutePath,
		},
		{
			name:           "--no-root=true, ssh-key-file-path empty",
			params:         []string{"--no-root=true"},
			noRoot:         true,
			sshKeyFilePath: env.ConfigMapMountPath + "/" + SSHRelativePath,
		},
		{
			name:           "--no-root=true, --ssh-key-file-path=/a/b",
			params:         []string{"--no-root=true", "--ssh-key-file-path=/a/b"},
			noRoot:         true,
			sshKeyFilePath: "/a/b",
		},
		{
			name:           "--ssh-key-file-path=/a/b",
			params:         []string{"--ssh-key-file-path=/a/b"},
			noRoot:         false,
			sshKeyFilePath: "/a/b",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pluginInterface := New(pluginsinterface.PluginClientset{}, test.params)
			plugin := pluginInterface.(*sshPlugin)
			if plugin.noRoot != test.noRoot {
				t.Errorf("Expected noRoot=%v, got %v", test.noRoot, plugin.noRoot)
			}

			if plugin.sshKeyFilePath != test.sshKeyFilePath {
				t.Errorf("Expected sshKeyFilePath=%s, got %s", test.sshKeyFilePath, plugin.sshKeyFilePath)
			}
		})
	}
}
