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

package kube

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildConfigExplicitMaster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	cfg, err := BuildConfig(ClientOptions{
		Master:     "https://127.0.0.1:6443",
		KubeConfig: "",
		QPS:        50,
		Burst:      100,
	})
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1:6443", cfg.Host)
	require.Equal(t, float32(50), cfg.QPS)
	require.Equal(t, 100, cfg.Burst)
}
