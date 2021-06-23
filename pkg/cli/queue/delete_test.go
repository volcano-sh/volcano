/*
Copyright 2017 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeleteQueue(t *testing.T) {
	response := v1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-queue",
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	deleteQueueFlags.Master = server.URL
	testCases := []struct {
		Name        string
		QueueName   string
		ExpectValue error
	}{
		{
			Name:        "Normal Case Delete Queue Succeed",
			QueueName:   "normal-case",
			ExpectValue: nil,
		},
		{
			Name:        "Abnormal Case Delete Queue Failed For Name Not Specified",
			QueueName:   "",
			ExpectValue: fmt.Errorf("queue name must be specified"),
		},
	}

	for _, testCase := range testCases {
		deleteQueueFlags.Name = testCase.QueueName

		err := DeleteQueue()
		if false == reflect.DeepEqual(err, testCase.ExpectValue) {
			t.Errorf("Case '%s' failed, expected: '%v', got '%v'", testCase.Name, testCase.ExpectValue, err)
		}
	}
}

func TestInitDeleteFlags(t *testing.T) {
	var cmd cobra.Command
	InitDeleteFlags(&cmd)

	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
}
