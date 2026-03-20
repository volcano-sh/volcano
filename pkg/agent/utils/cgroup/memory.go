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

package cgroup

import (
	"path/filepath"
	"strconv"
	"strings"

	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/file"
)

const (
	MemoryUnlimited = -1

	valueUnlimitedV1 = "-1"
	valueUnlimitedV2 = "max"

	MemoryMax  = "MemoryMax"
	MemoryHigh = "MemoryHigh"
	MemoryLow  = "MemoryLow"
	MemoryMin  = "MemoryMin"

	MemoryUsageFile    string = "memory.stat"
	MemoryQoSLevelFile string = "memory.qos_level"
	MemoryLimitFile    string = "memory.limit_in_bytes"

	MemoryUsageFileV2 string = "memory.stat"
	MemoryMaxFileV2   string = "memory.max"
	MemoryHighFileV2  string = "memory.high"
	MemoryLowFileV2   string = "memory.low"
	MemoryMinFileV2   string = "memory.min"
)

// MemoryInterfaceName represents the name of a specific memory interface.
type MemoryInterfaceName string

// MemorySubsystem defines the method to access different memory control files.
type MemorySubsystem interface {
	// Max returns the interface for memory.max (or memory.limit_in_bytes in v1).
	Max() (MemoryInterface, bool)
	// High returns the interface for memory.high (cgroup v2 only).
	High() (MemoryInterface, bool)
	// Low returns the interface for memory.low (cgroup v2 only).
	Low() (MemoryInterface, bool)
	// Min returns the interface for memory.min (cgroup v2 only).
	Min() (MemoryInterface, bool)
}

// MemoryInterface defines the operations for interacting with a specific memory cgroup interface.
// All memory amounts are represented in bytes.
// Note: A negative value typically indicates "unlimited".
type MemoryInterface interface {
	// Name returns the identifier of this memory interface.
	Name() string
	// Get reads the current value in bytes from the cgroup path.
	Get(cgroupPath string) (bytes int64, err error)
	// Set writes a value in bytes to the cgroup path.
	Set(cgroupPath string, bytes int64) error
}

type memorySubsystemImpl struct {
	interfaceMap map[MemoryInterfaceName]MemoryInterface
}

// NewMemorySubsystem creates a new MemorySubsystem based on the detected cgroup version.
func NewMemorySubsystem(cgroupVersion string) MemorySubsystem {
	switch cgroupVersion {
	case CgroupV1:
		return &memorySubsystemImpl{
			interfaceMap: map[MemoryInterfaceName]MemoryInterface{
				MemoryMax: &memoryInterfaceImpl{
					name:           MemoryMax,
					fileName:       MemoryLimitFile,
					valueUnlimited: valueUnlimitedV1,
				},
			},
		}
	case CgroupV2:
		return &memorySubsystemImpl{
			interfaceMap: map[MemoryInterfaceName]MemoryInterface{
				MemoryMax: &memoryInterfaceImpl{
					name:           MemoryMax,
					fileName:       MemoryMaxFileV2,
					valueUnlimited: valueUnlimitedV2,
				},
				MemoryHigh: &memoryInterfaceImpl{
					name:           MemoryHigh,
					fileName:       MemoryHighFileV2,
					valueUnlimited: valueUnlimitedV2,
				},
				MemoryLow: &memoryInterfaceImpl{
					name:           MemoryLow,
					fileName:       MemoryLowFileV2,
					valueUnlimited: valueUnlimitedV2,
				},
				MemoryMin: &memoryInterfaceImpl{
					name:           MemoryMin,
					fileName:       MemoryMinFileV2,
					valueUnlimited: valueUnlimitedV2,
				},
			},
		}
	default:
		return &memorySubsystemImpl{interfaceMap: make(map[MemoryInterfaceName]MemoryInterface)}
	}
}

// Max returns the memory max interface if available.
func (m *memorySubsystemImpl) Max() (MemoryInterface, bool) {
	memoryMax, ok := m.interfaceMap[MemoryMax]
	return memoryMax, ok
}

// High returns the memory high interface if available.
func (m *memorySubsystemImpl) High() (MemoryInterface, bool) {
	memoryHigh, ok := m.interfaceMap[MemoryHigh]
	return memoryHigh, ok
}

// Low returns the memory low interface if available.
func (m *memorySubsystemImpl) Low() (MemoryInterface, bool) {
	memoryLow, ok := m.interfaceMap[MemoryLow]
	return memoryLow, ok
}

// Min returns the memory min interface if available.
func (m *memorySubsystemImpl) Min() (MemoryInterface, bool) {
	memoryMin, ok := m.interfaceMap[MemoryMin]
	return memoryMin, ok
}

type memoryInterfaceImpl struct {
	name           string
	fileName       string
	valueUnlimited string
}

// Name returns the name of the memory interface.
func (m *memoryInterfaceImpl) Name() string {
	return m.name
}

// Get retrieves the current value of the memory setting from the cgroup file.
func (m *memoryInterfaceImpl) Get(cgroupPath string) (int64, error) {
	p := filepath.Join(cgroupPath, m.fileName)
	data, err := file.ReadByteFromFile(p)
	if err != nil {
		return 0, err
	}
	value := strings.TrimRight(string(data), "\n")
	if value == m.valueUnlimited {
		return MemoryUnlimited, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

// Set updates the memory setting in the cgroup file.
func (m *memoryInterfaceImpl) Set(cgroupPath string, value int64) error {
	p := filepath.Join(cgroupPath, m.fileName)
	data := func() string {
		if value == MemoryUnlimited || value < 0 {
			return m.valueUnlimited
		}
		return strconv.FormatInt(value, 10)
	}()
	return utils.UpdateFile(p, []byte(data))
}
