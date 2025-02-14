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
	"errors"

	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type Config struct {
	NvidiaConfig NvidiaConfig `yaml:"nvidia"`
}

var (
	configs *Config
)

func GetConfig() *Config {
	return configs
}

func LoadConfigFromCM(kubeClient kubernetes.Interface, cmName string) (*Config, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps("kube-system").Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		cm, err = kubeClient.CoreV1().ConfigMaps("volcano-system").Get(context.Background(), cmName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
	}
	data, ok := cm.Data["device-config.yaml"]
	if !ok {
		return nil, errors.New("data-config.yaml not found")
	}
	var yamlData Config
	err = yaml.Unmarshal([]byte(data), &yamlData)
	if err != nil {
		return nil, err
	}
	return &yamlData, nil
}

func InitDevicesConfig(kubeClient kubernetes.Interface, cmName string) {
	var err error
	if len(cmName) == 0 {
		cmName = "volcano-vgpu-device-config"
	}
	configs, err = LoadConfigFromCM(kubeClient, cmName)
	if err != nil {
		configs = &Config{
			NvidiaConfig: NvidiaConfig{
				ResourceCountName:   "volcano.sh/vgpu-number",
				ResourceCoreName:    "volcano.sh/vgpu-cores",
				ResourceMemoryName:  "volcano.sh/vgpu-memory",
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
	klog.V(3).InfoS("Initializing volcano vgpu config", "device-configs", configs)
}
