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

package jobflow

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
)

func TestDescribeJobFlowDocuments(t *testing.T) {
	tests := []struct {
		name   string
		format string
		names  []string
	}{
		{name: "empty YAML list", format: "yaml"},
		{name: "single YAML object", format: "yaml", names: []string{"first"}},
		{name: "multiple YAML objects", format: "yaml", names: []string{"first", "second"}},
		{name: "multiple JSON objects", format: "json", names: []string{"first", "second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := &flowv1alpha1.JobFlowList{}
			for _, name := range tt.names {
				response.Items = append(response.Items, flowv1alpha1.JobFlow{
					TypeMeta:   metav1.TypeMeta{APIVersion: flowv1alpha1.SchemeGroupVersion.String(), Kind: "JobFlow"},
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				})
			}
			server := util.CreateTestServer(response)
			defer server.Close()

			// An explicit empty kubeconfig keeps the test independent of local credentials.
			kubeconfig := filepath.Join(t.TempDir(), "config")
			require.NoError(t, os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0600))
			savedFlags := *describeJobFlowFlags
			t.Cleanup(func() { *describeJobFlowFlags = savedFlags })
			*describeJobFlowFlags = describeFlags{
				CommonFlags: util.CommonFlags{Master: server.URL, Kubeconfig: kubeconfig},
				Namespace:   "default",
				Format:      tt.format,
			}

			reader, stdout := util.RedirectStdout()
			defer reader.Close()
			// Restore stdout even when Describe fails, before asserting on its result.
			err := DescribeJobFlow(context.Background())
			output := util.CaptureOutput(reader, stdout)
			require.NoError(t, err)

			decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(output), 4096)
			var names []string
			for {
				var object flowv1alpha1.JobFlow
				err := decoder.Decode(&object)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				names = append(names, object.Name)
			}
			assert.Equal(t, tt.names, names, "all described objects must survive decoding")
		})
	}
}
