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
	queue := &v1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
		Spec:       v1beta1.QueueSpec{Weight: 5},
		Status: v1beta1.QueueStatus{
			State:   v1beta1.QueueStateOpen,
			Inqueue: 1,
			Pending: 2,
			Running: 3,
			Unknown: 0,
		},
	}

	var buf bytes.Buffer
	PrintQueue(queue, &buf)

	out := buf.String()
	for _, want := range []string{"test-queue", "Open", Name, Weight, State, Inqueue, Pending, Running, Unknown} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintQueue() output missing %q, got %q", want, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("PrintQueue() should print exactly a header and a data line, got %d lines: %q", len(lines), out)
	}
}

func TestPrintQueues(t *testing.T) {
	queues := &v1beta1.QueueList{
		Items: []v1beta1.Queue{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "queue-a"},
				Spec:       v1beta1.QueueSpec{Weight: 1},
				Status:     v1beta1.QueueStatus{State: v1beta1.QueueStateOpen, Running: 2},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "queue-b"},
				Spec:       v1beta1.QueueSpec{Weight: 2},
				Status:     v1beta1.QueueStatus{State: v1beta1.QueueStateClosed, Pending: 4},
			},
		},
	}

	var buf bytes.Buffer
	PrintQueues(queues, &buf)

	out := buf.String()
	for _, want := range []string{"queue-a", "queue-b", "Open", "Closed"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintQueues() output missing %q, got %q", want, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("PrintQueues() should print a header plus one line per queue, got %d lines: %q", len(lines), out)
	}
}

func TestPrintQueuesEmpty(t *testing.T) {
	queues := &v1beta1.QueueList{}

	var buf bytes.Buffer
	PrintQueues(queues, &buf)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("PrintQueues() with no queues should print only the header, got %d lines: %q", len(lines), buf.String())
	}
}
