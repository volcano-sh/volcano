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

package api

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ignoredDevicesList_Set_BasicUsage(t *testing.T) {
	tests := []struct {
		name                   string
		deviceLists            [][]string
		expectedIgnoredDevices []string
	}{
		{
			name:                   "set several values to ignoredDevicesList",
			deviceLists:            [][]string{{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"}},
			expectedIgnoredDevices: []string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"},
		},
		{
			name:                   "set several lists of values to ignoredDevicesList atomically",
			deviceLists:            [][]string{{"volcano.sh/vgpu-memory"}, {"volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"}},
			expectedIgnoredDevices: []string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"},
		},
		{
			name:                   "possible way to clear ignoredDevicesList",
			deviceLists:            nil,
			expectedIgnoredDevices: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lst := ignoredDevicesList{}
			lst.Set(tt.deviceLists...)
			assert.Equal(t, tt.expectedIgnoredDevices, lst.ignoredDevices)
		})
	}
}

func Test_ignoredDevicesList_Range_BasicUsage(t *testing.T) {
	lst := ignoredDevicesList{}
	lst.Set([]string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"})

	t.Run("read and copy values from the ignoredDevicesList", func(t *testing.T) {
		ignoredDevices := make([]string, 0, len(lst.ignoredDevices))
		lst.Range(func(_ int, device string) bool {
			ignoredDevices = append(ignoredDevices, device)
			return true
		})
		assert.Equal(t, lst.ignoredDevices, ignoredDevices)
	})

	t.Run("break iteration through the ignoredDevicesList", func(t *testing.T) {
		i := 0
		flag := false
		lst.Range(func(_ int, device string) bool {
			i++
			if lst.ignoredDevices[1] == device {
				flag = true
				return false
			}
			return true
		})

		assert.Equal(t, true, flag)
		assert.Equal(t, 2, i)
	})
}

func Test_ignoredDevicesList_Set_Concurrent(t *testing.T) {
	lst := ignoredDevicesList{}
	expected := []string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"}

	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			lst.Set(expected)
		}()
	}
	wg.Wait()

	assert.Equal(t, expected, lst.ignoredDevices)
}

func Test_ignoredDevicesList_Range_Concurrent(t *testing.T) {
	lst := ignoredDevicesList{}
	lst.Set([]string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"})

	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			ignoredDevices := make([]string, 0, len(lst.ignoredDevices))
			lst.Range(func(_ int, device string) bool {
				ignoredDevices = append(ignoredDevices, device)
				return true
			})
			assert.Equal(t, ignoredDevices, lst.ignoredDevices)
		}()
	}
	wg.Wait()
}

func Test_ignoredDevicesList_NoRace(t *testing.T) {
	lst := ignoredDevicesList{}

	var wg sync.WaitGroup
	wg.Add(16)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			lst.Set([]string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-memory-percentage", "volcano.sh/vgpu-cores"})
		}()
		go func() {
			defer wg.Done()
			lst.Range(func(_ int, _ string) bool {
				return true
			})
		}()
	}
	wg.Wait()
}
