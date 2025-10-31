/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package test is using for HuaWei Ascend pin scheduling test.
*/
package test

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
)

// SetTestJobPodGroupPendingStatus set test job's PodGroupStatus
func SetTestJobPodGroupPendingStatus(job *api.JobInfo) {
	if job.PodGroup == nil {
		AddTestJobPodGroup(job)
	}
	job.PodGroup.Status.Phase = util.PodGroupPending
}

// AddTestJobPodGroup set test job pg.
func AddTestJobPodGroup(job *api.JobInfo) {
	var minRes = make(v1.ResourceList, npuIndex3)
	if job == nil || job.PodGroup != nil {
		return
	}
	for _, task := range job.Tasks {
		for k, v := range task.Resreq.ScalarResources {
			minRes[k] = resource.MustParse(fmt.Sprintf("%f", v))
		}
	}

	pg := &api.PodGroup{
		Version: api.PodGroupVersionV1Beta1,
	}
	pg.Spec.MinResources = &minRes
	job.SetPodGroup(pg)
}

// AddTestJobLabel add test job's label.
func AddTestJobLabel(job *api.JobInfo, labelKey, labelValue string) {
	if job.PodGroup == nil {
		AddTestJobPodGroup(job)
	}
	if job.PodGroup.Labels == nil {
		job.PodGroup.Labels = make(map[string]string, npuIndex3)
	}
	job.PodGroup.Labels = map[string]string{labelKey: labelValue}
	for _, task := range job.Tasks {
		AddTestTaskLabel(task, labelKey, labelValue)
	}
}

// FakeNormalTestJob make normal test job.
func FakeNormalTestJob(jobName string, taskNum int) *api.JobInfo {
	return FakeNormalTestJobByCreatTime(jobName, taskNum, 0)
}

// FakeNormalTestJobByCreatTime make normal test job by create time.
func FakeNormalTestJobByCreatTime(jobName string, taskNum int, creatTime int64) *api.JobInfo {
	tasks := FakeNormalTestTasks(taskNum)
	for _, vTask := range tasks {
		AddFakeTaskResReq(vTask, NPU910CardName, NPUIndex8)
	}
	job := api.NewJobInfo(api.JobID("vcjob/"+jobName), tasks...)
	job.Name = jobName
	for _, task := range tasks {
		task.Job = job.UID
	}
	job.MinAvailable = int32(taskNum)
	job.PodGroup = new(api.PodGroup)
	job.PodGroup.Status.Phase = util.PodGroupRunning
	job.PodGroup.Status.Conditions = make([]scheduling.PodGroupCondition, 1)
	job.CreationTimestamp = metav1.Time{Time: time.Unix(time.Now().Unix()+creatTime, 0)}
	return job
}

// SetFakeJobRequestSource add job require on total,task.
func SetFakeJobRequestSource(fJob *api.JobInfo, name string, value int) {
	if fJob.PodGroup == nil {
		AddTestJobPodGroup(fJob)
	}
	SetTestJobPodGroupPendingStatus(fJob)

	if len(fJob.TotalRequest.ScalarResources) == 0 {
		fJob.TotalRequest.ScalarResources = make(map[v1.ResourceName]float64, npuIndex3)
	}
	fJob.TotalRequest.ScalarResources[v1.ResourceName(name)] = float64(value)

	var minRes = make(v1.ResourceList, npuIndex3)
	minRes[v1.ResourceName(name)] = resource.MustParse(fmt.Sprintf("%f", float64(value)))
	fJob.PodGroup.Spec.MinResources = &minRes
	reqResource := api.NewResource(*fJob.PodGroup.Spec.MinResources)
	fJob.TotalRequest = reqResource
	return
}

// SetFakeJobResRequest set fake
func SetFakeJobResRequest(fJob *api.JobInfo, name v1.ResourceName, need string) {
	resources := v1.ResourceList{}
	AddResource(resources, name, need)
	if fJob.Tasks == nil || len(fJob.Tasks) == 0 {
		return
	}
	total := resource.Quantity{}
	for _, task := range fJob.Tasks {
		task.Resreq = api.NewResource(resources)
		total.Add(resource.MustParse(need))
	}
	fJob.PodGroup.Spec.MinResources = &v1.ResourceList{name: total}
}

// SetFakeNPUJobStatusPending set job and it's tasks to pending status.
func SetFakeNPUJobStatusPending(fJob *api.JobInfo) {
	if fJob == nil {
		return
	}
	fJob.PodGroup.Status.Phase = util.PodGroupPending
	for _, task := range fJob.Tasks {
		SetFakeNPUTaskStatus(task, api.Pending)
		SetFakeNPUPodStatus(task.Pod, v1.PodPending)
	}
	return
}

// SetFakeNPUJobErrors set job and it's tasks to pending status.
func SetFakeNPUJobErrors(fJob *api.JobInfo, msg string) {
	if fJob == nil {
		return
	}
	fJob.JobFitErrors = msg
	if len(fJob.NodesFitErrors) == 0 {
		fJob.NodesFitErrors = make(map[api.TaskID]*api.FitErrors, npuIndex3)
	}
	for tID := range fJob.Tasks {
		fitError := api.NewFitErrors()
		fitError.SetError(msg)
		fJob.NodesFitErrors[tID] = fitError
	}

	return
}

// FakeTaskWithResReq fake task with resource require
func FakeTaskWithResReq(name, resName string, resNum int) *api.TaskInfo {
	pod := NPUPod{
		Namespace: "vcjob", Name: name, Phase: v1.PodRunning,
		ReqSource: buildNPUResourceList("1", "1000", v1.ResourceName(resName), strconv.Itoa(resNum)),
	}
	task := api.NewTaskInfo(BuildNPUPod(pod))
	task.Job = FakeJobName
	return task
}

// SetJobStatusRunning set job status running
func SetJobStatusRunning(Job *api.JobInfo) {
	Job.PodGroup.Status.Phase = util.PodGroupRunning
}
