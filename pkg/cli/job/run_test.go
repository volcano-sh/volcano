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

package job

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"volcano.sh/volcano/pkg/cli/util"

	"github.com/spf13/cobra"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestCreateJob(t *testing.T) {
	response := v1alpha1.Job{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}

	})

	server := httptest.NewServer(handler)
	defer server.Close()

	fileName := time.Now().Format("20060102150405999") + "testCreateJob.yaml"
	val, err := json.Marshal(response)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(fileName, val, os.ModePerm)
	if err != nil {
		panic(err)
	}
	defer os.Remove(fileName)

	testCases := []struct {
		Name        string
		ExpectValue error
		FileName    string
	}{
		{
			Name:        "CreateJob",
			ExpectValue: nil,
		},
		{
			Name:        "CreateJobWithFile",
			FileName:    fileName,
			ExpectValue: nil,
		},
	}

	for i, testcase := range testCases {
		launchJobFlags = &runFlags{
			CommonFlags: util.CommonFlags{
				Master: server.URL,
			},
			Name:      "test",
			Namespace: "test",
			Requests:  "cpu=1000m,memory=100Mi",
		}

		err := RunJob(context.TODO())
		if err != nil {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, err)
		}
	}

}

func TestRunJobNamespace(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&v1alpha1.Job{})
	}))
	defer server.Close()

	dir := t.TempDir()
	kubeconfig, err := util.CreateTestKubeconfig(dir, server.URL, "volcano-system")
	if err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}
	writeJobFile := func(name, namespace string) string {
		content := "apiVersion: batch.volcano.sh/v1alpha1\nkind: Job\nmetadata:\n  name: file-job\n"
		if namespace != "" {
			content += "  namespace: " + namespace + "\n"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write job file: %v", err)
		}
		return path
	}
	withNamespace := writeJobFile("with-namespace.yaml", "default")
	withoutNamespace := writeJobFile("without-namespace.yaml", "")

	testCases := []struct {
		name      string
		namespace string
		fileName  string
		wantPath  string
	}{
		{
			name:     "namespace of the current kubeconfig context",
			wantPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/volcano-system/jobs",
		},
		{
			name:      "namespace flag",
			namespace: "team-a",
			wantPath:  "/apis/batch.volcano.sh/v1alpha1/namespaces/team-a/jobs",
		},
		{
			name:     "namespace of the job file",
			fileName: withNamespace,
			wantPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/default/jobs",
		},
		{
			name:     "job file without a namespace",
			fileName: withoutNamespace,
			wantPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/volcano-system/jobs",
		},
		{
			name:      "namespace flag with a job file without a namespace",
			namespace: "team-a",
			fileName:  withoutNamespace,
			wantPath:  "/apis/batch.volcano.sh/v1alpha1/namespaces/team-a/jobs",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requestPath = ""
			launchJobFlags = &runFlags{
				CommonFlags: util.CommonFlags{Kubeconfig: kubeconfig},
				Name:        "test",
				Namespace:   tc.namespace,
				Requests:    "cpu=1000m,memory=100Mi",
				FileName:    tc.fileName,
			}
			r, oldStdout := util.RedirectStdout()
			defer r.Close()
			err := RunJob(context.TODO())
			util.CaptureOutput(r, oldStdout)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if requestPath != tc.wantPath {
				t.Errorf("request path: got %q, want %q", requestPath, tc.wantPath)
			}
		})
	}
}

func TestInitRunFlags(t *testing.T) {
	var cmd cobra.Command
	InitRunFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("scheduler") == nil {
		t.Errorf("Could not find the flag scheduler")
	}
	if cmd.Flag("image") == nil {
		t.Errorf("Could not find the flag image")
	}
	if cmd.Flag("replicas") == nil {
		t.Errorf("Could not find the flag replicas")
	}
	if cmd.Flag("min") == nil {
		t.Errorf("Could not find the flag min")
	}
	if cmd.Flag("requests") == nil {
		t.Errorf("Could not find the flag requests")
	}
	if cmd.Flag("limits") == nil {
		t.Errorf("Could not find the flag limits")
	}

}
