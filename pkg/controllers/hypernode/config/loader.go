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
	"fmt"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

type Loader interface {
	LoadConfig() (*api.NetworkTopologyConfig, error)
}

const (
	NamespaceEnvKey    = "KUBE_POD_NAMESPACE"
	ReleaseNameEnvKey  = "HELM_RELEASE_NAME"
	DefaultReleaseName = "volcano"

	DefaultNamespace = "volcano-system"
	DefaultConfigKey = "volcano-controller.conf"
)

// loader handles loading discovery configuration from ConfigMap
type loader struct {
	configMapLister    v1.ConfigMapLister
	configMapNamespace string
	configMapName      string
	configKey          string
}

// NewConfigLoader creates a new config loader
func NewConfigLoader(configMapLister v1.ConfigMapLister, namespace, name string) Loader {
	return &loader{
		configMapLister:    configMapLister,
		configMapNamespace: namespace,
		configMapName:      name,
		configKey:          DefaultConfigKey,
	}
}

// LoadConfig loads the configuration from ConfigMap
func (l *loader) LoadConfig() (*api.NetworkTopologyConfig, error) {
	klog.InfoS("Loading config from ConfigMap", "namespace", l.configMapNamespace, "name", l.configMapName)
	cm, err := l.configMapLister.ConfigMaps(l.configMapNamespace).Get(l.configMapName)
	if err != nil {
		return nil, err
	}

	configYaml, ok := cm.Data[l.configKey]
	if !ok {
		return nil, fmt.Errorf("config key not found in ConfigMap: %s", l.configKey)
	}

	config := &api.NetworkTopologyConfig{}
	if err = yaml.Unmarshal([]byte(configYaml), config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}
	return config, nil
}
