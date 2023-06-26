/*
Copyright 2019 The Volcano Authors.

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

package helpers

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	// PodNameFmt pod name format
	PodNameFmt = "%s-%s-%d"
	// persistentVolumeClaimFmt represents persistent volume claim name format
	persistentVolumeClaimFmt = "%s-pvc-%s"
)

// GetPodIndexUnderTask returns task Index.
func GetPodIndexUnderTask(pod *v1.Pod) string {
	num := strings.Split(pod.Name, "-")
	if len(num) >= 3 {
		return num[len(num)-1]
	}

	return ""
}

// CompareTask by pod index
func CompareTask(lv, rv *api.TaskInfo) bool {
	lStr := GetPodIndexUnderTask(lv.Pod)
	rStr := GetPodIndexUnderTask(rv.Pod)
	lIndex, lErr := strconv.Atoi(lStr)
	rIndex, rErr := strconv.Atoi(rStr)
	if lErr != nil || rErr != nil {
		return lv.Pod.CreationTimestamp.Before(&rv.Pod.CreationTimestamp)
	}
	/*
		If one party satisfies the task minAvailable, the other party has a higher priority:

									  ｜ lIndex+1 <= lv.MinAvailable ｜ lIndex+1 > lv.MinAvailable
		  ----------------------------｜-----------------------------｜-----------------------------
		  rIndex+1 <= rv.MinAvailable ｜      index compare          ｜        r prior to l
		  ----------------------------｜-----------------------------｜-----------------------------
		  rIndex+1 > rv.MinAvailable  ｜      l prior to r           ｜        index compare

		for example:
		master:
			Replaces: 5
			MinAvailable: 2
			Pods: master-0、master-1、master-2、master-3、master-4
		work:
			Replaces: 3
			MinAvailable: 3
			Pods: work-0、work-1、work-2

		the right pods order should be:
			master-0、work-0、master-1、work-1、work-2、  master-2、master-3、work-3、master-4
		   ｜-----------------MinAvailable-------------｜
	*/
	if lIndex+1 > lv.MinAvailable && rIndex+1 <= rv.MinAvailable {
		return false
	}
	if rIndex+1 > lv.MinAvailable && lIndex+1 <= rv.MinAvailable {
		return true
	}

	if lIndex == rIndex {
		return lv.Pod.CreationTimestamp.Before(&rv.Pod.CreationTimestamp)
	}
	if lIndex > rIndex {
		return false
	}
	return true
}

// GetTaskKey returns task key/name
func GetTaskKey(pod *v1.Pod) string {
	if pod.Annotations == nil || pod.Annotations[batch.TaskSpecKey] == "" {
		return batch.DefaultTaskSpec
	}
	return pod.Annotations[batch.TaskSpecKey]
}

// GetTaskSpec returns task spec
func GetTaskSpec(job *batch.Job, taskName string) (batch.TaskSpec, bool) {
	for _, ts := range job.Spec.Tasks {
		if ts.Name == taskName {
			return ts, true
		}
	}
	return batch.TaskSpec{}, false
}

// MakeDomainName creates task domain name
func MakeDomainName(ts batch.TaskSpec, job *batch.Job, index int) string {
	hostName := ts.Template.Spec.Hostname
	subdomain := ts.Template.Spec.Subdomain
	if len(hostName) == 0 {
		hostName = MakePodName(job.Name, ts.Name, index)
	}
	if len(subdomain) == 0 {
		subdomain = job.Name
	}
	return hostName + "." + subdomain
}

// MakePodName creates pod name.
func MakePodName(jobName string, taskName string, index int) string {
	return fmt.Sprintf(PodNameFmt, jobName, taskName, index)
}

// GenRandomStr generate random str with specified length l.
func GenRandomStr(l int) string {
	str := "0123456789abcdefghijklmnopqrstuvwxyz"
	bytes := []byte(str)
	var result []byte
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < l; i++ {
		result = append(result, bytes[r.Intn(len(bytes))])
	}
	return string(result)
}

// GenPVCName generates pvc name with job name.
func GenPVCName(jobName string) string {
	return fmt.Sprintf(persistentVolumeClaimFmt, jobName, GenRandomStr(12))
}

// GetJobKeyByReq gets the key for the job request.
func GetJobKeyByReq(req *apis.Request) string {
	return fmt.Sprintf("%s/%s", req.Namespace, req.JobName)
}

// GetTasklndexUnderJob return index of the task in the job.
func GetTasklndexUnderJob(taskName string, job *batch.Job) int {
	for index, task := range job.Spec.Tasks {
		if task.Name == taskName {
			return index
		}
	}
	return -1
}

// GetPodsNameUnderTask return names of all pods in the task.
func GetPodsNameUnderTask(taskName string, job *batch.Job) []string {
	var res []string
	for _, task := range job.Spec.Tasks {
		if task.Name == taskName {
			for index := 0; index < int(task.Replicas); index++ {
				res = append(res, MakePodName(job.Name, taskName, index))
			}
			break
		}
	}
	return res
}
