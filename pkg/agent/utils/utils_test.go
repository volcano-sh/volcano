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

package utils

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdatePodCgroup(t *testing.T) {
	tmp, err := os.MkdirTemp("", "cgroup")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = os.RemoveAll(tmp)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", tmp, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)

	testCases := []struct {
		name        string
		files       []string
		content     []byte
		expectedErr bool
	}{
		{
			name: "memory-qos",
			files: []string{
				"pod00000000-1111-2222-3333-000000000001/memory.qos_level",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000001/memory.qos_level",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000002/memory.qos_level",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000003/memory.qos_level",
			},
			content:     []byte("-1"),
			expectedErr: false,
		},

		{
			name: "cpu-qos",
			files: []string{
				"pod00000000-1111-2222-3333-000000000001/cpu.qos_level",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000001/cpu.qos_level",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000002/cpu.qos_level",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000003/cpu.qos_level",
			},
			content:     []byte("-1"),
			expectedErr: false,
		},

		{
			name: "network-qos",
			files: []string{
				"pod00000000-1111-2222-3333-000000000001/net_cls.classid",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000001/net_cls.classid",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000002/net_cls.classid",
				"pod00000000-1111-2222-3333-000000000001/c0000000000000000000000000003/net_cls.classid",
			},
			content:     []byte("4294967295"),
			expectedErr: false,
		},
	}
	for _, tc := range testCases {
		for _, f := range tc.files {
			mkErr := os.MkdirAll(path.Join(tmp, path.Dir(f)), 0750)
			if mkErr != nil {
				assert.Equal(t, nil, mkErr, tc.name)
			}
			writeErr := os.WriteFile(path.Join(tmp, f), []byte("0"), 0660)
			if writeErr != nil {
				assert.Equal(t, nil, writeErr, tc.name)
			}
		}

		actualErr := UpdatePodCgroup(path.Join(tmp, tc.files[0]), tc.content)
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name)

		for _, f := range tc.files {
			actualContent, readErr := os.ReadFile(path.Join(path.Join(tmp, f)))
			assert.Equal(t, nil, readErr, tc.name)
			assert.Equal(t, tc.content, actualContent, tc.name)
		}
	}
}

func TestGetOSReleaseFromFile(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTemp")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)

	testCases := []struct {
		name              string
		content           string
		expectedOSRelease *OSRelease
		expectedErr       bool
	}{
		{
			name: "openEuler && x86",
			content: `
NAME="openEuler"
VERSION="22.03 (LTS-SP2)"
ID="openEuler"
VERSION_ID="22.03"
PRETTY_NAME="openEuler 22.03 (LTS-SP2)"
ANSI_COLOR="0;31"
            `,
			expectedErr: false,
			expectedOSRelease: &OSRelease{
				Name:      "openEuler",
				Version:   "22.03 (LTS-SP2)",
				ID:        "openEuler",
				VersionID: "22.03",
			},
		},
	}

	for _, tc := range testCases {
		tmpFile := path.Join(dir, "os-release")
		if err = os.WriteFile(tmpFile, []byte(tc.content), 0660); err != nil {
			assert.Equal(t, nil, err)
		}
		actualOSRelease, actualErr := GetOSReleaseFromFile(tmpFile)
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name)
		assert.Equal(t, tc.expectedOSRelease, actualOSRelease, tc.name)
	}

}

func TestGetCPUManagerPolicy(t *testing.T) {
	dir, err := os.MkdirTemp("", "cpuManagerPolicy")
	assert.NoError(t, err)
	defer func() {
		err = os.RemoveAll(dir)
		assert.NoError(t, err)
	}()

	tests := []struct {
		name    string
		want    string
		prepare func()
	}{
		{
			name: "file not exist",
			want: "",
			prepare: func() {
				err = os.Setenv(kubeletRootDirEnv, dir)
				assert.NoError(t, err)
			},
		},
		{
			name: "read correctly",
			want: "static",
			prepare: func() {
				err = os.Setenv(kubeletRootDirEnv, dir)
				assert.NoError(t, err)
				b := []byte(`{"policyName":"static","defaultCpuSet":"0-1","checksum":1636926438}`)
				err = os.WriteFile(path.Join(dir, cpuManagerState), b, 0600)
				assert.NoError(t, err)
			},
		},
	}
	for _, tt := range tests {
		if tt.prepare != nil {
			tt.prepare()
		}
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCPUManagerPolicy(); got != tt.want {
				t.Errorf("GetCPUManagerPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}
