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

package cni

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/klog/v2"
)

//go:generate mockgen -destination  ./mocks/mock_config.go -package mocks -source config.go

type CNIPluginsConfHandler interface {
	// AddOrUpdateCniPluginToConfList adds new cni conf to the file(cni.conflist)
	// If the cni plugin existed, update the configuration
	AddOrUpdateCniPluginToConfList(configPath, name string, plugin map[string]interface{}) (err error)
	// DeleteCniPluginFromConfList removes cni conf to the file(cni.conflist)
	DeleteCniPluginFromConfList(configPath, name string) (err error)
}

type ConfHandler struct{}

var _ CNIPluginsConfHandler = &ConfHandler{}

var confHandler CNIPluginsConfHandler

func GetCNIPluginConfHandler() CNIPluginsConfHandler {
	if confHandler == nil {
		confHandler = &ConfHandler{}
	}
	return confHandler
}

func SetCNIPluginConfHandler(c CNIPluginsConfHandler) {
	confHandler = c
}

func (p *ConfHandler) AddOrUpdateCniPluginToConfList(configPath, name string, plugin map[string]interface{}) (err error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var list map[string]interface{}
	err = json.Unmarshal(content, &list)
	if err != nil {
		return err
	}

	klog.InfoS("Add/Update cni plugin", "cni-conf", string(content))
	plugins, ok := list["plugins"].([]interface{})
	if !ok {
		return fmt.Errorf("failed to get plugins from file")
	}

	var index int
	var existedPlugin map[string]interface{}
	for index = 0; index < len(plugins); index++ {
		existedPlugin, ok = plugins[index].(map[string]interface{})
		if !ok {
			return fmt.Errorf("failed to get cni from cni conflist")
		}

		pluginName, ok := existedPlugin["name"]
		if !ok {
			continue
		}

		pluginNameInStr, ok := pluginName.(string)
		if !ok {
			continue
		}

		if pluginNameInStr == name {
			break
		}
	}

	if index >= len(plugins) {
		plugins = append(plugins, plugin)
	} else {
		plugins[index] = plugin
	}

	list["plugins"] = plugins

	newCniConf, err := json.MarshalIndent(list, "", "    ")
	if err != nil {
		return err
	}
	err = os.WriteFile(configPath, newCniConf, 0750)
	klog.InfoS("Add/Update cni plugin successfully", "cni-conf", string(newCniConf))
	return err
}

func (p *ConfHandler) DeleteCniPluginFromConfList(configPath, name string) (err error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var list map[string]interface{}
	err = json.Unmarshal(content, &list)
	if err != nil {
		return err
	}

	klog.InfoS("Delete cni plugin", "cni-conf", string(content))
	plugins, ok := list["plugins"].([]interface{})
	if !ok {
		return fmt.Errorf("failed to get plugins from file")
	}

	var index int
	for index = 0; index < len(plugins); index++ {
		pl, ok := plugins[index].(map[string]interface{})
		if !ok {
			return fmt.Errorf("failed to get cni from cni conflist")
		}
		pluginName, ok := pl["name"].(string)
		if !ok {
			continue
		}

		if pluginName == name {
			break
		}
	}

	if index >= len(plugins) {
		return
	}
	plugins = append(plugins[:index], plugins[index+1:]...)

	list["plugins"] = plugins

	newCniConf, err := json.MarshalIndent(list, "", "    ")
	if err != nil {
		return err
	}
	err = os.WriteFile(configPath, newCniConf, 0750)
	klog.InfoS("Delete cni plugin successfully", "cni-conf", string(newCniConf))
	return err
}
