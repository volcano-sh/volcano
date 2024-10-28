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

package framework

import (
	"sync"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
)

var metricCollectorFuncs = []NewMetricCollectFunc{}
var mutex sync.Mutex

type NewMetricCollectFunc = func(config *config.Configuration, cgroupManager cgroup.CgroupManager) MetricCollect

func RegisterMetricCollectFunc(f NewMetricCollectFunc) {
	mutex.Lock()
	defer mutex.Unlock()
	metricCollectorFuncs = append(metricCollectorFuncs, f)
}

func GetMetricCollectFunc() []NewMetricCollectFunc {
	return metricCollectorFuncs
}
