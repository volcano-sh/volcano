/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added Unix socket-based HTTP interface for runtime klog level adjustment and debugging
- Improved default scheduler configuration with comprehensive action and plugin setup

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

package agentscheduler

import (
	"flag"
	"os"

	"gopkg.in/yaml.v2"

	"volcano.sh/volcano/pkg/agentscheduler/conf"
	"volcano.sh/volcano/pkg/util"
)

var DefaultSchedulerConf = `
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: overcommit
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
`

func UnmarshalSchedulerConf(confStr string) ([]conf.Configuration, map[string]string, error) {
	schedulerConf := &conf.SchedulerConfiguration{}

	if err := yaml.Unmarshal([]byte(confStr), schedulerConf); err != nil {
		return nil, nil, err
	}
	return schedulerConf.Configurations, schedulerConf.MetricsConfiguration, nil
}

func runSchedulerSocket() {
	fs := flag.CommandLine
	startKlogLevel := fs.Lookup("v").Value.String()
	socketDir := os.Getenv(util.SocketDirEnvName)
	if socketDir == "" {
		socketDir = util.DefaultSocketDir
	}
	util.ListenAndServeKlogLogLevel("klog", startKlogLevel, socketDir)
}
