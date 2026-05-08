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

	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestSSHPlugin(t *testing.T) {

	tests := []struct {
		name           string
		params         []string
		noRoot         bool
		sshKeyFilePath string
		sshPrivateKey  string
		sshPublicKey   string
	}{
		{
			name:           "no params specified",
			sshKeyFilePath: SSHAbsolutePath,
			sshPrivateKey:  "",
			sshPublicKey:   "",
		},
		{
			name:           "--ssh-key-file-path=/a/b",
			params:         []string{"--ssh-key-file-path=/a/b"},
			sshKeyFilePath: "/a/b",
			sshPrivateKey:  "",
			sshPublicKey:   "",
		},
		{
			name:           "with user provided ssh keys",
			params:         []string{"--ssh-private-key=-----BEGIN RSA PRIVATE KEY-----\\nMIIEpAIBAAKCAQEAyeyZjWDx5Na9bw1f61M4s+QlLT/kyrB37AR2j5Sb/A9hvJak\\nLNQQpNC+KVfYNl4jePG+6lwHqye//pcC9+0SWsHWwgaahjMLnAthR2k8JAakNA9x\\nV/wHz0YU99OKEetaOuxXpWZPXCHX0zuQO87YbdKzRbgxACirM3Phkwr7XLtQtWZk\\nyXG34CQXZQWgBIS1Fl+PlGOpVpOPnWoZPMpbAK74i/Tz4sP8Zhqc6dya1hrbUwY3\\nYfMZNYXpaAw7wWVjq8grfs0+Fl3SxHrzTXge2m+eZAZ6iPJ8cX4uYKxi0ZmxpM/a\\ngI6Mmjq0MU75Vxpq22LaUvHIpOfX5UxhkrsxlwIDAQABAoIBAQDGOuIb6zpNn4rl\\nBMpPqamW4LimjX08hrWUHGWQWyIu96LJk1GlOKMGSm8FA1odNZm5WApG5QYaPrG7\\na+DcJ/7G3ljIrdbxPBd/n6RmiKcj7ukwuqBY8fFwyKo5CZEYOmagRfldRO1P02Gf\\n22+jZ1MNrbWVElf4gfRgVLj0s+lEhFkzhi+QGMmMpjEJnnG98xxVGEvWMw1rnKJm\\n3Gi771Gltbg3GuEPs3IeoBgba3EaHmSxJnBivAL4zsO8UUCAXB13cUiXx8qO7y1e\\nCSWSenRmK2ugbL6v0co12O0n0pxF9xlJ6fALdRWzpJsFlN3ttkY9N5GrQc/pVjOa\\nvqa172RRAoGBAOSAIMNLT6QjgYDk5Z7ZxjNnxH/lMso+cx6bxk9YMKRrw0fDQh8m\\ncBAihXhuntCPDGhrzQ+Anqx4jJVDFqac0xBck90a8LmmzD0q72eDTCYPouDWe6DL\\nJQAc/HDmIC13sADEXmGW3c0Qn4hjBnMd89ouYj7ZajU2sED2irPPc/HLAoGBAOI5\\nruL4Q0FarGrP3a9z9EDrVJsK2OfSTaJ7rhZ+uvB838svbHU+4mEYPhx4PCwvrYyi\\nFn4hyau003ZmLc1qTABjmwcO/PPiYyoRHJDUIIhiIyIL+id/G53uG2eTzqYtU6uS\\nnAIB2rKwwhU8ek+zbJBLu5uxuxlf4mdZITdkwtXlAoGBALH3RQ02A9JgQQYFwP2G\\nucLhx/6goX05RGoLg1na4w+8Sr0Cy+X9BvzaFkAlUBY5w700cOLpFyxXO48pUGP1\\n8sFkiVmFGQZPbfUaEpn5ff6K4R3ijyk97xR2fvrjkR44gOEoECZL3XZQwx/zmFti\\nccF1rNksdnb5oC8IliDTq4cfAoGANyy6asECJj5nLuXju5ccS3kZ+XZ70I6KQMbJ\\nftMJ5P2P146JdU8RB31SKL9qbZxzR4mA0uKKvUYtDQN+yErUnoOsm9wb9Z+RcAEc\\nZnZWOO02hGdHa7qkkbAxHuH91KnZbk8jnZm2LT7PFz7Y1fd80vSlnSOL7nRkU7B5\\nWXlJy8ECgYA4g0wc0Jq8c1Q0FulMkOQqYRDXaDo34987L+mZ70i/RtdkKjK/IKJ9\\n18UDCyEaDPD0BWBJGPejZkY8UD6FBG/5k7wNIbT7hHLRSRlw4iRmVX2hRVXrXzD8\\nvc86Qyg2iG0JqkMAvRdH40amPKp5bW4VcfcvQo4TSsI972u12rgwtg==\\n-----END RSA PRIVATE KEY-----\\n", "--ssh-public-key=ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDJ7JmNYPHk1r1vDV/rUziz5CUtP+TKsHfsBHaPlJv8D2G8lqQs1BCk0L4pV9g2XiN48b7qXAerJ7/+lwL37RJawdbCBpqGMwucC2FHaTwkBqQ0D3FX/AfPRhT304oR61o67FelZk9cIdfTO5A7ztht0rNFuDEAKKszc+GTCvtcu1C1ZmTJcbfgJBdlBaAEhLUWX4+UY6lWk4+dahk8ylsArviL9PPiw/xmGpzp3JrWGttTBjdh8xk1heloDDvBZWOryCt+zT4WXdLEevNNeB7ab55kBnqI8nxxfi5grGLRmbGkz9qAjoyaOrQxTvlXGmrbYtpS8cik59flTGGSuzGX root@aiplatform"},
			sshKeyFilePath: SSHAbsolutePath,
			sshPrivateKey:  "-----BEGIN RSA PRIVATE KEY-----\\nMIIEpAIBAAKCAQEAyeyZjWDx5Na9bw1f61M4s+QlLT/kyrB37AR2j5Sb/A9hvJak\\nLNQQpNC+KVfYNl4jePG+6lwHqye//pcC9+0SWsHWwgaahjMLnAthR2k8JAakNA9x\\nV/wHz0YU99OKEetaOuxXpWZPXCHX0zuQO87YbdKzRbgxACirM3Phkwr7XLtQtWZk\\nyXG34CQXZQWgBIS1Fl+PlGOpVpOPnWoZPMpbAK74i/Tz4sP8Zhqc6dya1hrbUwY3\\nYfMZNYXpaAw7wWVjq8grfs0+Fl3SxHrzTXge2m+eZAZ6iPJ8cX4uYKxi0ZmxpM/a\\ngI6Mmjq0MU75Vxpq22LaUvHIpOfX5UxhkrsxlwIDAQABAoIBAQDGOuIb6zpNn4rl\\nBMpPqamW4LimjX08hrWUHGWQWyIu96LJk1GlOKMGSm8FA1odNZm5WApG5QYaPrG7\\na+DcJ/7G3ljIrdbxPBd/n6RmiKcj7ukwuqBY8fFwyKo5CZEYOmagRfldRO1P02Gf\\n22+jZ1MNrbWVElf4gfRgVLj0s+lEhFkzhi+QGMmMpjEJnnG98xxVGEvWMw1rnKJm\\n3Gi771Gltbg3GuEPs3IeoBgba3EaHmSxJnBivAL4zsO8UUCAXB13cUiXx8qO7y1e\\nCSWSenRmK2ugbL6v0co12O0n0pxF9xlJ6fALdRWzpJsFlN3ttkY9N5GrQc/pVjOa\\nvqa172RRAoGBAOSAIMNLT6QjgYDk5Z7ZxjNnxH/lMso+cx6bxk9YMKRrw0fDQh8m\\ncBAihXhuntCPDGhrzQ+Anqx4jJVDFqac0xBck90a8LmmzD0q72eDTCYPouDWe6DL\\nJQAc/HDmIC13sADEXmGW3c0Qn4hjBnMd89ouYj7ZajU2sED2irPPc/HLAoGBAOI5\\nruL4Q0FarGrP3a9z9EDrVJsK2OfSTaJ7rhZ+uvB838svbHU+4mEYPhx4PCwvrYyi\\nFn4hyau003ZmLc1qTABjmwcO/PPiYyoRHJDUIIhiIyIL+id/G53uG2eTzqYtU6uS\\nnAIB2rKwwhU8ek+zbJBLu5uxuxlf4mdZITdkwtXlAoGBALH3RQ02A9JgQQYFwP2G\\nucLhx/6goX05RGoLg1na4w+8Sr0Cy+X9BvzaFkAlUBY5w700cOLpFyxXO48pUGP1\\n8sFkiVmFGQZPbfUaEpn5ff6K4R3ijyk97xR2fvrjkR44gOEoECZL3XZQwx/zmFti\\nccF1rNksdnb5oC8IliDTq4cfAoGANyy6asECJj5nLuXju5ccS3kZ+XZ70I6KQMbJ\\nftMJ5P2P146JdU8RB31SKL9qbZxzR4mA0uKKvUYtDQN+yErUnoOsm9wb9Z+RcAEc\\nZnZWOO02hGdHa7qkkbAxHuH91KnZbk8jnZm2LT7PFz7Y1fd80vSlnSOL7nRkU7B5\\nWXlJy8ECgYA4g0wc0Jq8c1Q0FulMkOQqYRDXaDo34987L+mZ70i/RtdkKjK/IKJ9\\n18UDCyEaDPD0BWBJGPejZkY8UD6FBG/5k7wNIbT7hHLRSRlw4iRmVX2hRVXrXzD8\\nvc86Qyg2iG0JqkMAvRdH40amPKp5bW4VcfcvQo4TSsI972u12rgwtg==\\n-----END RSA PRIVATE KEY-----\\n",
			sshPublicKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDJ7JmNYPHk1r1vDV/rUziz5CUtP+TKsHfsBHaPlJv8D2G8lqQs1BCk0L4pV9g2XiN48b7qXAerJ7/+lwL37RJawdbCBpqGMwucC2FHaTwkBqQ0D3FX/AfPRhT304oR61o67FelZk9cIdfTO5A7ztht0rNFuDEAKKszc+GTCvtcu1C1ZmTJcbfgJBdlBaAEhLUWX4+UY6lWk4+dahk8ylsArviL9PPiw/xmGpzp3JrWGttTBjdh8xk1heloDDvBZWOryCt+zT4WXdLEevNNeB7ab55kBnqI8nxxfi5grGLRmbGkz9qAjoyaOrQxTvlXGmrbYtpS8cik59flTGGSuzGX root@aiplatform",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pluginInterface := New(pluginsinterface.PluginClientset{}, test.params)
			plugin := pluginInterface.(*sshPlugin)

			if plugin.sshKeyFilePath != test.sshKeyFilePath {
				t.Errorf("Expected sshKeyFilePath=%s, got %s", test.sshKeyFilePath, plugin.sshKeyFilePath)
			}
			if plugin.sshPrivateKey != test.sshPrivateKey {
				t.Errorf("Expected sshPrivateKey=%s, got %s", test.sshPrivateKey, plugin.sshPrivateKey)
			}
			if plugin.sshPublicKey != test.sshPublicKey {
				t.Errorf("Expected sshPublicKey=%s, got %s", test.sshPublicKey, plugin.sshPublicKey)
			}
		})
	}
}
