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

package cache

import (
	"fmt"
	"regexp"
)

/*
data:
  node-label-based-topologies: |-
    topologies:
	- name: topology1
	  topo-level-keys:
	    - topology1-tier1
		- topology1-tier2
	- name: topology2
	  topo-level-keys:
	    - topology2-tier1
		- topology2-tier2
*/

const (
	NetworkTopologyAwareConfigMapName = "volcano-networktopologyaware-configuration"
	NodeLabelBasedTopologiesConfKey   = "node-label-based-topologies"
	TopologyNameRegex                 = "^[a-z0-9]+$"
	LabelKeyRegex                     = "^[A-Za-z0-9]([-A-Za-z0-9._]*[A-Za-z0-9])?(\\/[a-zA-Z0-9]([-A-Za-z0-9._]*[A-Za-z0-9])?)?$"
	TopologyNameLengthLimit           = 253
)

type NodeLabelBasedTopology struct {
	Name          string   `yaml:"name"`
	TopoLevelKeys []string `yaml:"topo-level-keys"`
}

type NodeLabelBasedTopologies struct {
	Topologies []NodeLabelBasedTopology `yaml:"topologies"`
}

func (t *NodeLabelBasedTopologies) Validate() error {
	topologyNameRegex, err := regexp.Compile(TopologyNameRegex)
	if err != nil {
		return err
	}
	labelKeyRegex, err := regexp.Compile(LabelKeyRegex)
	if err != nil {
		return err
	}

	hyperNames := make(map[string]bool)
	for _, topology := range t.Topologies {
		if _, ok := hyperNames[topology.Name]; ok {
			return fmt.Errorf("topologies.name: Invalid value %s, the is duplicated", topology.Name)
		}
		hyperNames[topology.Name] = true
		if !topologyNameRegex.MatchString(topology.Name) {
			return fmt.Errorf("topologies.name: Invalid value %s, regex used for validation %s", topology.Name, TopologyNameRegex)
		}
		for _, key := range topology.TopoLevelKeys {
			if len(key) > TopologyNameLengthLimit {
				return fmt.Errorf("topologies.topo-level-keys: Invalid value %s, maximum length of 253 characters", key)
			}
			if !labelKeyRegex.MatchString(key) {
				return fmt.Errorf("topologies.topo-level-keys: Invalid value %s, regex used for validation %s", key, LabelKeyRegex)
			}
		}
	}
	return nil
}
