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

package drf

import (
	"github.com/golang/glog"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
)

// PolicyName is the name of drf policy; it'll be use for any case
// that need a name, e.g. default policy, register drf policy.
var PolicyName = "drf"

type drfScheduler struct {
}

func New() *drfScheduler {
	return &drfScheduler{}
}

func (drf *drfScheduler) Name() string {
	return PolicyName
}

func (drf *drfScheduler) Initialize() {
	// TODO
}

func (drf *drfScheduler) Allocate(consumers map[string]*cache.ConsumerInfo, nodes []*cache.NodeInfo) map[string]*cache.ConsumerInfo {
	glog.V(4).Infof("Enter Allocate ...")
	defer glog.V(4).Infof("Leaving Allocate ...")

	for _, node := range nodes {
		available := node.Idle.Clone()

		for {
			// If no available resource on this node, next.
			if available.IsEmpty() {
				break
			}

			assigned := false
			for _, c := range consumers {
				for _, ps := range c.PodSets {
					for _, p := range ps.Pending {
						if len(p.Nodename) != 0 {
							continue
						}

						if p.Request.Less(available) {
							p.Nodename = node.Name
							available.Sub(p.Request)
							assigned = true

							glog.V(3).Infof("assign <%v/%v> to <%s>: available <%v>, request <%v>",
								p.Namespace, p.Name, p.Nodename, available, p.Request)
						}
					}
				}
			}

			// If no Pods can use those resources, next.
			if !assigned {
				break
			}
		}
	}

	return consumers
}

func (drf *drfScheduler) UnInitialize() {
	// TODO
}
