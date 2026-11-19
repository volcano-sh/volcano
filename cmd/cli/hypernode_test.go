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

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestHyperNodeTreeRejectsInvalidArguments(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"charset", []string{"tree", "--charset=invalid"}, "charset"},
		{"position", []string{"tree", "unexpected"}, "unexpected"},
		{"config", []string{"tree", "-k", "/does-not-exist", "-s", "http://127.0.0.1:1"}, "does-not-exist"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := buildHyperNodeCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, output.String())
		})
	}

}

func TestHyperNodeTreeCanDisableDRAWithoutHidingDefaultReadErrors(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%t", disabled), func(t *testing.T) {
			var draRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/apis/topology.volcano.sh/v1alpha1/hypernodes":
					fmt.Fprint(w, `{"apiVersion":"topology.volcano.sh/v1alpha1","kind":"HyperNodeList","items":[]}`)
				case "/api/v1/nodes":
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"NodeList","items":[]}`)
				case "/apis/resource.k8s.io/v1/resourceslices":
					draRequests.Add(1)
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"ResourceSlice access denied"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config")
			require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
				Clusters:       map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
				Contexts:       map[string]*clientcmdapi.Context{"test": {Cluster: "test"}},
				CurrentContext: "test",
			}, path))
			cmd := buildHyperNodeCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var output bytes.Buffer
			cmd.SetOut(&output)
			args := []string{"tree", "-k", path}
			if disabled {
				args = append(args, "--dra=false")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if disabled {
				require.NoError(t, err)
				assert.Equal(t, int32(0), draRequests.Load())
				assert.Equal(t, "No HyperNodes found.\n", output.String())
			} else {
				require.ErrorContains(t, err, "ResourceSlice access denied")
				assert.ErrorContains(t, err, "--dra=false")
				assert.Equal(t, int32(1), draRequests.Load())
				assert.Empty(t, output.String())
			}
		})
	}
}
