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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			FileName:  testcase.FileName,
		}

		err := RunJob(context.TODO())
		if err != nil {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, err)
		}
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

func TestRunJobUsesFileNamespace(t *testing.T) {
	var requestPath, bodyNamespace string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		job := v1alpha1.Job{}
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		bodyNamespace = job.Namespace
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(job)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	fileName := filepath.Join(t.TempDir(), "job.yaml")
	content := []byte("apiVersion: batch.volcano.sh/v1alpha1\nkind: Job\nmetadata:\n  name: demo\n  namespace: team-a\n")
	if err := os.WriteFile(fileName, content, 0o600); err != nil {
		t.Fatal(err)
	}

	launchJobFlags = &runFlags{
		CommonFlags: util.CommonFlags{
			Master: server.URL,
		},
		Requests: "cpu=1000m,memory=100Mi",
		Limits:   "cpu=1000m,memory=100Mi",
		FileName: fileName,
	}

	if err := RunJob(context.TODO()); err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}
	if want := "/apis/batch.volcano.sh/v1alpha1/namespaces/team-a/jobs"; requestPath != want {
		t.Errorf("job created at %q, want %q", requestPath, want)
	}
	if bodyNamespace != "team-a" {
		t.Errorf("job sent with namespace %q, want %q", bodyNamespace, "team-a")
	}
}

func TestResolveJobNamespace(t *testing.T) {
	jobIn := func(namespace string) *v1alpha1.Job {
		return &v1alpha1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: namespace}}
	}
	testCases := []struct {
		name          string
		flagNamespace string
		job           *v1alpha1.Job
		want          string
		wantErr       bool
	}{
		{name: "no flag, no file", want: "default"},
		{name: "flag only", flagNamespace: "test", want: "test"},
		{name: "file without namespace", job: jobIn(""), want: "default"},
		{name: "file namespace", job: jobIn("team-a"), want: "team-a"},
		{name: "flag matches file", flagNamespace: "team-a", job: jobIn("team-a"), want: "team-a"},
		{name: "flag overrides file without namespace", flagNamespace: "test", job: jobIn(""), want: "test"},
		{name: "flag contradicts file", flagNamespace: "test", job: jobIn("team-a"), wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveJobNamespace(tc.flagNamespace, tc.job)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected an error, got namespace %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got namespace %q, want %q", got, tc.want)
			}
		})
	}
}
