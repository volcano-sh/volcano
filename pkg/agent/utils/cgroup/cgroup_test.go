/*
Copyright 2025 The Volcano Authors.

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

package cgroup

import (
	"testing"
)

func TestBuildContainerCgroupName(t *testing.T) {
	tests := []struct {
		name         string
		cgroupDriver string
		containerID  string
		expected     string
	}{
		{
			name:         "cgroupfs with containerd",
			cgroupDriver: CgroupDriverCgroupfs,
			containerID:  "containerd://abc123",
			expected:     "abc123",
		},
		{
			name:         "cgroupfs with docker",
			cgroupDriver: CgroupDriverCgroupfs,
			containerID:  "docker://xyz789",
			expected:     "xyz789",
		},
		{
			name:         "cgroupfs with pure ID",
			cgroupDriver: CgroupDriverCgroupfs,
			containerID:  "abc123",
			expected:     "abc123",
		},
		{
			name:         "systemd with containerd",
			cgroupDriver: CgroupDriverSystemd,
			containerID:  "containerd://abc123",
			expected:     "cri-containerd-abc123.scope",
		},
		{
			name:         "systemd with docker",
			cgroupDriver: CgroupDriverSystemd,
			containerID:  "docker://xyz789",
			expected:     "docker-xyz789.scope",
		},
		{
			name:         "systemd with cri-o",
			cgroupDriver: CgroupDriverSystemd,
			containerID:  "cri-o://def456",
			expected:     "crio-def456.scope",
		},
		{
			name:         "systemd with pure ID",
			cgroupDriver: CgroupDriverSystemd,
			containerID:  "abc123",
			expected:     "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &CgroupManagerImpl{
				cgroupDriver: tt.cgroupDriver,
			}
			got := mgr.BuildContainerCgroupName(tt.containerID)
			if got != tt.expected {
				t.Errorf("BuildContainerCgroupName() = %v, want %v", got, tt.expected)
			}
		})
	}
}
