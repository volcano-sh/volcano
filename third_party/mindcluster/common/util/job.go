/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package util is using for the total variable.
*/
package util

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// NPUJob only npu vcJob have.
type NPUJob struct {
	// the mapKey is taskID, not Name.
	Tasks              map[api.TaskID]NPUTask
	NPUTaskNum         int
	SchedulingTaskNum  int
	ReqNPUName         string
	ReqNPUNum          int
	SpBlockNPUNum      int
	SubHealthyStrategy string
}

// ComJob all vcJob has.
type ComJob struct {
	Name          api.JobID
	ReferenceName string
	NameSpace     string
	Status        string
	Annotation    map[string]string
	Selector      map[string]string
	Label         map[string]string
}

// SchedulerJobAttr vcJob's attribute.
type SchedulerJobAttr struct {
	ComJob
	*NPUJob
}

// GetPluginNameByReq get plugin name by job request resource name
func (sJob SchedulerJobAttr) GetPluginNameByReq() string {
	name := sJob.ReqNPUName
	// 1. dynamic vJobs
	if name == AscendNPUCore {
		label, ok := sJob.Label[JobKindKey]
		if !ok {
			klog.V(LogErrorLev).Infof("%s no has %s label in dyCut mode.", sJob.Name, JobKindKey)
			return ""
		}
		switch label {
		case JobKind910Value, JobKind910BValue:
			name = NPU910CardName
		case JobKind310PValue:
			name = NPU310PCardName
		default:
			klog.V(LogErrorLev).Infof("%s unknown label: %s in dyCut mode.", sJob.Name, label)
			return ""
		}
	}
	return name
}

// IsLargeModelJob job is large model job
func (sJob SchedulerJobAttr) IsLargeModelJob() bool {
	return sJob.Label[TorAffinityKey] == LargeModelTag && sJob.NPUTaskNum >= fillJobMaxNPUTaskNum

}

// IsTorAffinityJob check job is tor affinity job
func (sJob *SchedulerJobAttr) IsTorAffinityJob() bool {
	if sJob == nil {
		return false
	}
	if k, ok := sJob.Label[TorAffinityKey]; ok && (k == LargeModelTag || k == NormalSchema) {
		return true
	}
	return false
}

// IsJobHasTorAffinityLabel check job has tor affinity label
func (sJob *SchedulerJobAttr) IsJobHasTorAffinityLabel() bool {
	if sJob == nil {
		return false
	}
	k, ok := sJob.Label[TorAffinityKey]
	return ok && k != NullTag
}

// IsVJob Determine whether is the NPU virtual job.
// Dynamic segmentation: huawei.com/npu-core.
// static segmentation: huawei.com/Ascend910-Y.
// no segmentation: huawei.com/Ascend910.
func (nJob *NPUJob) IsVJob() bool {
	if nJob == nil {
		return false
	}
	if len(strings.Split(nJob.ReqNPUName, "-")) > 1 {
		return true
	}
	return false
}

// IsNPUJob Determine whether is the NPU job.
// Dynamic segmentation: huawei.com/npu-core.
// static segmentation: huawei.com/Ascend910-Y.
// no segmentation: huawei.com/Ascend910.
func (nJob *NPUJob) IsNPUJob() bool {
	return strings.Contains(nJob.ReqNPUName, HwPreName)
}

// GetSchedulingTaskNum get the num of scheduling task
func (nJob *NPUJob) GetSchedulingTaskNum() int {
	if nJob == nil {
		return 0
	}
	schedulingTaskNum := 0
	for _, task := range nJob.Tasks {
		if task.NodeName == "" {
			schedulingTaskNum++
		}
	}

	return schedulingTaskNum
}

// ReferenceNameOfJob get name of job
func ReferenceNameOfJob(job *api.JobInfo) string {
	if job != nil && job.PodGroup != nil && len(job.PodGroup.OwnerReferences) > 0 {
		return job.PodGroup.OwnerReferences[0].Name
	}
	return ""
}

// UuidOfJob get uid of job
func UuidOfJob(job *api.JobInfo) types.UID {
	if job != nil && job.PodGroup != nil && len(job.PodGroup.OwnerReferences) > 0 {
		return job.PodGroup.OwnerReferences[0].UID
	}
	return ""
}
