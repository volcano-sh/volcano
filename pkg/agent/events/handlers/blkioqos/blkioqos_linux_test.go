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

package blkioqos

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func TestBlkioQoSHandle_Handle(t *testing.T) {
	// Test cgroup v1
	tmpDirV1 := t.TempDir()
	dirV1 := path.Join(tmpDirV1, "blkio", "kubepods", "podfake-id1")
	err := os.MkdirAll(dirV1, 0755)
	assert.NoError(t, err)
	filePathV1 := path.Join(dirV1, "blkio.weight")
	_, err = os.OpenFile(filePathV1, os.O_RDWR|os.O_CREATE, 0644)
	assert.NoError(t, err)

	// Test cgroup v2
	tmpDirV2 := t.TempDir()
	dirV2 := path.Join(tmpDirV2, "kubepods", "podfake-id2")
	err = os.MkdirAll(dirV2, 0755)
	assert.NoError(t, err)
	filePathV2 := path.Join(dirV2, "io.weight")
	_, err = os.OpenFile(filePathV2, os.O_RDWR|os.O_CREATE, 0644)
	assert.NoError(t, err)

	tests := []struct {
		name        string
		cgroupMgr   cgroup.CgroupManager
		event       framework.PodEvent
		post        func() string
		wantErr     bool
		expectValue string
	}{
		{
			name:      "cgroup v1: no annotation, skip",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDirV1, ""),
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "cgroup v1: valid weight annotation",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDirV1, ""),
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Annotations: map[string]string{
							apis.BlkioWeightAnnotationKey: "500",
						},
					},
				},
			},
			post: func() string {
				value, _ := os.ReadFile(filePathV1)
				return string(value)
			},
			wantErr:     false,
			expectValue: "500",
		},
		{
			name:      "cgroup v1: weight below minimum, clamped to 10",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDirV1, ""),
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Annotations: map[string]string{
							apis.BlkioWeightAnnotationKey: "5",
						},
					},
				},
			},
			post: func() string {
				value, _ := os.ReadFile(filePathV1)
				return string(value)
			},
			wantErr:     false,
			expectValue: "10",
		},
		{
			name:      "cgroup v1: weight above maximum, clamped to 1000",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDirV1, ""),
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Annotations: map[string]string{
							apis.BlkioWeightAnnotationKey: "2000",
						},
					},
				},
			},
			post: func() string {
				value, _ := os.ReadFile(filePathV1)
				return string(value)
			},
			wantErr:     false,
			expectValue: "1000",
		},
		{
			name:      "cgroup v2: valid weight annotation, converted to v2 range",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDirV2, ""),
			event: framework.PodEvent{
				UID:      "fake-id2",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Annotations: map[string]string{
							apis.BlkioWeightAnnotationKey: "500",
						},
					},
				},
			},
			post: func() string {
				value, _ := os.ReadFile(filePathV2)
				return string(value)
			},
			wantErr:     false,
			expectValue: "5000",
		},
		{
			name:      "invalid annotation value, skip",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDirV1, ""),
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Annotations: map[string]string{
							apis.BlkioWeightAnnotationKey: "invalid",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "cgroup v2: valid weight annotation, converted to v2 range" {
				os.Setenv("VOLCANO_TEST_CGROUP_VERSION", "v2")
				defer os.Unsetenv("VOLCANO_TEST_CGROUP_VERSION")
			} else {
				os.Setenv("VOLCANO_TEST_CGROUP_VERSION", "v1")
				defer os.Unsetenv("VOLCANO_TEST_CGROUP_VERSION")
			}

			h := &BlkioQoSHandle{
				cgroupMgr: tt.cgroupMgr,
			}
			if err := h.Handle(tt.event); (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.post != nil && tt.expectValue != "" {
				actualValue := tt.post()
				assert.Equal(t, tt.expectValue, actualValue, "Expected cgroup file to contain %s, got %s", tt.expectValue, actualValue)
			}
		})
	}
}