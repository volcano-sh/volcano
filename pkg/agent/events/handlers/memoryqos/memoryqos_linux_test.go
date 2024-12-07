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

package memoryqos

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func TestMemroyQoSHandle_Handle(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTempCgroup")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)

	testCases := []struct {
		name             string
		cgroupMgr        cgroup.CgroupManager
		cgroupSubpath    string
		event            framework.PodEvent
		expectedErr      bool
		expectedQoSLevel string
	}{
		{
			name:          "Burstable pod event && CgroupDriver=Cgroupfs",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/memory/kubepods/burstable",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000001",
				QoSLevel: -1,
				QoSClass: "Burstable",
			},
			expectedErr:      false,
			expectedQoSLevel: "-1",
		},

		{
			name:          "Guaranteed pod event && CgroupDriver=Cgroupfs",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/memory/kubepods",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000002",
				QoSLevel: -1,
				QoSClass: "Guaranteed",
			},
			expectedErr:      false,
			expectedQoSLevel: "-1",
		},

		{
			name:          "BestEffort pod event && CgroupDriver=Cgroupfs",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/memory/kubepods/besteffort",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000003",
				QoSLevel: -1,
				QoSClass: "BestEffort",
			},
			expectedErr:      false,
			expectedQoSLevel: "-1",
		},
		{
			name:          "BestEffort pod event && CgroupDriver=Cgroupfs qos level=-2",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/memory/kubepods/besteffort",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000003",
				QoSLevel: -2,
				QoSClass: "BestEffort",
			},
			expectedErr:      false,
			expectedQoSLevel: "-1",
		},
		{
			name:          "BestEffort pod event && CgroupDriver=Cgroupfs qos level=2",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/memory/kubepods/besteffort",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000003",
				QoSLevel: 2,
				QoSClass: "BestEffort",
			},
			expectedErr:      false,
			expectedQoSLevel: "0",
		},
	}

	for _, tc := range testCases {
		fakeCgroupPath := path.Join(dir, tc.cgroupSubpath, "pod"+string(tc.event.UID))
		err = os.MkdirAll(fakeCgroupPath, 0750)
		assert.Equal(t, err == nil, true, tc.name)

		tmpFile := path.Join(fakeCgroupPath, "memory.qos_level")
		if err = os.WriteFile(tmpFile, []byte("0"), 0660); err != nil {
			assert.Equal(t, nil, err, tc.name)
		}

		h := NewMemoryQoSHandle(nil, nil, tc.cgroupMgr)
		handleErr := h.Handle(tc.event)
		assert.Equal(t, tc.expectedErr, handleErr != nil, tc.name)

		actualLevel, readErr := os.ReadFile(tmpFile)
		assert.Equal(t, nil, readErr, tc.name)
		assert.Equal(t, tc.expectedQoSLevel, string(actualLevel), tc.name)
	}
}
