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

package conformance

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/apis/scheduling"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

type conformancePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return conformance plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &conformancePlugin{pluginArguments: arguments}
}

func (pp *conformancePlugin) Name() string {
	return "conformance"
}

func (pp *conformancePlugin) OnSessionOpen(ssn *framework.Session) {
	evictableFn := func(evictor *api.TaskInfo, evictees []*api.TaskInfo) []*api.TaskInfo {
		var victims []*api.TaskInfo

		for _, evictee := range evictees {
			className := evictee.Pod.Spec.PriorityClassName
			// Skip critical pod.
			if className == scheduling.SystemClusterCritical ||
				className == scheduling.SystemNodeCritical ||
				evictee.Namespace == v1.NamespaceSystem {

				continue
			}

			victims = append(victims, evictee)
		}

		return victims
	}

	ssn.AddPreemptableFn(pp.Name(), evictableFn)
	ssn.AddReclaimableFn(pp.Name(), evictableFn)
}

func (pp *conformancePlugin) OnSessionClose(ssn *framework.Session) {}
