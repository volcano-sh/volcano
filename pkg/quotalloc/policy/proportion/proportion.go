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
	"sort"

	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/policy/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// PolicyName is the name of proportion policy; it'll be use for any case
// that need a name, e.g. default policy, register proportion policy.
var PolicyName = "proportion"

type proportionScheduler struct {
}

func New() *proportionScheduler {
	return &proportionScheduler{}
}

// collect total resources of the cluster
func (ps *proportionScheduler) collectSchedulingInfo(jobGroup map[string][]*cache.QuotaAllocatorInfo, nodes []*cache.NodeInfo) (int64, int64, int64) {
	totalCPU := int64(0)
	totalMEM := int64(0)
	totalWeight := int64(0)

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

	for _, jobs := range jobGroup {
		for _, job := range jobs {
			totalWeight += int64(job.QuotaAllocator().Spec.Weight)
		}
	}

	return totalCPU, totalMEM, totalWeight
}

// sort quotaAllocator by weight from high to low
func (ps *proportionScheduler) sortQuotaAllocatorByWeight(jobGroup map[string][]*cache.QuotaAllocatorInfo) []*cache.QuotaAllocatorInfo {
	sortedWeightJobs := util.WeightJobSlice{}

	for _, jobs := range jobGroup {
		for _, job := range jobs {
			sortedWeightJobs = append(sortedWeightJobs, job)
		}
	}
	sort.Sort(sortedWeightJobs)

	return sortedWeightJobs
}

func (ps *proportionScheduler) Name() string {
	return PolicyName
}

func (ps *proportionScheduler) Initialize() {
	// TODO
}

func (ps *proportionScheduler) Group(
	jobs []*cache.QuotaAllocatorInfo,
	pods []*cache.PodInfo,
) (map[string][]*cache.QuotaAllocatorInfo, []*cache.PodInfo) {
	glog.V(4).Infof("Enter Group ...")
	defer glog.V(4).Infof("Leaving Group ...")

	groups := make(map[string][]*cache.QuotaAllocatorInfo)
	for _, job := range jobs {
		groups[job.QuotaAllocator().Namespace] = append(groups[job.QuotaAllocator().Namespace], job)
	}

	scheduledPods := make([]*cache.PodInfo, 0)
	for _, pod := range pods {
		// only schedule Pending/Running pod
		if pod.Pod().Status.Phase == corev1.PodPending || pod.Pod().Status.Phase == corev1.PodRunning {
			scheduledPods = append(scheduledPods, pod.Clone())
		}
	}

	return groups, scheduledPods
}

func (ps *proportionScheduler) Allocate(
	jobGroup map[string][]*cache.QuotaAllocatorInfo,
	nodes []*cache.NodeInfo,
) map[string]*cache.QuotaAllocatorInfo {
	glog.V(4).Infof("Enter Allocate ...")
	defer glog.V(4).Infof("Leaving Allocate ...")

	totalCPU, totalMEM, totalWeight := ps.collectSchedulingInfo(jobGroup, nodes)
	if totalCPU == 0 || totalMEM == 0 || totalWeight == 0 {
		glog.V(4).Infof("There is no resources or quotaAllocators in cluster, totalCPU %d, totalMEM %d, totalWeight %d", totalCPU, totalMEM, totalWeight)
		return nil
	}

	jobsSortedByWeight := ps.sortQuotaAllocatorByWeight(jobGroup)
	glog.V(4).Infof("Scheduler information, totalCPU %d, totalMEM %d, totalWeight %d, quotaAllocatorSize %d", totalCPU, totalMEM, totalWeight, len(jobsSortedByWeight))

	allocatedQuotaAllocatorResult := make(map[string]*cache.QuotaAllocatorInfo)
	for _, job := range jobsSortedByWeight {
		allocatedQuotaAllocatorResult[job.Name()] = job.Clone()
		// clear Used resources
		allocatedQuotaAllocatorResult[job.Name()].QuotaAllocator().Status.Used = arbv1.ResourceList{
			Resources: make(map[arbv1.ResourceName]resource.Quantity),
		}
	}

	for _, job := range jobsSortedByWeight {
		assignedCPU := totalCPU * int64(job.QuotaAllocator().Spec.Weight) / totalWeight
		assignedMEM := totalMEM * int64(job.QuotaAllocator().Spec.Weight) / totalWeight
		allocatedQuotaAllocatorResult[job.Name()].QuotaAllocator().Status.Deserved.Resources["cpu"] = *resource.NewQuantity(assignedCPU, resource.DecimalSI)
		allocatedQuotaAllocatorResult[job.Name()].QuotaAllocator().Status.Deserved.Resources["memory"] = *resource.NewQuantity(assignedMEM, resource.DecimalSI)
	}

	return allocatedQuotaAllocatorResult
}

func (ps *proportionScheduler) Assign(
	jobs map[string]*cache.QuotaAllocatorInfo,
) map[string]*cache.QuotaAllocatorInfo {
	// TODO
	return nil
}

func (ps *proportionScheduler) Polish(
	job *cache.QuotaAllocatorInfo,
	res *cache.Resource,
) []*cache.QuotaAllocatorInfo {
	// TODO
	return nil
}

func (ps *proportionScheduler) UnInitialize() {
	// TODO
}
