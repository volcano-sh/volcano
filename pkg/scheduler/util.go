/*
Copyright 2018 The Kubernetes Authors.

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

package scheduler

import (
	yaml "gopkg.in/yaml.v2"
	"io/ioutil"

	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins"
)

var defaultSchedulerConf = `
version: v2
actions:
- name: enqueue
  arguments:
    idleres-mul: 1.2
- name: allocate
- name: backfill
tiers:
- plugins:
  - name: priority
  - name: gang
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
`

func loadSchedulerConf(confStr string) (*conf.SchedulerConf, error) {

	schedulerConf := conf.SchedulerConfiguration{}
	schedulerConfv2 := conf.SchedulerConfigurationV2{}
	config := conf.SchedulerConf{}
	Tiers := []conf.Tier{}
	buf := make([]byte, len(confStr))
	copy(buf, confStr)

	if err := yaml.Unmarshal(buf, &schedulerConfv2); err != nil {
		if err := yaml.Unmarshal(buf, &schedulerConf); err != nil {
			return nil, err
		}
		config.Version = framework.SchedulerConfigVersion1
		config.V1Conf = &schedulerConf
		Tiers = schedulerConf.Tiers
	} else {
		config.Version = framework.SchedulerConfigVersion2
		config.V2Conf = &schedulerConfv2
		Tiers = schedulerConfv2.Tiers
	}
	// Set default settings for each plugin if not set
	for i, tier := range Tiers {
		for j := range tier.Plugins {
			plugins.ApplyPluginConfDefaults(&Tiers[i].Plugins[j])
		}
	}

	return &config, nil
}

func readSchedulerConf(confPath string) (string, error) {
	dat, err := ioutil.ReadFile(confPath)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}
