/*
Copyright 2017 The Kubernetes Authors.

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

package preemption

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
)

// Interface is the interface of preemption.
type Interface interface {
	// Run start informer
	Run(stopCh <-chan struct{})

	// Preprocessing kill pod to make each queue underused
	Preprocessing(queues map[string]*schedulercache.QueueInfo, pods []*schedulercache.PodInfo) (map[string]*schedulercache.QueueInfo, error)

	// PreemptResource preempt resources between job
	PreemptResources(queues map[string]*schedulercache.QueueInfo) error
}
