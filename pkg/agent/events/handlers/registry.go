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

package handlers

import (
	"sync"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

var handlerFuncs = map[string][]NewEventHandleFunc{}
var mutex sync.Mutex

type NewEventHandleFunc = func(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle

func RegisterEventHandleFunc(eventName string, f NewEventHandleFunc) {
	mutex.Lock()
	defer mutex.Unlock()

	handlerFuncs[eventName] = append(handlerFuncs[eventName], f)
}

func GetEventHandlerFuncs() map[string][]NewEventHandleFunc {
	return handlerFuncs
}
