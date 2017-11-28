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
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

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
func (ps *proportionScheduler) collectSchedulingInfo(jobGroup map[string][]*schedulercache.QueueInfo, nodes []*schedulercache.NodeInfo) (int64, int64, int64) {
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
			totalWeight += int64(job.Queue().Spec.Weight)
		}
	}

	return totalCPU, totalMEM, totalWeight
}

// sort queue by cpu from low to high
func (ps *proportionScheduler) sortQueueByCPU(jobGroup map[string][]*schedulercache.QueueInfo) []*schedulercache.QueueInfo {
	sortedCPUJobs := CPUJobSlice{}

	for _, jobs := range jobGroup {
		for _, job := range jobs {
			sortedCPUJobs = append(sortedCPUJobs, job)
		}
	}
	sort.Sort(sortedCPUJobs)

	return sortedCPUJobs
}

// sort queue by memory from low to high
func (ps *proportionScheduler) sortQueueByMEM(jobGroup map[string][]*schedulercache.QueueInfo) []*schedulercache.QueueInfo {
	sortedMEMJobs := MEMJobSlice{}

	for _, jobs := range jobGroup {
		for _, job := range jobs {
			sortedMEMJobs = append(sortedMEMJobs, job)
		}
	}
	sort.Sort(sortedMEMJobs)

	return sortedMEMJobs
}

// sort queue by weight from high to low
func (ps *proportionScheduler) sortQueueByWeight(jobGroup map[string][]*schedulercache.QueueInfo) []*schedulercache.QueueInfo {
	sortedWeightJobs := WeightJobSlice{}

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
	jobs []*schedulercache.QueueInfo,
) map[string][]*schedulercache.QueueInfo {
	groups := make(map[string][]*schedulercache.QueueInfo)
	for _, job := range jobs {
		groups[job.Queue().Namespace] = append(groups[job.Queue().Namespace], job)
	}

	return groups
}

func (ps *proportionScheduler) Allocate(
	jobGroup map[string][]*schedulercache.QueueInfo,
	nodes []*schedulercache.NodeInfo,
) map[string]*schedulercache.QueueInfo {
	glog.V(4).Infof("Enter Allocate ...")
	defer glog.V(4).Infof("Leaving Allocate ...")

	totalCPU, totalMEM, totalWeight := ps.collectSchedulingInfo(jobGroup, nodes)
	if totalCPU == 0 || totalMEM == 0 || totalWeight == 0 {
		glog.V(4).Infof("There is no resources or queues in cluster, totalCPU %d, totalMEM %d, totalWeight %d", totalCPU, totalMEM, totalWeight)
		return nil
	}

	totalResources := map[apiv1.ResourceName]int64{
		"cpu":    totalCPU,
		"memory": totalMEM,
	}
	sortedJobs := map[apiv1.ResourceName][]*schedulercache.QueueInfo{
		"cpu":    ps.sortQueueByCPU(jobGroup),
		"memory": ps.sortQueueByMEM(jobGroup),
	}
	jobsSortedByWeight := ps.sortQueueByWeight(jobGroup)
	glog.V(4).Infof("Scheduler information, totalCPU %d, totalMEM %d, totalWeight %d, queueSize %d", totalCPU, totalMEM, totalWeight, len(jobsSortedByWeight))

	allocatedQueueResult := make(map[string]*schedulercache.QueueInfo)
	for _, jobs := range jobGroup {
		for _, job := range jobs {
			allocatedQueueResult[job.Name()] = job.Clone()
			// clear Used resources
			allocatedQueueResult[job.Name()].Queue().Status.Used = apiv1.ResourceList{
				Resources: make(map[apiv1.ResourceName]resource.Quantity),
			}
		}
	}

	// assign resource cpu/memory to each queue by max-min weighted fairness
	resourceTypes := []apiv1.ResourceName{"cpu", "memory"}
	totalAllocatedRes := map[apiv1.ResourceName]int64{
		"cpu":    int64(0),
		"memory": int64(0),
	}
	for _, resType := range resourceTypes {
		leftRes := totalResources[resType]
		leftWeight := totalWeight
		for _, job := range sortedJobs[resType] {
			if leftRes == 0 || leftWeight == 0 {
				break
			}

			requestAsQuantity := job.Queue().Spec.Request.Resources[resType].DeepCopy()
			requestRes, _ := requestAsQuantity.AsInt64()

			queueWeight := int64(job.Queue().Spec.Weight)
			calculatedRes := queueWeight * leftRes / leftWeight

			allocatedRes := int64(0)
			if requestRes >= calculatedRes {
				allocatedRes = calculatedRes
			} else {
				allocatedRes = requestRes
			}
			totalAllocatedRes[resType] += allocatedRes
			allocatedQueueResult[job.Name()].Queue().Status.Deserved.Resources[resType] = *resource.NewQuantity(allocatedRes, resource.DecimalSI)

			leftRes -= allocatedRes
			leftWeight -= queueWeight
			glog.V(4).Infof("First round, assign %s %d to queue %s, weight %d, request %d", resType, allocatedRes, job.Name(), queueWeight, requestRes)
		}
	}

	// assign left resources to queue from high weight to low weight
	totalUnallocatedRes := map[apiv1.ResourceName]int64{
		"cpu":    totalResources["cpu"] - totalAllocatedRes["cpu"],
		"memory": totalResources["memory"] - totalAllocatedRes["memory"],
	}
	for _, job := range jobsSortedByWeight {
		for _, resType := range resourceTypes {
			if totalUnallocatedRes[resType] <= 0 {
				continue
			}

			requestRes := job.Queue().Spec.Request.Resources[resType].DeepCopy()
			allocatedRes := allocatedQueueResult[job.Name()].Queue().Status.Deserved.Resources[resType].DeepCopy()
			if requestRes.Cmp(allocatedRes) <= 0 {
				continue
			}

			requestRes.Sub(allocatedRes)
			insufficientRes, _ := requestRes.AsInt64()
			assignedRes := int64(0)
			if totalUnallocatedRes[resType] > insufficientRes {
				assignedRes = insufficientRes
				totalUnallocatedRes[resType] -= insufficientRes
			} else {
				assignedRes = totalUnallocatedRes[resType]
				totalUnallocatedRes[resType] = 0
			}
			res := allocatedQueueResult[job.Name()].Queue().Status.Deserved.Resources[resType]
			res.Add(*resource.NewQuantity(assignedRes, resource.DecimalSI))
			allocatedQueueResult[job.Name()].Queue().Status.Deserved.Resources[resType] = res
			glog.V(4).Infof("Second round, assign %s %d to queue %s", resType, allocatedRes, job.Name())
		}
	}

	return allocatedQueueResult
}

func (ps *proportionScheduler) Assign(
	jobs []*schedulercache.QueueInfo,
	alloc *schedulercache.QueueInfo,
) *schedulercache.Resource {
	// TODO
	return nil
}

func (ps *proportionScheduler) Polish(
	job *schedulercache.QueueInfo,
	res *schedulercache.Resource,
) []*schedulercache.QueueInfo {
	// TODO
	return nil
}

func (ps *proportionScheduler) UnInitialize() {
	// TODO
}
