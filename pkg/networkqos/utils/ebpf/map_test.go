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

package ebpf

import (
	"fmt"
	"os"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
)

func TestCreateAndPinArrayMap(t *testing.T) {
	createdMap, _ := ebpf.NewMap(&ebpf.MapSpec{})
	testCases := []struct {
		name          string
		dir           string
		mapName       string
		mkdirErr      error
		createdMap    *ebpf.Map
		createMapErr  error
		defaultKV     []ebpf.MapKV
		expectedError error
	}{
		{
			name:       "create map successfully",
			dir:        "/tmp/test",
			mapName:    "throttle_cfg",
			createdMap: createdMap,
		},

		{
			name:          "make dir failed",
			dir:           "/tmp/test",
			mapName:       "throttle_cfg",
			mkdirErr:      fmt.Errorf("mkdir failed"),
			expectedError: fmt.Errorf("mkdir failed"),
		},

		{
			name:          "new map failed",
			dir:           "/tmp/test",
			mapName:       "throttle_cfg",
			createMapErr:  fmt.Errorf("new map failed"),
			expectedError: fmt.Errorf("new map failed"),
		},
	}

	for _, tc := range testCases {
		patch := gomonkey.NewPatches()

		patch.ApplyFunc(os.MkdirAll, func(path string, perm os.FileMode) error {
			return tc.mkdirErr
		})
		patch.ApplyFunc(ebpf.NewMapWithOptions, func(spec *ebpf.MapSpec, opts ebpf.MapOptions) (*ebpf.Map, error) {
			return tc.createdMap, tc.createMapErr
		})

		actualErr := GetEbpfMap().CreateAndPinArrayMap(tc.dir, tc.mapName, tc.defaultKV)
		patch.Reset()
		assert.Equal(t, tc.expectedError, actualErr, tc.name, actualErr)
	}
}
