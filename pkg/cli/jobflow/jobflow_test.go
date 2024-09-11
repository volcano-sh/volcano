/*
Copyright 2024 The Volcano Authors.

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
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func TestListJobFlow(t *testing.T) {
	testCases := []struct {
		name           string
		Response       interface{}
		Namespace      string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobFlowList{
				Items: []flowv1alpha1.JobFlow{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "test-jobflow",
							Namespace:         "default",
							CreationTimestamp: metav1.Time{Time: time.Now().UTC().AddDate(0, 0, -3)},
						},
						Status: flowv1alpha1.JobFlowStatus{
							State: flowv1alpha1.State{
								Phase: "Succeed",
							},
						},
					},
				},
			},
			Namespace:   "default",
			ExpectedErr: nil,
			ExpectedOutput: `Name            Namespace    Phase      Age    
test-jobflow    default      Succeed    3d`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			listJobFlowFlags.Master = server.URL
			listJobFlowFlags.Namespace = testCase.Namespace

			r, oldStdout := redirectStdout()
			defer r.Close()
			err := ListJobFlow(context.TODO())
			gotOutput := captureOutput(r, oldStdout)

			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
			if gotOutput != testCase.ExpectedOutput {
				t.Errorf("test case: %s failed: got: %s, want: %s", testCase.name, gotOutput, testCase.ExpectedOutput)
			}
		})
	}
}

func TestGetJobFlow(t *testing.T) {
	testCases := []struct {
		name           string
		Response       *flowv1alpha1.JobFlow
		Namespace      string
		Name           string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-jobflow",
					Namespace:         "default",
					CreationTimestamp: metav1.Now(),
				},
				Status: flowv1alpha1.JobFlowStatus{
					State: flowv1alpha1.State{
						Phase: "Succeed",
					},
				},
			},
			Namespace:   "default",
			Name:        "test-jobflow",
			ExpectedErr: nil,
			ExpectedOutput: `Name            Namespace    Phase      Age    
test-jobflow    default      Succeed    0s`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			getJobFlowFlags.Master = server.URL
			// Set the namespace and name as the flags
			getJobFlowFlags.Namespace = testCase.Namespace
			getJobFlowFlags.Name = testCase.Name

			r, oldStdout := redirectStdout()
			defer r.Close()
			err := GetJobFlow(context.TODO())
			gotOutput := captureOutput(r, oldStdout)
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
			if gotOutput != testCase.ExpectedOutput {
				t.Fatalf("test case: %s failed: got: %s, want: %s", testCase.name, gotOutput, testCase.ExpectedOutput)
			}
		})
	}
}

func TestDeleteJobFlow(t *testing.T) {
	testCases := []struct {
		name           string
		Response       *flowv1alpha1.JobFlow
		Namespace      string
		Name           string
		FilePath       string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobflow",
					Namespace: "default",
				},
			},
			Namespace:      "default",
			Name:           "test-jobflow",
			ExpectedErr:    nil,
			ExpectedOutput: `Deleted JobFlow: default/test-jobflow`,
		},
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobflow",
					Namespace: "default",
				},
			},
			FilePath:    "test.yaml",
			ExpectedErr: nil,
			ExpectedOutput: `Deleted JobFlow: default/test-a
Deleted JobFlow: default/test-b`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			deleteJobFlowFlags.Master = server.URL
			deleteJobFlowFlags.Namespace = testCase.Namespace
			deleteJobFlowFlags.Name = testCase.name
			deleteJobFlowFlags.FilePath = testCase.FilePath

			if testCase.FilePath != "" {
				err := createAndWriteFile(testCase.FilePath, content)
				if err != nil {
					t.Fatalf("Failed to create and write file: %v", err)
				}
				// Delete the file after the test
				defer func() {
					err := os.Remove(testCase.FilePath)
					if err != nil {
						t.Fatalf("Failed to remove file: %v", err)
					}
				}()
			}

			r, oldStdout := redirectStdout()
			defer r.Close()
			err := DeleteJobFlow(context.TODO())
			gotOutput := captureOutput(r, oldStdout)
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
			if gotOutput != testCase.ExpectedOutput {
				t.Fatalf("test case: %s failed: got: %s, want: %s", testCase.name, gotOutput, testCase.ExpectedOutput)
			}
		})
	}
}

func TestCreateJobFlow(t *testing.T) {
	testCases := []struct {
		name           string
		Response       *flowv1alpha1.JobFlow
		FilePath       string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobFlow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobflow",
					Namespace: "default",
				},
			},
			FilePath:    "test.yaml",
			ExpectedErr: nil,
			ExpectedOutput: `Created JobFlow: default/test-a
Created JobFlow: default/test-b`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			createJobFlowFlags.Master = server.URL
			createJobFlowFlags.FilePath = testCase.FilePath

			if testCase.FilePath != "" {
				err := createAndWriteFile(testCase.FilePath, content)
				if err != nil {
					t.Fatalf("Failed to create and write file: %v", err)
				}
				// Delete the file after the test
				defer func() {
					err := os.Remove(testCase.FilePath)
					if err != nil {
						t.Fatalf("Failed to remove file: %v", err)
					}
				}()
			}
			r, oldStdout := redirectStdout()
			defer r.Close()
			err := CreateJobFlow(context.TODO())
			gotOutput := captureOutput(r, oldStdout)
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
			if gotOutput != testCase.ExpectedOutput {
				t.Fatalf("test case: %s failed: got: %s, want: %s", testCase.name, gotOutput, testCase.ExpectedOutput)
			}
		})
	}
}

func TestDescribeJobFlow(t *testing.T) {
	testCases := []struct {
		name           string
		Response       *flowv1alpha1.JobFlow
		Namespace      string
		Name           string
		Format         string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case, use yaml format",
			Response: &flowv1alpha1.JobFlow{
				TypeMeta: metav1.TypeMeta{
					APIVersion: flowv1alpha1.SchemeGroupVersion.String(),
					Kind:       "JobFlow",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobflow",
					Namespace: "default",
				},
			},
			Namespace:   "default",
			Name:        "test-jobflow",
			Format:      "yaml",
			ExpectedErr: nil,
			ExpectedOutput: `apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  creationTimestamp: null
  name: test-jobflow
  namespace: default
spec: {}
status:
  state: {}`,
		},
		{
			name: "Normal Case, use json format",
			Response: &flowv1alpha1.JobFlow{
				TypeMeta: metav1.TypeMeta{
					APIVersion: flowv1alpha1.SchemeGroupVersion.String(),
					Kind:       "JobFlow",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobflow",
					Namespace: "default",
				},
			},
			Namespace:   "default",
			Name:        "test-jobflow",
			Format:      "json",
			ExpectedErr: nil,
			ExpectedOutput: `{
  "kind": "JobFlow",
  "apiVersion": "flow.volcano.sh/v1alpha1",
  "metadata": {
    "name": "test-jobflow",
    "namespace": "default",
    "creationTimestamp": null
  },
  "spec": {},
  "status": {
    "state": {}
  }
}`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			describeJobFlowFlags.Master = server.URL
			describeJobFlowFlags.Namespace = testCase.Namespace
			describeJobFlowFlags.Name = testCase.name
			describeJobFlowFlags.Format = testCase.Format

			r, oldStdout := redirectStdout()
			defer r.Close()
			err := DescribeJobFlow(context.TODO())
			gotOutput := captureOutput(r, oldStdout)
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
			if gotOutput != testCase.ExpectedOutput {
				t.Fatalf("test case: %s failed: got: %s, want: %s", testCase.name, gotOutput, testCase.ExpectedOutput)
			}
		})
	}
}

func createTestServer(response interface{}) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})

	server := httptest.NewServer(handler)
	return server
}

// redirectStdout redirects os.Stdout to a pipe and returns the read and write ends of the pipe.
func redirectStdout() (*os.File, *os.File) {
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	return r, oldStdout
}

// captureOutput reads from r until EOF and returns the result as a string.
func captureOutput(r *os.File, oldStdout *os.File) string {
	w := os.Stdout
	os.Stdout = oldStdout
	w.Close()
	gotOutput, _ := io.ReadAll(r)
	return strings.TrimSpace(string(gotOutput))
}

func createAndWriteFile(filePath, content string) error {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.WriteString(file, content)
		if err != nil {
			return err
		}
	}
	return nil
}

func TestInitCreateFlags(t *testing.T) {
	var cmd cobra.Command
	InitCreateFlags(&cmd)

	if cmd.Flag("file") == nil {
		t.Errorf("Could not find the flag file")
	}
}

func TestInitGetFlags(t *testing.T) {
	var cmd cobra.Command
	InitGetFlags(&cmd)

	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag name")
	}
}

func TestInitListFlags(t *testing.T) {
	var cmd cobra.Command
	InitListFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
}

func TestInitDescribeFlags(t *testing.T) {
	var cmd cobra.Command
	InitDescribeFlags(&cmd)
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("format") == nil {
		t.Errorf("Could not find the flag format")
	}
}

func TestInitDeleteFlags(t *testing.T) {
	var cmd cobra.Command
	InitDeleteFlags(&cmd)
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("file") == nil {
		t.Errorf("Could not find the flag file")
	}
}

var content = `apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  name: test-a
  namespace: default
spec:
  jobRetainPolicy: delete   # After jobflow runs, keep the generated job. Otherwise, delete it.
  flows:
    - name: a
    - name: b
      dependsOn:
        targets: ['a']
    - name: c
      dependsOn:
        targets: ['b']
    - name: d
      dependsOn:
        targets: ['b']
    - name: e
      dependsOn:
        targets: ['c','d']
---
apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  name: test-b
  namespace: default
spec:
  jobRetainPolicy: delete   # After jobflow runs, keep the generated job. Otherwise, delete it.
  flows:
    - name: a
    - name: b
      dependsOn:
        targets: ['a']
    - name: c
      dependsOn:
        targets: ['b']
    - name: d
      dependsOn:
        targets: ['b']
    - name: e
      dependsOn:
        targets: ['c','d']
---`
