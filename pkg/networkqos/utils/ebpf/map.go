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
	"unsafe"

	"github.com/cilium/ebpf"

	"volcano.sh/volcano/pkg/networkqos/api"
)

//go:generate mockgen -destination  ./mocks/mock_map.go -package mocks -source map.go

// EbpfMap is an interface of ebpf map options.
type EbpfMap interface {
	// CreateAndPinArrayMap create map with default set of key/value data
	// only supports map of "array" type, pinning of "pingByName" type
	// dir is the base path to pin map
	// $dir/$name is the path to pin map
	// If the map already existed,
	CreateAndPinArrayMap(dir, name string, defaultKV []ebpf.MapKV) (err error)
	// UnpinArrayMap removes the persisted state for the map from the BPF virtual filesystem
	UnpinArrayMap(pinPath string) (err error)
	// LookUpMapValue looks up the value of the key
	LookUpMapValue(pinPath string, key, value interface{}) (err error)
	// UpdateMapValue updates the value of the key
	UpdateMapValue(pinPath string, key, value interface{}) (err error)
}

type Map struct{}

var _ EbpfMap = &Map{}

var ebpfMap EbpfMap

func GetEbpfMap() EbpfMap {
	if ebpfMap == nil {
		ebpfMap = &Map{}
	}
	return ebpfMap
}

func SetEbpfMap(m EbpfMap) {
	ebpfMap = m
}

func (m *Map) CreateAndPinArrayMap(dir, name string, defaultKV []ebpf.MapKV) (err error) {
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	eMap, err := ebpf.NewMapWithOptions(&ebpf.MapSpec{
		Name:       name,
		Type:       ebpf.Array,
		KeySize:    uint32(unsafe.Sizeof(uint32(0))),
		ValueSize:  uint32(unsafe.Sizeof(api.EbpfNetThrottlingConfig{})),
		Pinning:    ebpf.PinByName,
		MaxEntries: 1,
		Contents:   defaultKV,
	}, ebpf.MapOptions{
		PinPath: dir,
	})
	if err != nil {
		return err
	}
	defer eMap.Close()

	return nil
}

func (m *Map) UnpinArrayMap(pinPath string) (err error) {
	_, err = os.Stat(pinPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	eMap, err := ebpf.LoadPinnedMap(pinPath, nil)
	if err != nil {
		return fmt.Errorf("failed to load pinned map(%s), error %v", pinPath, err)
	}
	defer eMap.Close()

	if err = eMap.Unpin(); err != nil {
		return fmt.Errorf("cannot unpin map %s: %w", pinPath, err)
	}

	return nil
}

func (m *Map) LookUpMapValue(pinPath string, key, value interface{}) (err error) {
	_, err = os.Stat(pinPath)
	if err != nil {
		return err
	}

	eMap, err := ebpf.LoadPinnedMap(pinPath, nil)
	if err != nil {
		return fmt.Errorf("failed to load pinned map(%s), error %v", pinPath, err)
	}
	defer eMap.Close()

	err = eMap.Lookup(key, value)
	if err != nil {
		return fmt.Errorf("failed to look up map(%s), error %v, value %+v", pinPath, err, value)
	}
	return nil
}

func (m *Map) UpdateMapValue(pinPath string, key, value interface{}) (err error) {
	_, err = os.Stat(pinPath)
	if err != nil {
		return err
	}

	eMap, err := ebpf.LoadPinnedMap(pinPath, nil)
	if err != nil {
		return fmt.Errorf("failed to load pinned map(%s), error %v", pinPath, err)
	}
	defer eMap.Close()

	err = eMap.Put(key, value)
	if err != nil {
		return fmt.Errorf("failed to look up map(%s), error %v", pinPath, err)
	}
	return nil
}
