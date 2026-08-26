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

package pod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestListPod(t *testing.T) {
	testCases := []struct {
		name           string
		Response       interface{}
		Namespace      string
		JobName        string
		QueueName      string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					buildPod("default", "my-pod",
						map[string]string{v1alpha1.JobNameKey: "my-job1"}, map[string]string{}),
				},
			},
			Namespace:   "default",
			JobName:     "",
			ExpectedErr: nil,
			ExpectedOutput: `Name          Ready      Status         Restart  Age       
my-pod        0/1        Running        0        3d`,
		},
		{
			name: "Normal Case with namespace filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					buildPod("default", "my-pod",
						map[string]string{v1alpha1.JobNameKey: "my-job1"}, map[string]string{}),
				},
			},
			Namespace:   "default",
			JobName:     "",
			ExpectedErr: nil,
			ExpectedOutput: `Name          Ready      Status         Restart  Age       
my-pod        0/1        Running        0        3d`,
		},
		{
			name: "Normal Case with jobName filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					buildPod("default", "my-pod",
						map[string]string{v1alpha1.JobNameKey: "my-job1"}, map[string]string{}),
				},
			},
			Namespace:   "default",
			JobName:     "my-job1",
			ExpectedErr: nil,
			ExpectedOutput: `Name          Ready      Status         Restart  Age       
my-pod        0/1        Running        0        3d`,
		},
		{
			name: "Normal Case with queueName filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					buildPod("default", "my-pod1",
						map[string]string{v1alpha1.QueueNameKey: "my-queue1"}, map[string]string{}),
					buildPod("default", "my-pod2",
						map[string]string{v1alpha1.JobNameKey: "my-job2", v1alpha1.QueueNameKey: "my-queue1"}, map[string]string{}),
					buildPod("default", "my-pod3",
						map[string]string{}, map[string]string{schedulingv1beta1.QueueNameAnnotationKey: "my-queue1"}),
				},
			},
			Namespace:   "default",
			QueueName:   "my-queue1",
			ExpectedErr: nil,
			ExpectedOutput: `Name           Ready      Status         Restart  Age       
my-pod1        0/1        Running        0        3d        
my-pod2        0/1        Running        0        3d        
my-pod3        0/1        Running        0        3d`,
		},
		{
			name: "Normal Case with queueName filter and jobName filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					buildPod("default", "my-pod1",
						map[string]string{v1alpha1.JobNameKey: "my-job1", v1alpha1.QueueNameKey: "my-queue1"}, map[string]string{}),
				},
			},
			Namespace:   "default",
			QueueName:   "my-queue1",
			JobName:     "my-job1",
			ExpectedErr: nil,
			ExpectedOutput: `Name           Ready      Status         Restart  Age       
my-pod1        0/1        Running        0        3d`,
		},
		{
			name: "Normal Case with queueName filter and jobName filter, and does not match",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					buildPod("default", "my-pod1",
						map[string]string{v1alpha1.JobNameKey: "my-job1", v1alpha1.QueueNameKey: "my-queue1"}, map[string]string{}),
				},
			},
			Namespace:      "default",
			QueueName:      "my-queue2",
			JobName:        "my-job1",
			ExpectedErr:    fmt.Errorf("the input vcjob %s does not match the queue %s", "my-job1", "my-queue2"),
			ExpectedOutput: "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			listPodFlags.Master = server.URL
			listPodFlags.Namespace = testCase.Namespace
			listPodFlags.JobName = testCase.JobName
			listPodFlags.QueueName = testCase.QueueName
			listPodFlags.Namespace = testCase.Namespace
			r, oldStdout := redirectStdout()
			defer r.Close()

			err := ListPods(context.TODO())
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

func buildPod(namespace, name string, labels map[string]string, annotations map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:               types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			Name:              name,
			Namespace:         namespace,
			Labels:            labels,
			Annotations:       annotations,
			CreationTimestamp: metav1.Time{Time: time.Now().UTC().AddDate(0, 0, -3)},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "my-container",
					Image: "nginx",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func TestPrintPodsAllNamespaces(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			buildPod("team-a", "worker-0", nil, nil),
			buildPod("team-b", "worker-0", nil, nil),
		},
	}

	var buf bytes.Buffer
	PrintPods(pods, &buf, true)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header plus 2 rows, got %d lines: %q", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], Namespace) {
		t.Errorf("expected header to start with %q, got %q", Namespace, lines[0])
	}
	if !strings.HasPrefix(lines[1], "team-a") || !strings.Contains(lines[1], "worker-0") {
		t.Errorf("expected first row for team-a/worker-0, got %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "team-b") || !strings.Contains(lines[2], "worker-0") {
		t.Errorf("expected second row for team-b/worker-0, got %q", lines[2])
	}
}

func TestPrintPodsWithoutNamespaceColumn(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			buildPod("team-a", "worker-0", nil, nil),
		},
	}

	var buf bytes.Buffer
	PrintPods(pods, &buf, false)

	out := buf.String()
	if strings.Contains(out, Namespace) {
		t.Errorf("did not expect a Namespace column, got %q", out)
	}
	if !strings.HasPrefix(out, Name) {
		t.Errorf("expected header to start with %q, got %q", Name, out)
	}
}

func TestPrintPodsHeaderDrivenWidth(t *testing.T) {
	// When the header is wider than every value (a single-character namespace)
	// or there are no rows at all, the header must still keep a separator from
	// the next column instead of touching it.
	cases := map[string]*corev1.PodList{
		"single char namespace": {
			Items: []corev1.Pod{buildPod("a", "w", nil, nil)},
		},
		"empty list": {},
	}

	for name, pods := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintPods(pods, &buf, true)

			header := strings.SplitN(buf.String(), "\n", 2)[0]
			if !strings.HasPrefix(header, Namespace+" ") {
				t.Errorf("expected %q to be followed by a separator, got %q", Namespace, header)
			}
		})
	}
}

func TestSortPods(t *testing.T) {
	// Rows can turn up with the namespaces interleaved, so check they come back
	// grouped by namespace, and by name within each one.
	pods := []corev1.Pod{
		buildPod("team-b", "worker-1", nil, nil),
		buildPod("team-a", "worker-1", nil, nil),
		buildPod("team-b", "worker-0", nil, nil),
		buildPod("team-a", "worker-0", nil, nil),
	}

	sortPods(pods)

	var got []string
	for _, p := range pods {
		got = append(got, p.Namespace+"/"+p.Name)
	}
	want := []string{"team-a/worker-0", "team-a/worker-1", "team-b/worker-0", "team-b/worker-1"}
	if !slices.Equal(got, want) {
		t.Errorf("expected pods in order %v, got %v", want, got)
	}
}

func TestAppendIfNotExists(t *testing.T) {
	existing := []corev1.Pod{
		buildPod("team-a", "worker-0", nil, nil),
	}
	toAppend := []corev1.Pod{
		buildPod("team-a", "worker-0", nil, nil),
		buildPod("team-b", "worker-0", nil, nil),
	}

	got := appendIfNotExists(existing, toAppend)

	var keys []string
	for _, p := range got {
		keys = append(keys, p.Namespace+"/"+p.Name)
	}
	want := []string{"team-a/worker-0", "team-b/worker-0"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("expected pods %v, got %v", want, keys)
	}
}
