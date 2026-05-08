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

package cpuqos

import (
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func TestCPUQoSHandle_Handle(t *testing.T) {
	// make a fake pod cgroup path first.
	tmpDir := t.TempDir()
	dir := path.Join(tmpDir, "cpu", "kubepods", "podfake-id2")
	err := os.MkdirAll(dir, 0644)
	assert.NoError(t, err)
	filePath := path.Join(dir, "cpu.qos_level")
	_, err = os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	assert.NoError(t, err)

	tests := []struct {
		name      string
		cgroupMgr cgroup.CgroupManager
		event     framework.PodEvent
		post      func() int64
		wantErr   bool
	}{
		{
			name:      "cgroup path not exits, return no err",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: -1,
				QoSClass: "Guaranteed",
			},
			wantErr: false,
		},
		{
			name:      "update cgroup path successfully, return no err",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event: framework.PodEvent{
				UID:      "fake-id2",
				QoSLevel: -1,
				QoSClass: "Guaranteed",
			},
			post: func() int64 {
				value, _ := os.ReadFile(filePath)
				i, _ := strconv.Atoi(string(value))
				return int64(i)
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &CPUQoSHandle{
				cgroupMgr: tt.cgroupMgr,
			}
			if err := h.Handle(tt.event); (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.post != nil {
				assert.Equal(t, tt.event.QoSLevel, tt.post())
			}
		})
	}
}
