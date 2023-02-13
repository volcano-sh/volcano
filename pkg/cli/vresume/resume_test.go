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

package vresume

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	v1alpha1batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
)

func TestResumeJob(t *testing.T) {
	responsecommand := v1alpha1.Command{}
	responsejob := v1alpha1batch.Job{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "command") {
			w.Header().Set("Content-Type", "application/json")
			val, err := json.Marshal(responsecommand)
			if err == nil {
				w.Write(val)
			}

		} else {
			w.Header().Set("Content-Type", "application/json")
			val, err := json.Marshal(responsejob)
			if err == nil {
				w.Write(val)
			}

		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	resumeJobFlags.Master = server.URL
	resumeJobFlags.Namespace = "test"
	resumeJobFlags.JobName = "testjob"

	testCases := []struct {
		Name        string
		ExpectValue error
	}{
		{
			Name:        "ResumeJob",
			ExpectValue: nil,
		},
	}

	for i, testcase := range testCases {
		err := ResumeJob()
		if err != nil {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, err)
		}
	}

}

func TestInitResumeFlags(t *testing.T) {
	var cmd cobra.Command
	InitResumeFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag scheduler")
	}

}
