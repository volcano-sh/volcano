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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

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
my-pod        0/1        Running        0        0s`,
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
my-pod        0/1        Running        0        0s`,
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
my-pod        0/1        Running        0        0s`,
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
my-pod1        0/1        Running        0        0s        
my-pod2        0/1        Running        0        0s        
my-pod3        0/1        Running        0        0s`,
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
my-pod1        0/1        Running        0        0s`,
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
			CreationTimestamp: metav1.Now(),
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
