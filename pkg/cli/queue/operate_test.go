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

func TestOperateQueue(t *testing.T) {
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

	operateQueueFlags.Master = server.URL
	testCases := []struct {
		Name        string
		QueueName   string
		Weight      int32
		Action      string
		ExpectValue error
	}{
		{
			Name:        "Normal Case Operate Queue Succeed, Action close",
			QueueName:   "normal-case-action-close",
			Action:      ActionClose,
			ExpectValue: nil,
		},
		{
			Name:        "Normal Case Operate Queue Succeed, Action open",
			QueueName:   "normal-case-action-open",
			Action:      ActionOpen,
			ExpectValue: nil,
		},
		{
			Name:        "Normal Case Operate Queue Succeed, Update Weight",
			QueueName:   "normal-case-update-weight",
			Action:      ActionUpdate,
			Weight:      3,
			ExpectValue: nil,
		},
		{
			Name:      "Abnormal Case Update Queue Failed For Invalid Weight",
			QueueName: "abnormal-case-invalid-weight",
			Action:    ActionUpdate,
			ExpectValue: fmt.Errorf("when %s queue %s, weight must be specified, "+
				"the value must be greater than 0", ActionUpdate, "abnormal-case-invalid-weight"),
		},
		{
			Name:        "Abnormal Case Operate Queue Failed For Name Not Specified",
			QueueName:   "",
			ExpectValue: fmt.Errorf("queue name must be specified"),
		},
		{
			Name:        "Abnormal Case Operate Queue Failed For Action null",
			QueueName:   "abnormal-case-null-action",
			Action:      "",
			ExpectValue: fmt.Errorf("action can not be null"),
		},
		{
			Name:      "Abnormal Case Operate Queue Failed For Action Invalid",
			QueueName: "abnormal-case-invalid-action",
			Action:    "invalid",
			ExpectValue: fmt.Errorf("action %s invalid, valid actions are %s, %s and %s",
				"invalid", ActionOpen, ActionClose, ActionUpdate),
		},
	}

	for _, testCase := range testCases {
		operateQueueFlags.Name = testCase.QueueName
		operateQueueFlags.Action = testCase.Action
		operateQueueFlags.Weight = testCase.Weight

		err := OperateQueue()
		if false == reflect.DeepEqual(err, testCase.ExpectValue) {
			t.Errorf("Case '%s' failed, expected: '%v', got '%v'", testCase.Name, testCase.ExpectValue, err)
		}
	}
}

func TestInitOperateFlags(t *testing.T) {
	var cmd cobra.Command
	InitOperateFlags(&cmd)

	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("weight") == nil {
		t.Errorf("Could not find the flag weight")
	}
	if cmd.Flag("action") == nil {
		t.Errorf("Could not find the flag action")
	}
}
