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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestDeleteJobJob(t *testing.T) {
	response := v1alpha1.Job{}
	response.Name = "testJob"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}

	})

	server := httptest.NewServer(handler)
	defer server.Close()

	deleteJobFlags.Master = server.URL
	deleteJobFlags.Namespace = "test"
	deleteJobFlags.JobName = "testJob"

	testCases := []struct {
		Name        string
		ExpectValue error
	}{
		{
			Name:        "DeleteJob",
			ExpectValue: nil,
		},
	}

	for i, testcase := range testCases {
		err := DeleteJob()
		if err != nil {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, err)
		}
	}

}

func TestInitDeleteFlags(t *testing.T) {
	var cmd cobra.Command
	InitDeleteFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}

}
