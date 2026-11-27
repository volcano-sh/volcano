/*
Copyright 2026 The Volcano Authors.

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

package vqueues

import (
	"bytes"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestPrintQueue(t *testing.T) {
	testQueue := &v1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-queue",
		},
		Spec: v1beta1.QueueSpec{
			Weight: 5,
		},
		Status: v1beta1.QueueStatus{
			State:   v1beta1.QueueStateOpen,
			Inqueue: 1,
			Pending: 2,
			Running: 3,
			Unknown: 0,
		},
	}

	tests := []struct {
		name          string
		queue         *v1beta1.Queue
		hasWriter     bool
		expectedTexts []string
	}{
		{
			name:      "Print valid queue",
			queue:     testQueue,
			hasWriter: true,
			expectedTexts: []string{
				Name, Weight, State, Inqueue, Pending, Running, Unknown,
				"test-queue", "5", string(v1beta1.QueueStateOpen), "1", "2", "3", "0",
			},
		},
		{
			name:      "Nil queue should not panic or write",
			queue:     nil,
			hasWriter: true,
		},
		{
			name:      "Nil writer should not panic",
			queue:     testQueue,
			hasWriter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if tt.hasWriter {
				PrintQueue(tt.queue, &buf)
			} else {
				PrintQueue(tt.queue, nil)
				return
			}

			output := buf.String()
			for _, expected := range tt.expectedTexts {
				if !strings.Contains(output, expected) {
					t.Errorf("PrintQueue() output = %q, expected to contain %q", output, expected)
				}
			}
		})
	}
}

func TestPrintQueues(t *testing.T) {
	testQueues := &v1beta1.QueueList{
		Items: []v1beta1.Queue{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "queue-1"},
				Spec:       v1beta1.QueueSpec{Weight: 1},
				Status: v1beta1.QueueStatus{
					State:   v1beta1.QueueStateOpen,
					Inqueue: 2,
					Pending: 1,
					Running: 4,
					Unknown: 0,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "queue-2"},
				Spec:       v1beta1.QueueSpec{Weight: 2},
				Status: v1beta1.QueueStatus{
					State:   v1beta1.QueueStateClosed,
					Inqueue: 0,
					Pending: 0,
					Running: 1,
					Unknown: 1,
				},
			},
		},
	}

	tests := []struct {
		name          string
		queues        *v1beta1.QueueList
		hasWriter     bool
		expectedTexts []string
	}{
		{
			name:      "Print valid queue list",
			queues:    testQueues,
			hasWriter: true,
			expectedTexts: []string{
				Name, Weight, State, Inqueue, Pending, Running, Unknown,
				"queue-1", "1", string(v1beta1.QueueStateOpen), "2", "4",
				"queue-2", "2", string(v1beta1.QueueStateClosed), "1",
			},
		},
		{
			name:      "Print empty queue list should print header only",
			queues:    &v1beta1.QueueList{Items: []v1beta1.Queue{}},
			hasWriter: true,
			expectedTexts: []string{
				Name, Weight, State, Inqueue, Pending, Running, Unknown,
			},
		},
		{
			name:      "Nil queue list should not panic or write",
			queues:    nil,
			hasWriter: true,
		},
		{
			name:      "Nil writer should not panic",
			queues:    testQueues,
			hasWriter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if tt.hasWriter {
				PrintQueues(tt.queues, &buf)
			} else {
				PrintQueues(tt.queues, nil)
				return
			}

			output := buf.String()
			for _, expected := range tt.expectedTexts {
				if !strings.Contains(output, expected) {
					t.Errorf("PrintQueues() output = %q, expected to contain %q", output, expected)
				}
			}
		})
	}
}
