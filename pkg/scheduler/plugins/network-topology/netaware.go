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

package networktopology

import (
	"strings"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// PluginName indicates name of volcano scheduler plugin.
const (
	PluginName            = "network-topology"
	networkTopologyWeight = "network-topology.weight"
	networkTopologyType   = "network-topology.type" // strategy type for network topology
	// strategy value for network-topology.type
	staticAware  = "static"
	dynamicAware = "dynamic"
)

type netTopPlugin struct {
	pluginArguments framework.Arguments
	weight          int            // network-topology plugin score weight
	topologyType    string         // supported topology type: static, dynamic
	staticTopAware  staticTopAware // use node labels to generate topology
}

// New return gang plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &netTopPlugin{pluginArguments: arguments, weight: 1, staticTopAware: staticTopAware{records: map[api.JobID]string{}}}
}

func (np *netTopPlugin) Name() string {
	return PluginName
}

func (np *netTopPlugin) parseArguments() {
	np.pluginArguments.GetInt(&np.weight, networkTopologyWeight)
	value, ok := np.pluginArguments[networkTopologyType]
	if !ok {
		klog.Warningf("%s is not set, use default strategy %s", networkTopologyType, staticAware)
		np.topologyType = staticAware
		return
	}

	v, ok := value.(string)
	if !ok {
		klog.Warningf("invalid value for %s, use default strategy %s", networkTopologyType, staticAware)
		np.topologyType = staticAware
		return
	}
	np.topologyType = strings.TrimSpace(v)
}

// parseStaticAwareArguments return a boolean value indicating whether staticAware is valid to be used
func (np *netTopPlugin) parseStaticAwareArguments() bool {
	keys, exist := np.pluginArguments[networkTopologyKeys]
	if !exist {
		klog.Warningf("plugin %s (with type %s) arguments does not configure %s, skip", PluginName, np.topologyType, networkTopologyKeys)
		return false
	}
	topKeys, ok := keys.(string)
	if !ok {
		klog.Warningf("plugin %s (with type %s) arguments %s should has a string value", PluginName, networkTopologyKeys, networkTopologyKeys)
		return false
	}
	for _, key := range strings.Split(topKeys, ",") {
		np.staticTopAware.topologyKeys = append(np.staticTopAware.topologyKeys, strings.TrimSpace(key))
	}
	np.staticTopAware.weight = np.weight
	return true
}

func (np *netTopPlugin) OnSessionOpen(ssn *framework.Session) {
	np.parseArguments()
	if np.topologyType == staticAware {
		valid := np.parseStaticAwareArguments()
		if valid {
			np.staticTopAware.OnSessionOpen(ssn)
		}
	} else {
		klog.Warningf("strategy %s is not supported in plugin %s", np.topologyType, PluginName)
	}
}

func (np *netTopPlugin) OnSessionClose(ssn *framework.Session) {
	if np.topologyType == staticAware {
		np.staticTopAware.OnSessionClose(ssn)
	}
}
