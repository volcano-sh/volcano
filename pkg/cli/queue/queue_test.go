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

package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/cli/podgroup"
	"volcano.sh/volcano/pkg/cli/util"
)

func getTestQueueHTTPServer(t *testing.T) *httptest.Server {

	response := v1beta1.Queue{}

	response.Name = "testQueue"
	response.Spec.Weight = int32(2)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})
	return httptest.NewServer(handler)
}

func getTestQueueListHTTPServer(t *testing.T) *httptest.Server {

	response := v1beta1.QueueList{}

	response.Items = []v1beta1.Queue{
		{
			ObjectMeta: v1.ObjectMeta{
				Name: "testQueue",
			},
			Spec: v1beta1.QueueSpec{
				Weight: int32(2),
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})
	return httptest.NewServer(handler)
}

func getCommonFlags(master string) util.CommonFlags {
	return util.CommonFlags{
		Master: master,
	}
}

func TestCreateQueue(t *testing.T) {
	InitCreateFlags(&cobra.Command{})

	type recorded struct {
		method string
		path   string
		body   string
	}
	var requests []recorded
	var failOn string

	response := v1beta1.Queue{}
	response.Name = "testQueue"
	response.Spec.Weight = int32(2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, recorded{r.Method, r.URL.Path, string(body)})
		if failOn != "" && strings.Contains(r.URL.Path, failOn) && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if val, err := json.Marshal(response); err == nil {
			w.Write(val)
		}
	}))
	defer server.Close()

	createQueueFlags.CommonFlags = getCommonFlags(server.URL)
	createQueueFlags.Name = "testQueue"
	createQueueFlags.Weight = int32(2)

	testCases := []struct {
		Name            string
		State           string
		FailOn          string
		WantErr         string
		WantRequests    int
		WantCloseAction bool
	}{
		{
			Name:         "state Open creates an open queue",
			State:        string(v1beta1.QueueStateOpen),
			WantRequests: 1,
		},
		{
			Name:         "state open in lower case is accepted",
			State:        strings.ToLower(string(v1beta1.QueueStateOpen)),
			WantRequests: 1,
		},
		{
			Name:            "state Closed closes the queue after creating it",
			State:           string(v1beta1.QueueStateClosed),
			WantRequests:    3,
			WantCloseAction: true,
		},
		{
			Name:            "state closed in lower case is accepted",
			State:           strings.ToLower(string(v1beta1.QueueStateClosed)),
			WantRequests:    3,
			WantCloseAction: true,
		},
		{
			Name:         "an unknown state is rejected before any request",
			State:        "Banana",
			WantErr:      "state Banana invalid",
			WantRequests: 0,
		},
		{
			Name:         "a misspelled state is rejected before any request",
			State:        "Clsoed",
			WantErr:      "state Clsoed invalid",
			WantRequests: 0,
		},
		{
			Name:         "an empty state is rejected before any request",
			State:        "",
			WantErr:      "invalid",
			WantRequests: 0,
		},
		{
			Name:         "a failed create does not send a close command",
			State:        string(v1beta1.QueueStateClosed),
			FailOn:       "/queues",
			WantErr:      "an error on the server",
			WantRequests: 1,
		},
		{
			Name:            "a failed close reports that the queue was created",
			State:           string(v1beta1.QueueStateClosed),
			FailOn:          "/commands",
			WantErr:         "was created, but submitting its close command failed",
			WantRequests:    3,
			WantCloseAction: true,
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			requests = nil
			failOn = testcase.FailOn
			createQueueFlags.State = testcase.State

			err := CreateQueue(context.TODO())
			if testcase.WantErr == "" && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if testcase.WantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got none", testcase.WantErr)
				}
				if !strings.Contains(err.Error(), testcase.WantErr) {
					t.Errorf("expected an error containing %q, got %v", testcase.WantErr, err)
				}
			}

			if len(requests) != testcase.WantRequests {
				t.Errorf("expected %d requests, got %d: %v", testcase.WantRequests, len(requests), requests)
			}

			var closeBody string
			for _, req := range requests {
				if req.method == http.MethodDelete {
					t.Errorf("the queue must not be deleted, got %s %s", req.method, req.path)
				}
				if req.method == http.MethodPost && strings.HasSuffix(req.path, "/commands") {
					closeBody = req.body
				}
			}
			if !testcase.WantCloseAction {
				if closeBody != "" {
					t.Errorf("no close command expected, got %s", closeBody)
				}
				return
			}
			if closeBody == "" {
				t.Fatalf("expected a close command, requests were %v", requests)
			}
			if !strings.Contains(closeBody, `"action":"CloseQueue"`) {
				t.Errorf("expected a CloseQueue action, got %s", closeBody)
			}
			if !strings.Contains(closeBody, createQueueFlags.Name) {
				t.Errorf("expected the command to target %s, got %s", createQueueFlags.Name, closeBody)
			}
		})
	}
}

func TestGetQueue(t *testing.T) {
	InitGetFlags(&cobra.Command{})
	server := getTestQueueHTTPServer(t)
	defer server.Close()

	getQueueFlags.CommonFlags = getCommonFlags(server.URL)

	testCases := []struct {
		Name        string
		ExpectValue error
		QueueName   string
	}{
		{
			Name:        "GetQueue",
			ExpectValue: nil,
			QueueName:   "testQueue",
		},
		{
			Name:        "",
			ExpectValue: fmt.Errorf("name is mandatory to get the particular queue details"),
			QueueName:   "",
		},
	}
	for _, testcase := range testCases {
		getQueueFlags.Name = testcase.QueueName
		err := GetQueue(context.TODO())
		if err != nil && (err.Error() != testcase.ExpectValue.Error()) {
			t.Errorf("(%s): expected: %v, got %v ", testcase.Name, testcase.ExpectValue, err)
		}
	}
}

func TestGetQueue_empty(t *testing.T) {
	InitGetFlags(&cobra.Command{})
	server := getTestQueueHTTPServer(t)
	defer server.Close()

	listQueueFlags.CommonFlags = getCommonFlags(server.URL)

	testCases := []struct {
		Name        string
		ExpectValue error
		QueueName   string
	}{
		{
			Name:        "GetQueue",
			ExpectValue: nil,
		},
	}
	for _, testcase := range testCases {
		err := ListQueue(context.TODO())
		if err != nil && (err.Error() != testcase.ExpectValue.Error()) {
			t.Errorf("(%s): expected: %v, got %v ", testcase.Name, testcase.ExpectValue, err)
		}
	}
}

func TestGetQueue_nonempty(t *testing.T) {
	InitGetFlags(&cobra.Command{})
	server := getTestQueueListHTTPServer(t)
	defer server.Close()

	listQueueFlags.CommonFlags = getCommonFlags(server.URL)

	testCases := []struct {
		Name        string
		ExpectValue error
		QueueName   string
	}{
		{
			Name:        "GetQueue",
			ExpectValue: nil,
			QueueName:   "testQueue",
		},
		{
			Name:        "",
			ExpectValue: fmt.Errorf("name is mandatory to get the particular queue details"),
			QueueName:   "",
		},
	}
	for _, testcase := range testCases {
		err := ListQueue(context.TODO())
		if err != nil && err.Error() != testcase.ExpectValue.Error() {
			t.Errorf("(%s): expected: %v, got %v ", testcase.Name, testcase.ExpectValue, err)
		}
	}
}

func TestListQueue(t *testing.T) {
	InitListFlags(&cobra.Command{})

	testCases := []struct {
		name       string
		queues     *v1beta1.QueueList
		queueStats map[string]*podgroup.PodGroupStatistics
		expected   string
	}{
		{
			name: "Single queue with formatting",
			queues: &v1beta1.QueueList{
				Items: []v1beta1.Queue{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "test-queue",
						},
						Spec: v1beta1.QueueSpec{
							Weight: 1,
							Parent: "root",
						},
						Status: v1beta1.QueueStatus{
							State: v1beta1.QueueStateOpen,
						},
					},
				},
			},
			queueStats: map[string]*podgroup.PodGroupStatistics{
				"test-queue": {
					Inqueue:   1,
					Pending:   2,
					Running:   3,
					Unknown:   4,
					Completed: 5,
				},
			},
			expected: `Name                     Weight  State   Parent  Inqueue Pending Running Unknown Completed
test-queue               1       Open    root    1       2       3       4       5       
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintQueues(tc.queues, tc.queueStats, &buf)
			got := buf.String()
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

// Test ListQueue can output results normally when there are residual podgroups with their bound queues not exist.
func TestListQueue_residualPg(t *testing.T) {
	mockServer := func() *httptest.Server {
		queues := v1beta1.QueueList{}
		queues.Items = []v1beta1.Queue{
			{
				ObjectMeta: v1.ObjectMeta{
					Name: "testQueue1",
				},
			},
			{
				ObjectMeta: v1.ObjectMeta{
					Name: "testQueue2",
				},
			},
		}
		podGroups := v1beta1.PodGroupList{}
		podGroups.Items = []v1beta1.PodGroup{
			{
				ObjectMeta: v1.ObjectMeta{
					Name: "testPodGroup1",
				},
				Spec: v1beta1.PodGroupSpec{
					Queue: "testQueue1",
				},
			},
			{
				ObjectMeta: v1.ObjectMeta{
					Name: "testPodGroup2",
				},
				Spec: v1beta1.PodGroupSpec{
					Queue: "testQueue2",
				},
			},
			{
				ObjectMeta: v1.ObjectMeta{
					Name: "testPodGroup3",
				},
				Spec: v1beta1.PodGroupSpec{
					Queue: "testQueue3",
				},
			},
		}
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "queues") {
				val, err := json.Marshal(queues)
				if err == nil {
					w.Write(val)
				}
			} else if strings.HasSuffix(r.URL.Path, "podgroups") {
				val, err := json.Marshal(podGroups)
				if err == nil {
					w.Write(val)
				}
			}
		})
		return httptest.NewServer(handler)
	}

	InitListFlags(&cobra.Command{})
	server := mockServer()
	defer server.Close()

	// Capture stdout
	originStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Create pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = originStdout
		w.Close()
	}()

	listQueueFlags.CommonFlags = getCommonFlags(server.URL)

	expectOutput := `Name                     Weight  State   Parent  Inqueue Pending Running Unknown Completed
testQueue1               0                       0       0       0       0       0       
testQueue2               0                       0       0       0       0       0`
	err = ListQueue(context.TODO())
	if err != nil {
		t.Errorf("List queue failed: %v", err)
		return
	}
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Read list queue output failed: %v", err)
	}
	result := strings.TrimSpace(string(out))

	if expectOutput != result {
		t.Errorf("(Test list queues with residual podgroups failed): \nExpect:\n%vGot:\n%v", expectOutput, result)
	}
}
