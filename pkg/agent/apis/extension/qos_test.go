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

package extension

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetQosLevel(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want int
	}{
		{
			name: "no qos level specified",
			pod:  makePod(""),
			want: 0,
		},
		{
			name: "invalid qos level",
			pod:  makePod("invalid"),
			want: 0,
		},
		{
			name: "qos LC",
			pod:  makePod("LC"),
			want: 2,
		},
		{
			name: "qos HLS",
			pod:  makePod("HLS"),
			want: 2,
		},
		{
			name: "qos LS",
			pod:  makePod("LS"),
			want: 1,
		},
		{
			name: "qos BE",
			pod:  makePod("BE"),
			want: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetQosLevel(tt.pod); got != tt.want {
				t.Errorf("GetQosLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func makePod(qosLevel string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"volcano.sh/qos-level": qosLevel,
			},
		},
	}
}
