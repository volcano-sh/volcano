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

// sort taskset under queue by priority from high to low
func (ps *proportionScheduler) sortTaskSetByPriority(queue string, ts []*schedulercache.TaskSetInfo) []*schedulercache.TaskSetInfo {
	sortedPriorityTaskSet := PriorityTaskSetSlice{}

	for _, t := range ts {
		if queue == t.TaskSet().Spec.Queue {
			sortedPriorityTaskSet = append(sortedPriorityTaskSet, t)
		}
	}
	sort.Sort(sortedPriorityTaskSet)

	return sortedPriorityTaskSet
}

func (ps *proportionScheduler) Name() string {
	return PolicyName
}

func (ps *proportionScheduler) Initialize() {
	// TODO
}

func (ps *proportionScheduler) Group(
	jobs []*schedulercache.QueueInfo,
	tasksets []*schedulercache.TaskSetInfo,
	pods []*schedulercache.PodInfo,
) (map[string][]*schedulercache.QueueInfo, []*schedulercache.PodInfo) {
	glog.V(4).Infof("Enter Group ...")
	defer glog.V(4).Infof("Leaving Group ...")

	// calculate total taskset resource request under queue
	scheduledJobs := make([]*schedulercache.QueueInfo, 0)
	for _, job := range jobs {
		cloneJob := job.Clone()

		totalResOfJob := map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
		for _, ts := range tasksets {
			if ts.TaskSet().Spec.Queue != cloneJob.Name() {
				continue
			}
			glog.V(4).Infof("taskset %s belongs to queue %s\n", ts.Name(), cloneJob.Name())
			totalResOfTaskSet := schedulercache.ResourcesMultiply(ts.TaskSet().Spec.ResourceUnit.Resources, ts.TaskSet().Spec.ResourceNo)
			totalResOfJob = schedulercache.ResourcesAdd(totalResOfJob, totalResOfTaskSet)
		}

		if !schedulercache.ResourcesIsZero(totalResOfJob) {
			// the taskset under this job has resource request, otherwise use the original resource request of job
			cloneJob.Queue().Spec.Request.Resources = totalResOfJob
		}
		scheduledJobs = append(scheduledJobs, cloneJob)

		glog.V(4).Infof("the resource request of queue %s, %#v", cloneJob.Name(), cloneJob.Queue().Spec.Request.Resources)
	}
	groups := make(map[string][]*schedulercache.QueueInfo)
	for _, job := range scheduledJobs {
		groups[job.Queue().Namespace] = append(groups[job.Queue().Namespace], job)
	}

	scheduledPods := make([]*schedulercache.PodInfo, 0)
	for _, pod := range pods {
		// only schedule Pending/Running pod
		if pod.Pod().Status.Phase == corev1.PodPending || pod.Pod().Status.Phase == corev1.PodRunning {
			scheduledPods = append(scheduledPods, pod.Clone())
		}
	}

	return groups, scheduledPods
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
			glog.V(4).Infof("Second round, assign %s %d to queue %s", resType, assignedRes, job.Name())
		}
	}

	return allocatedQueueResult
}

func (ps *proportionScheduler) Assign(
	jobs map[string]*schedulercache.QueueInfo,
	ts []*schedulercache.TaskSetInfo,
) map[string]*schedulercache.TaskSetInfo {
	glog.V(4).Infof("Enter Assign ...")
	defer glog.V(4).Infof("Leaving Assign ...")

	result := make(map[string]*schedulercache.TaskSetInfo)
	resourceTypes := []apiv1.ResourceName{"cpu", "memory"}
	for _, job := range jobs {
		cpuRes := job.Queue().Status.Allocated.Resources["cpu"].DeepCopy()
		memRes := job.Queue().Status.Allocated.Resources["memory"].DeepCopy()
		cpuInt, _ := cpuRes.AsInt64()
		memInt, _ := memRes.AsInt64()
		allocatedResources := map[apiv1.ResourceName]resource.Quantity{
			"cpu":    job.Queue().Status.Allocated.Resources["cpu"].DeepCopy(),
			"memory": job.Queue().Status.Allocated.Resources["memory"].DeepCopy(),
		}
		glog.V(4).Infof("assign resources to taskset under queue %s, cpu %d, memory %d\n", job.Name(), cpuInt, memInt)
		sortedTaskSet := ps.sortTaskSetByPriority(job.Name(), ts)
		for _, t := range sortedTaskSet {
			glog.V(4).Infof("    assign resource to taskset %s, queue %s, priority %d\n", t.Name(), t.TaskSet().Spec.Queue, t.TaskSet().Spec.Priority)
			totalResOfTaskSet := schedulercache.ResourcesMultiply(t.TaskSet().Spec.ResourceUnit.Resources, t.TaskSet().Spec.ResourceNo)

			// reset allocated resource of taskset
			t.TaskSet().Status.Allocated.Resources = map[apiv1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("0"),
				"memory": resource.MustParse("0"),
			}

			for _, resType := range resourceTypes {
				allocatedRes := allocatedResources[resType].DeepCopy()
				if allocatedRes.IsZero() {
					continue
				}

				requestRes := totalResOfTaskSet[resType].DeepCopy()
				assignRes := resource.MustParse("0")
				if allocatedRes.Cmp(requestRes) <= 0 {
					assignRes = allocatedRes
					allocatedResources[resType] = resource.MustParse("0")
				} else {
					assignRes = requestRes
					allocatedRes.Sub(requestRes)
					allocatedResources[resType] = allocatedRes
				}
				t.TaskSet().Status.Allocated.Resources[resType] = assignRes

				resInt, _ := assignRes.AsInt64()
				glog.V(4).Infof("        assign %s resource %d to taskset %s\n", resType, resInt, t.TaskSet().Name)
			}

			result[t.Name()] = t.Clone()
		}
	}

	return result
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
