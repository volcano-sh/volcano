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
	"reflect"
	"sync"

	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
)

const (
	deviceConfigFileName = "device-config.yaml"
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

func loadConfigFromCM(kubeClient kubernetes.Interface, cmName, cmNamespace string) (*Config, error) {
	if kubeClient == nil || reflect.ValueOf(kubeClient).IsNil() {
		return nil, fmt.Errorf("kube client is nil")
	}

	cm, err := kubeClient.CoreV1().ConfigMaps(cmNamespace).Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data, ok := cm.Data[deviceConfigFileName]
	if !ok {
		return nil, fmt.Errorf("%s not found", deviceConfigFileName)
	}
	var config Config
	err = yaml.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func GetDefaultDevicesConfig() *Config {
	return &Config{
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

// InitDevicesConfig is called from devices, to load configs from a CM or construct a default one
func InitDevicesConfig(cmName, cmNamespace string) {
	once.Do(func() {
		var err error
		configs, err = loadConfigFromCM(devices.GetClient(), cmName, cmNamespace)
		if err != nil {
			klog.V(3).InfoS("Volcano device config not found in namespace kube-system, using default config",
				"name", cmName)
			configs = GetDefaultDevicesConfig()
		}
		klog.V(3).InfoS("Initializing volcano device config", "device-configs", configs)
	})
}
