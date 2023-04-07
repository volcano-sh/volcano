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

package job

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestViewJob(t *testing.T) {
	response := v1alpha1.Job{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testJobWithLongLongLongName",
			Labels: map[string]string{
				"LabelWithLongLongLongLongName": "LongLongLongLongLabelValue",
			},
			Annotations: map[string]string{
				"AnnotationWithLongLongLongLongName": "LongLongLongLongAnnotationValue",
			},
		},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:     "taskWithLongLongLongLongName",
					Replicas: math.MaxInt32,
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Command: []string{"echo", "123"},
									Ports: []v1.ContainerPort{
										{
											Name: "placeholder",
										},
									},
								},
							},
							ImagePullSecrets: []v1.LocalObjectReference{
								{
									Name: "imagepull-secret",
								},
							},
						},
					},
				},
			},
		},
		Status: v1alpha1.JobStatus{
			Succeeded:    1,
			Pending:      3,
			Running:      1,
			Failed:       2,
			Unknown:      10,
			Terminating:  4,
			RetryCount:   5,
			MinAvailable: 6,
			Version:      7,
			ControlledResources: map[string]string{
				"svc": "",
			},
		},
	}

	eventList := v1.EventList{
		Items: []v1.Event{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: response.Name + ".123",
				},
				Count: 1,
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: response.Name + ".456",
				},
				Count:          2,
				FirstTimestamp: metav1.Now(),
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.String(), "job") {
			val, err := json.Marshal(response)
			if err == nil {
				w.Write(val)
			}
			return
		}

		val, err := json.Marshal(eventList)
		if err == nil {
			w.Write(val)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	viewJobFlags.Master = server.URL
	viewJobFlags.Namespace = "test"
	viewJobFlags.JobName = "testJob"

	testCases := []struct {
		Name        string
		ExpectValue error
	}{
		{
			Name:        "viewJob",
			ExpectValue: nil,
		},
	}

	for i, testcase := range testCases {
		err := ViewJob()
		if err != nil {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, err)
		}
	}

}

func TestInitViewFlags(t *testing.T) {
	var cmd cobra.Command
	InitViewFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}

}
