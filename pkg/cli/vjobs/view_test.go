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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestPrintJobInfoTaskTemplateAnnotations(t *testing.T) {
	hardcoded := []string{"Cri . Cci . Io", "Kubernetes . Io / Availablezone", "Network . Alpha . Kubernetes . Io"}

	testCases := []struct {
		name        string
		annotations map[string]string
		want        []string
	}{
		{
			name: "prints the template annotations sorted by key",
			annotations: map[string]string{
				"sidecar.istio.io/inject": "false",
				"example.com/team":        "ml",
			},
			want: []string{"        Annotations:\n          example.com/team:\tml\n          sidecar.istio.io/inject:\tfalse\n"},
		},
		{
			name: "prints none when the template has no annotations",
			want: []string{"        Annotations:\t<none>\n"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "default"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{{
						Name:     "worker",
						Replicas: 1,
						Template: v1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations}},
					}},
				},
			}

			var buf bytes.Buffer
			PrintJobInfo(job, &buf)
			out := buf.String()

			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q:\n%s", want, out)
				}
			}
			for _, key := range hardcoded {
				if strings.Contains(out, key) {
					t.Errorf("output contains annotation %q that is not set on the template:\n%s", key, out)
				}
			}
		})
	}
}
