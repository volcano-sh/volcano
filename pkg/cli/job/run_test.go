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
	"strings"
	"testing"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
)

func TestCreateJob(t *testing.T) {
	response := v1alpha1.Job{}
	var lastRequestPath string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRequestPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	tempDir := t.TempDir()

	fileWithNamespace := filepath.Join(tempDir, "job-with-namespace.yaml")
	jobWithNs := v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-with-ns",
			Namespace: "team-a",
		},
	}
	dataWithNs, err := yaml.Marshal(jobWithNs)
	if err != nil {
		t.Fatalf("failed to marshal jobWithNs: %v", err)
	}
	if err := os.WriteFile(fileWithNamespace, dataWithNs, 0644); err != nil {
		t.Fatalf("failed to write fileWithNamespace: %v", err)
	}

	fileWithoutNamespace := filepath.Join(tempDir, "job-no-namespace.yaml")
	jobWithoutNs := v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "job-without-ns",
		},
	}
	dataWithoutNs, err := yaml.Marshal(jobWithoutNs)
	if err != nil {
		t.Fatalf("failed to marshal jobWithoutNs: %v", err)
	}
	if err := os.WriteFile(fileWithoutNamespace, dataWithoutNs, 0644); err != nil {
		t.Fatalf("failed to write fileWithoutNamespace: %v", err)
	}

	testCases := []struct {
		Name           string
		JobName        string
		Namespace      string
		FileName       string
		ExpectErr      bool
		ExpectedErrMsg string
		ExpectedPath   string
	}{
		{
			Name:         "CreateJob",
			JobName:      "test",
			Namespace:    "test",
			ExpectErr:    false,
			ExpectedPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/test/jobs",
		},
		{
			Name:         "CreateJobDefaultNamespace",
			JobName:      "test",
			Namespace:    "",
			ExpectErr:    false,
			ExpectedPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/default/jobs",
		},
		{
			Name:         "CreateJobWithFile",
			FileName:     fileWithoutNamespace,
			Namespace:    "test",
			ExpectErr:    false,
			ExpectedPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/test/jobs",
		},
		{
			Name:         "CreateJobWithFile_NamespaceFromYAML",
			FileName:     fileWithNamespace,
			Namespace:    "",
			ExpectErr:    false,
			ExpectedPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/team-a/jobs",
		},
		{
			Name:         "CreateJobWithFile_MatchingNamespace",
			FileName:     fileWithNamespace,
			Namespace:    "team-a",
			ExpectErr:    false,
			ExpectedPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/team-a/jobs",
		},
		{
			Name:           "CreateJobWithFile_MismatchedNamespace",
			FileName:       fileWithNamespace,
			Namespace:      "team-b",
			ExpectErr:      true,
			ExpectedErrMsg: `the namespace from the provided object "team-a" does not match the namespace "team-b"`,
		},
		{
			Name:         "CreateJobWithFile_NoNamespaceInYAML_DefaultsToDefault",
			FileName:     fileWithoutNamespace,
			Namespace:    "",
			ExpectErr:    false,
			ExpectedPath: "/apis/batch.volcano.sh/v1alpha1/namespaces/default/jobs",
		},
		{
			Name:           "CreateJob_BlankNameAndFile",
			JobName:        "",
			FileName:       "",
			ExpectErr:      true,
			ExpectedErrMsg: "job name cannot be left blank",
		},
		{
			Name:           "CreateJob_InvalidFileType",
			JobName:        "test",
			FileName:       "invalid.txt",
			ExpectErr:      true,
			ExpectedErrMsg: "only support yaml file",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			lastRequestPath = ""
			launchJobFlags = &runFlags{
				CommonFlags: util.CommonFlags{
					Master: server.URL,
				},
				Name:      tc.JobName,
				Namespace: tc.Namespace,
				FileName:  tc.FileName,
				Requests:  "cpu=1000m,memory=100Mi",
			}

			err := RunJob(context.TODO())
			if tc.ExpectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.ExpectedErrMsg)
				}
				if !strings.Contains(err.Error(), tc.ExpectedErrMsg) {
					t.Fatalf("expected error containing %q, got %q", tc.ExpectedErrMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.ExpectedPath != "" && lastRequestPath != tc.ExpectedPath {
					t.Fatalf("expected request path %q, got %q", tc.ExpectedPath, lastRequestPath)
				}
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
	if cmd.Flag("namespace").DefValue != "" {
		t.Errorf("expected default value of namespace flag to be empty, got %q", cmd.Flag("namespace").DefValue)
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
