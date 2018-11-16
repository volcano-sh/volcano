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
	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"
)

type gangPlugin struct {
	args *framework.PluginArgs
}

func New(args *framework.PluginArgs) framework.Plugin {
	return &gangPlugin{
		args: args,
	}
}

func readyTaskNum(job *api.JobInfo) int {
	occupid := 0
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) || status == api.Succeeded {
			occupid = occupid + len(tasks)
		}
	}

	return occupid
}

func jobReady(obj interface{}) bool {
	job := obj.(*api.JobInfo)

	occupid := readyTaskNum(job)

	return occupid >= job.MinAvailable
}

func (gp *gangPlugin) OnSessionOpen(ssn *framework.Session) {
	preemptableFn := func(l, v interface{}) bool {
		preemptee := v.(*api.TaskInfo)

		job := ssn.JobIndex[preemptee.Job]

		occupid := readyTaskNum(job)

		preemptable := job.MinAvailable <= occupid-1

		if !preemptable {
			glog.V(3).Infof("Can not preempt task <%v/%v> because of gang-scheduling",
				preemptee.Namespace, preemptee.Name)
		}

		return preemptable
	}
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
			if lv.UID < rv.UID {
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

}
