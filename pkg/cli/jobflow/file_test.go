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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func TestJobFlowFileDocuments(t *testing.T) {
	for _, operation := range []string{"create", "delete"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name         string
				resourceName string
				annotation   string
				value        string
				prefix       string
				separator    string
			}{
				{name: "ordinary documents", annotation: `"release ready"`, value: "release ready", separator: "---\n"},
				{name: "separator in resource name", resourceName: "first---stage", annotation: `"release ready"`, value: "release ready", separator: "---\n"},
				{name: "quoted separator", annotation: `"release---ready"`, value: "release---ready", separator: "---\n"},
				{name: "block scalar separator", annotation: "|-\n      release\n      ---\n      ready", value: "release\n---\nready", separator: "---\n"},
				{name: "empty and comment documents", annotation: `"release ready"`, value: "release ready", prefix: "---\n# header\n---\n", separator: "---\n# between objects\n---\n"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					firstName := "first"
					if tc.resourceName != "" {
						firstName = tc.resourceName
					}
					var mu sync.Mutex
					var requests []string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						mu.Lock()
						requests = append(requests, r.Method+" "+r.URL.Path)
						requestNumber := len(requests)
						mu.Unlock()
						if operation == "create" {
							var obj flowv1alpha1.JobFlow
							if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
								t.Errorf("decode request: %v", err)
								w.WriteHeader(http.StatusBadRequest)
								return
							}
							expectedName := "second"
							if requestNumber == 1 {
								expectedName = firstName
							}
							if obj.Name != expectedName {
								t.Errorf("name = %q, want %q", obj.Name, expectedName)
							}
							if got := obj.Annotations["example.com/message"]; got != tc.value {
								t.Errorf("annotation = %q, want %q", got, tc.value)
							}
							w.WriteHeader(http.StatusCreated)
							if err := json.NewEncoder(w).Encode(&obj); err != nil {
								t.Errorf("encode response: %v", err)
							}
						} else {
							fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","status":"Success"}`)
						}
					}))
					defer server.Close()

					manifest := func(name, namespace string) string {
						return fmt.Sprintf(`apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  name: %s
%s  annotations:
    example.com/message: %s
spec:
  jobRetainPolicy: retain
  flows:
    - name: worker
`, name, namespace, tc.annotation)
					}
					input := tc.prefix + manifest(firstName, "") + tc.separator + manifest("second", "  namespace: team-a\n")
					path := filepath.Join(t.TempDir(), "objects.yaml")
					if err := os.WriteFile(path, []byte(input), 0600); err != nil {
						t.Fatal(err)
					}
					kubeconfig := filepath.Join(t.TempDir(), "config")
					config := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
current-context: test
`, server.URL)
					if err := os.WriteFile(kubeconfig, []byte(config), 0600); err != nil {
						t.Fatal(err)
					}

					cmd := &cobra.Command{Use: operation}
					if operation == "create" {
						saved := *createJobFlowFlags
						t.Cleanup(func() { *createJobFlowFlags = saved })
						InitCreateFlags(cmd)
						cmd.RunE = func(cmd *cobra.Command, args []string) error { return CreateJobFlow(cmd.Context()) }
					} else {
						saved := *deleteJobFlowFlags
						t.Cleanup(func() { *deleteJobFlowFlags = saved })
						InitDeleteFlags(cmd)
						cmd.RunE = func(cmd *cobra.Command, args []string) error { return DeleteJobFlow(cmd.Context()) }
					}
					cmd.SetArgs([]string{"--kubeconfig", kubeconfig, "-f", path})
					err := cmd.ExecuteContext(context.Background())
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						t.Fatalf("%s -f valid YAML: %v; requests: %v", operation, err, requests)
					}
					base := "/apis/flow.volcano.sh/v1alpha1/namespaces/"
					expected := []string{
						"POST " + base + "default/jobflows",
						"POST " + base + "team-a/jobflows",
					}
					if operation == "delete" {
						expected = []string{
							"DELETE " + base + "default/jobflows/" + firstName,
							"DELETE " + base + "team-a/jobflows/second",
						}
					}
					if !reflect.DeepEqual(requests, expected) {
						t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(expected, "\n"))
					}
				})
			}
		})
	}
}
