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
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/cache"
)

func OpenSession(cache cache.Cache) *Session {
	snapshot := cache.Snapshot()

	ssn := &Session{
		ID: uuid.NewUUID(),

		cache: cache,

		Jobs:  snapshot.Jobs,
		Nodes: snapshot.Nodes,
	}

	for _, pb := range pluginBuilders {
		ssn.plugins = append(ssn.plugins, pb())
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

	ssn.Jobs = nil
	ssn.Nodes = nil
}
