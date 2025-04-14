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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/cli/podgroup"
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

func getCommonFlags(master string) commonFlags {
	return commonFlags{
		Master: master,
	}
}

func TestCreateQueue(t *testing.T) {
	InitCreateFlags(&cobra.Command{})
	server := getTestQueueHTTPServer(t)
	defer server.Close()

	createQueueFlags.commonFlags = getCommonFlags(server.URL)
	createQueueFlags.Name = "testQueue"
	createQueueFlags.Weight = int32(2)

	testCases := []struct {
		Name        string
		ExpectValue error
	}{
		{
			Name:        "CreateQueue",
			ExpectValue: nil,
		},
	}
	for _, testcase := range testCases {
		err := CreateQueue(context.TODO())
		if err != nil {
			t.Errorf("(%s): expected: %v, got %v ", testcase.Name, testcase.ExpectValue, err)
		}
	}
}

func TestGetQueue(t *testing.T) {
	InitGetFlags(&cobra.Command{})
	server := getTestQueueHTTPServer(t)
	defer server.Close()

	getQueueFlags.commonFlags = getCommonFlags(server.URL)

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

	listQueueFlags.commonFlags = getCommonFlags(server.URL)

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

	listQueueFlags.commonFlags = getCommonFlags(server.URL)

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
