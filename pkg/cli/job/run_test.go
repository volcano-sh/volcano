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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/validation"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
	"volcano.sh/volcano/pkg/cli/vsub"
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

func TestRunDottedJobName(t *testing.T) {
	saved := launchJobFlags
	t.Cleanup(func() { launchJobFlags = saved })
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("vsub=", legacy), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var job v1alpha1.Job
				if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
					t.Error(err)
				}
				if job.Name != "train.v2" {
					t.Errorf("job name = %q, want train.v2", job.Name)
				}
				name := job.Spec.Tasks[0].Template.Spec.Containers[0].Name
				if errs := validation.IsDNS1123Label(name); len(errs) != 0 {
					t.Errorf("invalid container name %q: %v", name, errs)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(job)
			}))
			defer server.Close()
			initFlags, runJob := InitRunFlags, RunJob
			if legacy {
				initFlags, runJob = vsub.InitRunFlags, vsub.RunJob
			}
			cmd := &cobra.Command{RunE: func(cmd *cobra.Command, args []string) error { return runJob(cmd.Context()) }}
			initFlags(cmd)
			cmd.SetArgs([]string{"--master", server.URL, "--name", "train.v2"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Errorf("got %d create requests, want 1", requests)
			}
		})
	}
}
