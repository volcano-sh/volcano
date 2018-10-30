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

package framework

import (
	"github.com/golang/glog"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/cache"
)

func OpenSession(cache cache.Cache, args []*PluginArgs) *Session {
	ssn := openSession(cache)

	for _, arg := range args {
		if pb, found := GetPluginBuilder(arg.Name); !found {
			glog.Errorf("Failed to get plugin %s.", arg.Name)
		} else {
			ssn.plugins = append(ssn.plugins, pb(arg))
		}
	}

	for _, plugin := range ssn.plugins {
		plugin.OnSessionOpen(ssn)
	}

	return ssn
}

func CloseSession(ssn *Session) {
	for _, plugin := range ssn.plugins {
		plugin.OnSessionClose(ssn)
	}

	closeSession(ssn)
}
