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

package metriccollect

import (
	"fmt"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect/framework"
	_ "volcano.sh/volcano/pkg/metriccollect/local"
)

type MetricCollectorManager struct {
	plugins       map[string]framework.MetricCollect
	cgroupManager cgroup.CgroupManager
	Config        *config.Configuration
}

func NewMetricCollectorManager(config *config.Configuration, cgroupManager cgroup.CgroupManager) (*MetricCollectorManager, error) {
	mgr := &MetricCollectorManager{
		plugins:       make(map[string]framework.MetricCollect),
		cgroupManager: cgroupManager,
		Config:        config,
	}

	for _, pluginFunc := range framework.GetMetricCollectFunc() {
		plugin := pluginFunc(config, cgroupManager)
		pluginName := plugin.MetricCollectorName()
		if err := plugin.Run(); err != nil {
			return nil, err
		}
		mgr.plugins[pluginName] = plugin
	}

	return mgr, nil
}

func (cm *MetricCollectorManager) GetPluginByName(name string) (collector framework.MetricCollect, err error) {
	if plugin, ok := cm.plugins[name]; ok {
		return plugin, nil
	}
	return nil, fmt.Errorf("unsupported metric collector plugin %s", name)
}
