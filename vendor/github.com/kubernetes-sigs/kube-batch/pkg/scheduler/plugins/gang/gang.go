/*
Copyright 2018 The Kubernetes Authors.

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

package gang

import (
	"fmt"

	"github.com/golang/glog"

	arbcorev1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

type gangPlugin struct {
	args *framework.PluginArgs
}

func New(args *framework.PluginArgs) framework.Plugin {
	return &gangPlugin{
		args: args,
	}
}

// readyTaskNum return the number of tasks that are ready to run.
func readyTaskNum(job *api.JobInfo) int32 {
	occupid := 0
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) ||
			status == api.Succeeded ||
			status == api.Pipelined {
			occupid = occupid + len(tasks)
		}
	}

	return int32(occupid)
}

// validTaskNum return the number of tasks that are valid.
func validTaskNum(job *api.JobInfo) int32 {
	occupid := 0
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) ||
			status == api.Succeeded ||
			status == api.Pipelined ||
			status == api.Pending {
			occupid = occupid + len(tasks)
		}
	}

	return int32(occupid)
}

func jobReady(obj interface{}) bool {
	job := obj.(*api.JobInfo)

	occupid := readyTaskNum(job)

	return occupid >= job.MinAvailable
}

func (gp *gangPlugin) OnSessionOpen(ssn *framework.Session) {
	for _, job := range ssn.Jobs {
		if validTaskNum(job) < job.MinAvailable {
			ssn.Backoff(job, arbcorev1.UnschedulableEvent, "not enough valid tasks for gang-scheduling")
		}
	}

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
		var victims []*api.TaskInfo

		for _, preemptee := range preemptees {
			job := ssn.JobIndex[preemptee.Job]
			occupid := readyTaskNum(job)
			preemptable := job.MinAvailable <= occupid-1

			if !preemptable {
				glog.V(3).Infof("Can not preempt task <%v/%v> because of gang-scheduling",
					preemptee.Namespace, preemptee.Name)
			} else {
				victims = append(victims, preemptee)
			}
		}

		glog.V(3).Infof("Victims from Gang plugins are %+v", victims)

		return victims
	}

	// TODO(k82cn): Support preempt/reclaim batch job.
	ssn.AddReclaimableFn(preemptableFn)

	if gp.args.PreemptableFnEnabled {
		ssn.AddPreemptableFn(preemptableFn)
	}

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		lReady := jobReady(lv)
		rReady := jobReady(rv)

		glog.V(3).Infof("Gang JobOrderFn: <%v/%v> is ready: %t, <%v/%v> is ready: %t",
			lv.Namespace, lv.Name, lReady, rv.Namespace, rv.Name, rReady)

		if lReady && rReady {
			return 0
		}

		if lReady {
			return 1
		}

		if rReady {
			return -1
		}

		if !lReady && !rReady {
			if lv.CreationTimestamp.Equal(&rv.CreationTimestamp) {
				if lv.UID < rv.UID {
					return -1
				}
			} else if lv.CreationTimestamp.Before(&rv.CreationTimestamp) {
				return -1
			}
			return 1
		}

		return 0
	}

	if gp.args.JobOrderFnEnabled {
		ssn.AddJobOrderFn(jobOrderFn)
	}

	if gp.args.JobReadyFnEnabled {
		ssn.AddJobReadyFn(jobReady)
	}
}

func (gp *gangPlugin) OnSessionClose(ssn *framework.Session) {
	for _, job := range ssn.Jobs {
		if len(job.TaskStatusIndex[api.Pending]) != 0 {
			glog.V(3).Infof("Gang: <%v/%v> allocated: %v, pending: %v", job.Namespace, job.Name, len(job.TaskStatusIndex[api.Allocated]), len(job.TaskStatusIndex[api.Pending]))
			msg := fmt.Sprintf("%v/%v tasks in gang unschedulable: %v", len(job.TaskStatusIndex[api.Pending]), len(job.Tasks), job.FitError())
			ssn.Backoff(job, arbcorev1.UnschedulableEvent, msg)
		}
	}
}
