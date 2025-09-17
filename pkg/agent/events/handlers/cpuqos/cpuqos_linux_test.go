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
	"strings"
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

func TestCPUQoSHandle_Handle_CgroupV2(t *testing.T) {
	// Set environment variable to force cgroup v2 detection
	originalEnv := os.Getenv("VOLCANO_TEST_CGROUP_VERSION")
	os.Setenv("VOLCANO_TEST_CGROUP_VERSION", "v2")
	defer func() {
		if originalEnv == "" {
			os.Unsetenv("VOLCANO_TEST_CGROUP_VERSION")
		} else {
			os.Setenv("VOLCANO_TEST_CGROUP_VERSION", originalEnv)
		}
	}()

	tmpDir := t.TempDir()
	tests := []struct {
		name      string
		event     framework.PodEvent
		prepare   func()
		post      func() map[string]string
		wantErr   bool
		wantVal   map[string]string
	}{
		{
			name: "cgroup v2: set QoS level -1 (highest priority) sets cpu.idle",
			event: framework.PodEvent{
				UID:      "fake-id1",
				QoSLevel: -1,
				QoSClass: "Guaranteed",
			},
			prepare: func() {
				prepareV2QoS(t, tmpDir, "fake-id1", []qosInfoV2{
					{path: cgroup.CPUIdleFileV2, value: "0"},
				})
			},
			post: func() map[string]string {
				return readFilesContent([]string{
					path.Join(tmpDir, "kubepods/podfake-id1/cpu.idle"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				path.Join(tmpDir, "kubepods/podfake-id1/cpu.idle"): "1",
			},
		},
		{
			name: "cgroup v2: set QoS level 0 sets cpu.weight to 100",
			event: framework.PodEvent{
				UID:      "fake-id2",
				QoSLevel: 0,
				QoSClass: "BestEffort",
			},
			prepare: func() {
				prepareV2QoS(t, tmpDir, "fake-id2", []qosInfoV2{
					{path: cgroup.CPUWeightFileV2, value: "100"},
				})
			},
			post: func() map[string]string {
				return readFilesContent([]string{
					path.Join(tmpDir, "kubepods/besteffort/podfake-id2/cpu.weight"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				path.Join(tmpDir, "kubepods/besteffort/podfake-id2/cpu.weight"): "100",
			},
		},
		{
			name: "cgroup v2: set QoS level 2 sets cpu.weight to 1000 (no quota because it returns 0)",
			event: framework.PodEvent{
				UID:      "fake-id3",
				QoSLevel: 2,
				QoSClass: "Guaranteed",
			},
			prepare: func() {
				prepareV2QoS(t, tmpDir, "fake-id3", []qosInfoV2{
					{path: cgroup.CPUWeightFileV2, value: "100"},
				})
			},
			post: func() map[string]string {
				return readFilesContent([]string{
					path.Join(tmpDir, "kubepods/podfake-id3/cpu.weight"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				path.Join(tmpDir, "kubepods/podfake-id3/cpu.weight"): "1000",
			},
		},
		{
			name: "cgroup v2: cgroup path not exists, return no error",
			event: framework.PodEvent{
				UID:      "fake-id-nonexistent",
				QoSLevel: 1,
				QoSClass: "Burstable",
			},
			prepare:   func() {}, // No preparation, path doesn't exist
			post:      func() map[string]string { return nil },
			wantErr:   false,
			wantVal:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use real CgroupManager - it will detect v2 due to environment variable
			cgroupMgr := cgroup.NewCgroupManager("cgroupfs", tmpDir, "")
			assert.NotNil(t, cgroupMgr)
			assert.Equal(t, cgroup.CgroupV2, cgroupMgr.GetCgroupVersion())

			h := &CPUQoSHandle{
				cgroupMgr: cgroupMgr,
			}
			if tt.prepare != nil {
				tt.prepare()
			}
			if err := h.Handle(tt.event); (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.post != nil {
				actual := tt.post()
				if tt.wantVal != nil {
					assert.Equal(t, tt.wantVal, actual)
				}
			}
		})
	}
}

type qosInfoV2 struct {
	path  string
	value string
}

func prepareV2QoS(t *testing.T, tmpDir, podUID string, infos []qosInfoV2) {
	for _, info := range infos {
		// Create the proper cgroup path structure that matches the manager
		// For cgroup v2, the path should be: tmpDir/kubepods/[qos]/podUID/
		// We'll create all possible QoS paths since the test will determine which one to use
		qosPaths := []string{
			path.Join(tmpDir, "kubepods", "pod"+podUID),             // Guaranteed
			path.Join(tmpDir, "kubepods", "burstable", "pod"+podUID), // Burstable  
			path.Join(tmpDir, "kubepods", "besteffort", "pod"+podUID), // BestEffort
		}
		
		for _, qosPath := range qosPaths {
			err := os.MkdirAll(qosPath, 0644)
			assert.NoError(t, err)
			filePath := path.Join(qosPath, info.path)
			f, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
			if err == nil { // Only create if no error
				err = f.Chmod(0600)
				assert.NoError(t, err)
				_, err = f.WriteString(info.value)
				assert.NoError(t, err)
				err = f.Close()
				assert.NoError(t, err)
			}
		}
	}
}

func readFilesContent(filePaths []string) map[string]string {
	result := make(map[string]string)
	for _, filePath := range filePaths {
		content, err := os.ReadFile(filePath)
		if err == nil {
			result[filePath] = strings.TrimSpace(string(content))
		}
	}
	return result
}
