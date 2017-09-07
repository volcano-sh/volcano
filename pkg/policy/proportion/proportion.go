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

package proportion

import (
	"fmt"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/util/intstr"
)

type proportionScheduler struct {
	name string
}

func New() policy.Interface {
	return newProportionScheduler("proportion-scheduler")
}

func newProportionScheduler(name string) *proportionScheduler {
	return &proportionScheduler{
		name: name,
	}
}

func (ps *proportionScheduler) Name() string {
	if ps == nil {
		return ""
	}
	return ps.name
}

func (ps *proportionScheduler) Initialize() {
	// TODO
}

func (ps *proportionScheduler) Group(
	job []*schedulercache.ResourceQuotaAllocatorInfo,
) map[string][]*schedulercache.ResourceQuotaAllocatorInfo {
	// TODO
	groups := make(map[string][]*schedulercache.ResourceQuotaAllocatorInfo)
	groups["tmpGroup"] = job
	return groups
}

func (ps *proportionScheduler) Allocate(
	jobs []*schedulercache.ResourceQuotaAllocatorInfo,
	nodes []*schedulercache.NodeInfo,
) map[string]*schedulercache.ResourceQuotaAllocatorInfo {
	fmt.Println("======================== Policy.Allocate")

	// TODO
	totalCPU := int64(0)
	totalMEM := int64(0)
	for _, node := range nodes {
		if cpu, ok := node.Node().Status.Capacity["cpu"]; ok {
			if capacity, ok := cpu.AsInt64(); ok {
				totalCPU += capacity
			}
		}
		if memory, ok := node.Node().Status.Capacity["memory"]; ok {
			if capacity, ok := memory.AsInt64(); ok {
				totalMEM += capacity
			}
		}
	}
	fmt.Printf("==== totalCPU %d\n", totalCPU)
	fmt.Printf("==== totalMEM %d\n", totalMEM)

	totalWeight := 0
	for _, job := range jobs {
		if weight, ok := job.Allocator().Spec.Share["weight"]; ok {
			totalWeight += weight.IntValue()
		}
	}
	fmt.Printf("==== totalWeight %d\n", totalWeight)
	if totalCPU == 0 || totalMEM == 0 || totalWeight == 0 {
		fmt.Println("There is no resources or jobs in cluster")
		return nil
	}

	result := make(map[string]*schedulercache.ResourceQuotaAllocatorInfo)
	for _, job := range jobs {
		if weight, ok := job.Allocator().Spec.Share["weight"]; ok {
			result[job.Name()] = job.Clone()
			result[job.Name()].Allocator().Status.Share = make(map[string]intstr.IntOrString)
			result[job.Name()].Allocator().Status.Share["cpu"] = intstr.FromString(fmt.Sprintf("%d", int64(weight.IntValue())*totalCPU/int64(totalWeight)))
			result[job.Name()].Allocator().Status.Share["memory"] = intstr.FromString(fmt.Sprintf("%d", int64(weight.IntValue())*totalMEM/int64(totalWeight)))
		}
	}
	return result
}

func (ps *proportionScheduler) Assign(
	jobs []*schedulercache.ResourceQuotaAllocatorInfo,
	alloc *schedulercache.ResourceQuotaAllocatorInfo,
) *schedulercache.Resource {
	// TODO
	return nil
}

func (ps *proportionScheduler) Polish(
	job *schedulercache.ResourceQuotaAllocatorInfo,
	res *schedulercache.Resource,
) []*schedulercache.ResourceQuotaAllocatorInfo {
	// TODO
	return nil
}

func (ps *proportionScheduler) UnInitialize() {
	// TODO
}
