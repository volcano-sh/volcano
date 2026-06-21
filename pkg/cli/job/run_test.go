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
	"testing"
	"time"
	"volcano.sh/volcano/pkg/cli/util"

	"github.com/spf13/cobra"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestCreateJob(t *testing.T) {
	response := v1alpha1.Job{}
	requestPath := ""

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}

	})

	server := httptest.NewServer(handler)
	defer server.Close()

	fileName := time.Now().Format("20060102150405999") + "testCreateJob.yaml"
	fileJob := v1alpha1.Job{}
	fileJob.Namespace = "file-ns"
	val, err := json.Marshal(fileJob)
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
		ExpectPath  string
	}{
		{
			Name:        "CreateJob",
			ExpectValue: nil,
			ExpectPath:  "/apis/batch.volcano.sh/v1alpha1/namespaces/test/jobs",
		},
		{
			Name:        "CreateJobWithFile",
			FileName:    fileName,
			ExpectValue: nil,
			ExpectPath:  "/apis/batch.volcano.sh/v1alpha1/namespaces/file-ns/jobs",
		},
	}

	for i, testcase := range testCases {
		requestPath = ""
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
		if requestPath != testcase.ExpectPath {
			t.Errorf("case %d (%s): expected path: %v, got %v ", i, testcase.Name, testcase.ExpectPath, requestPath)
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
