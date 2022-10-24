/*
Copyright 2022 The Kubernetes Authors.

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

package nodeorder

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
)

func leastAllocated(handle k8sframework.Handle, fts feature.Features) (*noderesources.Fit, error) {
	leastAllocatedArgs := &config.NodeResourcesFitArgs{
		ScoringStrategy: &config.ScoringStrategy{
			Type:      config.LeastAllocated,
			Resources: []config.ResourceSpec{{Name: "cpu", Weight: 50}, {Name: "memory", Weight: 50}},
		},
	}
	p, err := noderesources.NewFit(leastAllocatedArgs, handle, fts)
	return p.(*noderesources.Fit), err
}

func mostAllocated(handle k8sframework.Handle, fts feature.Features) (*noderesources.Fit, error) {
	mostAllocatedArgs := &config.NodeResourcesFitArgs{
		ScoringStrategy: &config.ScoringStrategy{
			Type:      config.MostAllocated,
			Resources: []config.ResourceSpec{{Name: "cpu", Weight: 1}, {Name: "memory", Weight: 1}},
		},
	}
	p, err := noderesources.NewFit(mostAllocatedArgs, handle, fts)
	return p.(*noderesources.Fit), err
}

func balancedAllocated(handle k8sframework.Handle, fts feature.Features) (*noderesources.Fit, error) {
	blArgs := &config.NodeResourcesBalancedAllocationArgs{
		Resources: []config.ResourceSpec{
			{Name: string(v1.ResourceCPU), Weight: 1},
			{Name: string(v1.ResourceMemory), Weight: 1},
			{Name: "nvidia.com/gpu", Weight: 1},
		},
	}
	p, err := noderesources.NewBalancedAllocation(blArgs, handle, fts)
	return p.(*noderesources.Fit), err
}
