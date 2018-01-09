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

package policy

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
)

// Interface is the interface of policy.
type Interface interface {
	// The unique name of allocator.
	Name() string

	// Initialize initializes the allocator plugins.
	Initialize()

	// Allocate allocates the cluster's resources into each queue.
	Allocate(consumers []*cache.ConsumerInfo, nodes []*cache.NodeInfo) []*cache.ConsumerInfo

	// UnIntialize un-initializes the allocator plugins.
	UnInitialize()
}
