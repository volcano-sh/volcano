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

package config

import (
	"context"
	"fmt"
	"sync"

	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
)

const (
	DeviceConfigFileName = "device-config.yaml"
	ConfigMapName        = "volcano-vgpu-device-config"
)

/* Config Examples:
    nvidia:
      resourceCountName: "volcano.sh/vgpu"
      ...
    cambricon:
      resourceCountName: "volcano.sh/vmlu"
      ...
    hygon:
      resourceCountName: "volcano.sh/vdcu"
	  ...
*/

type Config struct {
	//NvidiaConfig is used for vGPU feature for nvidia, gpushare is not using this config
	NvidiaConfig NvidiaConfig `yaml:"nvidia"`
}

var (
	configs *Config
	once    sync.Once
)

// GetConfig returns an already-initialized config
func GetConfig() *Config {
	return configs
}

func loadConfigFromCM(kubeClient kubernetes.Interface, cmName string) (*Config, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps("kube-system").Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data, ok := cm.Data[DeviceConfigFileName]
	if !ok {
		return nil, fmt.Errorf("%s not found", DeviceConfigFileName)
	}
	var yamlData Config
	err = yaml.Unmarshal([]byte(data), &yamlData)
	if err != nil {
		return nil, err
	}
	return &yamlData, nil
}

// InitDevicesConfig is called from devices, to load configs from a CM or construct a default one
func InitDevicesConfig(cmName string) {
	once.Do(func() {
		var err error
		configs, err = loadConfigFromCM(devices.GetClient(), cmName)
		if err != nil {
			klog.V(3).InfoS("Volcano device config not found in namespace kube-system, using default config",
				"name", cmName)
			configs = &Config{
				NvidiaConfig: NvidiaConfig{
					ResourceCountName:   VolcanoVGPUNumber,
					ResourceCoreName:    VolcanoVGPUCores,
					ResourceMemoryName:  VolcanoVGPUMemory,
					DefaultMemory:       0,
					DefaultCores:        0,
					DefaultGPUNum:       1,
					DeviceSplitCount:    10,
					DeviceMemoryScaling: 1,
					DeviceCoreScaling:   1,
					DisableCoreLimit:    false,
				},
			}
		}
		klog.V(3).InfoS("Initializing volcano device config", "device-configs", configs)
	})
}
