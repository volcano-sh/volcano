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

package vjobs

import (
	"bytes"
	"strings"
	"testing"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestWriteLine(t *testing.T) {
	tests := []struct {
		name    string
		spaces  int
		content string
		params  []interface{}
		want    string
	}{
		{
			name:    "no indent",
			spaces:  0,
			content: "%s\n",
			params:  []interface{}{"hello"},
			want:    "hello\n",
		},
		{
			name:    "single indent",
			spaces:  1,
			content: "%s\n",
			params:  []interface{}{"hello"},
			want:    "  hello\n",
		},
		{
			name:    "nested indent",
			spaces:  3,
			content: "%s\n",
			params:  []interface{}{"hello"},
			want:    "      hello\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			WriteLine(&buf, tt.spaces, tt.content, tt.params...)
			if buf.String() != tt.want {
				t.Errorf("WriteLine() = %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestGetMaxLen(t *testing.T) {
	jobs := &v1alpha1.JobList{
		Items: []v1alpha1.Job{
			{ObjectMeta: metav1.ObjectMeta{Name: "short", Namespace: "ns"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "a-much-longer-job-name", Namespace: "a-longer-namespace"}},
		},
	}

	maxLenInfo := getMaxLen(jobs)

	wantNameLen := len("a-much-longer-job-name") + 3
	wantNamespaceLen := len("a-longer-namespace") + 3
	if maxLenInfo[0] != wantNameLen {
		t.Errorf("getMaxLen() name len = %d, want %d", maxLenInfo[0], wantNameLen)
	}
	if maxLenInfo[1] != wantNamespaceLen {
		t.Errorf("getMaxLen() namespace len = %d, want %d", maxLenInfo[1], wantNamespaceLen)
	}
}

func TestGetMaxLenShorterThanHeader(t *testing.T) {
	jobs := &v1alpha1.JobList{
		Items: []v1alpha1.Job{
			{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "b"}},
		},
	}

	maxLenInfo := getMaxLen(jobs)

	if maxLenInfo[0] != len(Name)+3 {
		t.Errorf("getMaxLen() name len = %d, want %d", maxLenInfo[0], len(Name)+3)
	}
	if maxLenInfo[1] != len(Namespace)+3 {
		t.Errorf("getMaxLen() namespace len = %d, want %d", maxLenInfo[1], len(Namespace)+3)
	}
}

func TestPrintJobs(t *testing.T) {
	jobs := &v1alpha1.JobList{
		Items: []v1alpha1.Job{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "job-a", Namespace: "default"},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks:         []v1alpha1.TaskSpec{{Replicas: 2}, {Replicas: 3}},
				},
				Status: v1alpha1.JobStatus{Running: 4, Pending: 1},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "job-b", Namespace: "default"},
				Spec:       v1alpha1.JobSpec{SchedulerName: "other-scheduler"},
			},
		},
	}

	var buf bytes.Buffer
	viewJobFlags.SchedulerName = "volcano"
	viewJobFlags.selector = ""
	viewJobFlags.allNamespace = false
	defer func() {
		viewJobFlags.SchedulerName = ""
	}()

	PrintJobs(jobs, &buf)

	out := buf.String()
	if !strings.Contains(out, "job-a") {
		t.Errorf("PrintJobs() output missing job-a, got %q", out)
	}
	if strings.Contains(out, "job-b") {
		t.Errorf("PrintJobs() output should have filtered out job-b by scheduler name, got %q", out)
	}
	if !strings.Contains(out, "Replicas") {
		t.Errorf("PrintJobs() output missing header, got %q", out)
	}
}

func TestPrintJobsSelectorFilter(t *testing.T) {
	jobs := &v1alpha1.JobList{
		Items: []v1alpha1.Job{
			{ObjectMeta: metav1.ObjectMeta{Name: "train-job", Namespace: "default"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "infer-job", Namespace: "default"}},
		},
	}

	var buf bytes.Buffer
	viewJobFlags.SchedulerName = ""
	viewJobFlags.selector = "train"
	viewJobFlags.allNamespace = false
	defer func() {
		viewJobFlags.selector = ""
	}()

	PrintJobs(jobs, &buf)

	out := buf.String()
	if !strings.Contains(out, "train-job") {
		t.Errorf("PrintJobs() output missing train-job, got %q", out)
	}
	if strings.Contains(out, "infer-job") {
		t.Errorf("PrintJobs() output should have filtered out infer-job, got %q", out)
	}
}

func TestPrintJobInfo(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
			Labels:    map[string]string{"app": "demo"},
		},
		Spec: v1alpha1.JobSpec{
			MinAvailable:  2,
			SchedulerName: "volcano",
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:     "worker",
					Replicas: 3,
					Template: coreV1.PodTemplateSpec{
						Spec: coreV1.PodSpec{
							Containers: []coreV1.Container{
								{Name: "main", Image: "busybox"},
							},
						},
					},
				},
			},
		},
		Status: v1alpha1.JobStatus{
			Running:   2,
			Succeeded: 1,
			State:     v1alpha1.JobState{Phase: v1alpha1.Running},
		},
	}

	var buf bytes.Buffer
	PrintJobInfo(job, &buf)

	out := buf.String()
	for _, want := range []string{"test-job", "app", "demo", "<none>", "worker", "busybox", "Running"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintJobInfo() output missing %q, got %q", want, out)
		}
	}
}

func TestPrintEvents(t *testing.T) {
	now := metav1.NewTime(time.Now())
	events := []coreV1.Event{
		{
			Type:           "Normal",
			Reason:         "Scheduled",
			Count:          3,
			FirstTimestamp: now,
			LastTimestamp:  now,
			Source:         coreV1.EventSource{Component: "volcano-scheduler", Host: "node-1"},
			Message:        "  pod scheduled  ",
		},
	}

	var buf bytes.Buffer
	PrintEvents(events, &buf)

	out := buf.String()
	for _, want := range []string{"Scheduled", "volcano-scheduler, node-1", "pod scheduled"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintEvents() output missing %q, got %q", want, out)
		}
	}
	if strings.Contains(out, "  pod scheduled  ") {
		t.Errorf("PrintEvents() should trim message whitespace, got %q", out)
	}
}

func TestPrintEventsEmpty(t *testing.T) {
	var buf bytes.Buffer
	PrintEvents(nil, &buf)

	if !strings.Contains(buf.String(), "<none>") {
		t.Errorf("PrintEvents() with no events should print <none>, got %q", buf.String())
	}
}
