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

package throttling

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"unsafe"

	cilliumbpf "github.com/cilium/ebpf"

	agentutils "volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/utils"
	"volcano.sh/volcano/pkg/networkqos/utils/ebpf"
)

const (
	TCEbpfPath               = "/bpf/tc/globals/"
	ThrottleConfigMapName    = "throttle_cfg"
	ThrottleStatusMapName    = "throttle_map"
	ThrottleConfigMapPinPath = TCEbpfPath + ThrottleConfigMapName
	ThrottleStatusMapPinPath = TCEbpfPath + ThrottleStatusMapName
)

type sliceStr struct {
	addr uintptr
	len  int
	cap  int
}

//go:generate mockgen -destination  ./mocks/mock_throttling.go -package mocks -source throttling.go

type ThrottlingConfig interface {
	// CreateThrottlingConfig inits the throttling config values, if no initial value is given, the default value will be used
	CreateThrottlingConfig(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, checkInterval string) (*api.EbpfNetThrottlingConfig, error)
	// CreateOrUpdateThrottlingConfig updates the throttling config values
	CreateOrUpdateThrottlingConfig(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, checkInterval string) (*api.EbpfNetThrottlingConfig, error)
	// DeleteThrottlingConfig deletes the throttling config map
	DeleteThrottlingConfig() (err error)
	// GetThrottlingConfig gets the throttling config values
	GetThrottlingConfig() (*api.EbpfNetThrottlingConfig, error)
	// GetThrottlingStatus returns the status of network throttling
	GetThrottlingStatus() (*api.EbpfNetThrottling, error)
}

var _ ThrottlingConfig = &NetworkThrottlingConfig{}

type NetworkThrottlingConfig struct {
	sysFsDir string
}

var networkThrottlingConfig ThrottlingConfig

func GetNetworkThrottlingConfig() ThrottlingConfig {
	if networkThrottlingConfig == nil {
		sfsFsPath := strings.TrimSpace(os.Getenv(agentutils.SysFsPathEnv))
		if sfsFsPath == "" {
			sfsFsPath = agentutils.DefaultSysFsPath
		}
		networkThrottlingConfig = &NetworkThrottlingConfig{
			sysFsDir: sfsFsPath,
		}
	}
	return networkThrottlingConfig
}

func SetNetworkThrottlingConfig(throttlingConfig ThrottlingConfig) {
	networkThrottlingConfig = throttlingConfig
}

func (w *NetworkThrottlingConfig) CreateThrottlingConfig(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, checkInterval string) (*api.EbpfNetThrottlingConfig, error) {
	var onlineBandwidthWatermarkBytes, offlineLowBandwidthBytes, offlineHighBandwidthBytes, intervalUInt uint64
	var err error

	onlineBandwidthWatermarkBytes, err = utils.SizeStrConvertToByteSize(onlineBandwidthWatermark)
	if err != nil {
		return nil, err
	}

	offlineLowBandwidthBytes, err = utils.SizeStrConvertToByteSize(offlineLowBandwidth)
	if err != nil {
		return nil, err
	}

	offlineHighBandwidthBytes, err = utils.SizeStrConvertToByteSize(offlineHighBandwidth)
	if err != nil {
		return nil, err
	}

	intervalUInt, err = strconv.ParseUint(checkInterval, 10, 0)
	if err != nil {
		return nil, err
	}

	config := &api.EbpfNetThrottlingConfig{
		Interval:  intervalUInt,
		WaterLine: onlineBandwidthWatermarkBytes,
		LowRate:   offlineLowBandwidthBytes,
		HighRate:  offlineHighBandwidthBytes,
	}

	Len := unsafe.Sizeof(*config) / unsafe.Sizeof(uint64(0))
	testBytes := &sliceStr{
		addr: uintptr(unsafe.Pointer(config)),
		cap:  int(Len),
		len:  int(Len),
	}
	data := *(*[]uint64)(unsafe.Pointer(testBytes))
	err = ebpf.GetEbpfMap().CreateAndPinArrayMap(path.Join(w.sysFsDir, TCEbpfPath), ThrottleConfigMapName, []cilliumbpf.MapKV{
		{
			Key:   uint32(0),
			Value: &data,
		},
	})

	if err != nil {
		return nil, err
	}
	return config, nil
}

func (w *NetworkThrottlingConfig) CreateOrUpdateThrottlingConfig(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, checkInterval string) (*api.EbpfNetThrottlingConfig, error) {
	config, err := w.GetThrottlingConfig()
	if err != nil {
		if errors.Is(err, cilliumbpf.ErrKeyNotExist) || os.IsNotExist(err) {
			return w.CreateThrottlingConfig(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, checkInterval)
		}
		return nil, err
	}

	var onlineBandwidthWatermarkBytes, offlineLowBandwidthBytes, offlineHighBandwidthBytes, intervalUInt uint64
	if onlineBandwidthWatermark != "" {
		onlineBandwidthWatermarkBytes, err = utils.SizeStrConvertToByteSize(onlineBandwidthWatermark)
		if err != nil {
			return nil, err
		}
		config.WaterLine = onlineBandwidthWatermarkBytes
	}

	if offlineLowBandwidth != "" {
		offlineLowBandwidthBytes, err = utils.SizeStrConvertToByteSize(offlineLowBandwidth)
		if err != nil {
			return nil, err
		}
		config.LowRate = offlineLowBandwidthBytes
	}

	if offlineHighBandwidth != "" {
		offlineHighBandwidthBytes, err = utils.SizeStrConvertToByteSize(offlineHighBandwidth)
		if err != nil {
			return nil, err
		}
		config.HighRate = offlineHighBandwidthBytes
	}

	if checkInterval != "" {
		intervalUInt, err = strconv.ParseUint(checkInterval, 10, 0)
		if err != nil {
			return nil, err
		}
		config.Interval = intervalUInt
	}

	Len := unsafe.Sizeof(*config) / unsafe.Sizeof(uint64(0))
	testBytes := &sliceStr{
		addr: uintptr(unsafe.Pointer(config)),
		cap:  int(Len),
		len:  int(Len),
	}
	data := *(*[]uint64)(unsafe.Pointer(testBytes))
	err = ebpf.GetEbpfMap().UpdateMapValue(path.Join(w.sysFsDir, ThrottleConfigMapPinPath), uint32(0), data)
	if err != nil {
		return nil, err
	}
	return config, err
}

func (w *NetworkThrottlingConfig) DeleteThrottlingConfig() (err error) {
	err = ebpf.GetEbpfMap().UnpinArrayMap(path.Join(w.sysFsDir, ThrottleConfigMapPinPath))
	if err != nil {
		return err
	}
	_, err = w.GetThrottlingConfig()
	if err != nil {
		if errors.Is(err, cilliumbpf.ErrKeyNotExist) || os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return fmt.Errorf("failed to unpin map, config map still existed")
}

func (w *NetworkThrottlingConfig) GetThrottlingConfig() (*api.EbpfNetThrottlingConfig, error) {
	throttlingConfig := make([]uint64, unsafe.Sizeof(api.EbpfNetThrottlingConfig{})/unsafe.Sizeof(uint64(0)))
	err := ebpf.GetEbpfMap().LookUpMapValue(path.Join(w.sysFsDir, ThrottleConfigMapPinPath), uint32(0), &throttlingConfig)
	if err != nil {
		return nil, err
	}

	var config *api.EbpfNetThrottlingConfig = *(**api.EbpfNetThrottlingConfig)(unsafe.Pointer(&throttlingConfig))
	return config, nil
}

func (w *NetworkThrottlingConfig) GetThrottlingStatus() (*api.EbpfNetThrottling, error) {
	throttling := make([]uint64, unsafe.Sizeof(api.EbpfNetThrottling{})/unsafe.Sizeof(uint64(0)))
	err := ebpf.GetEbpfMap().LookUpMapValue(path.Join(w.sysFsDir, ThrottleStatusMapPinPath), uint32(0), &throttling)
	if err != nil {
		return nil, err
	}

	var edtThrottling *api.EbpfNetThrottling = *(**api.EbpfNetThrottling)(unsafe.Pointer(&throttling))
	return edtThrottling, nil
}
