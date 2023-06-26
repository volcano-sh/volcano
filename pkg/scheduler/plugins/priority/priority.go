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

package priority

import (
	"fmt"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "priority"

type priorityPlugin struct {
	// Arguments given for the plugin
	pluginArguments  framework.Arguments
	taskMinAvailable map[string]int32
}

// New return priority plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &priorityPlugin{
		taskMinAvailable: map[string]int32{},
		pluginArguments:  arguments,
	}
}

func (pp *priorityPlugin) Name() string {
	return PluginName
}

func (pp *priorityPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.Infof("..........Enter %s plugin..........", PluginName)
	defer klog.Infof("..........Leaving %s plugin..........\n", PluginName)
	for _, job := range ssn.Jobs {
		for taskName, minAvailable := range job.TaskMinAvailable {
			taskID := fmt.Sprintf("%s/%s-%s", job.Namespace, job.Name, taskName)
			pp.taskMinAvailable[taskID] = minAvailable
		}
	}
	taskOrderFn := func(l interface{}, r interface{}) int {
		lv := l.(*api.TaskInfo)
		rv := r.(*api.TaskInfo)

		klog.V(4).Infof("Priority TaskOrder: <%v/%v> priority is %v, <%v/%v> priority is %v",
			lv.Namespace, lv.Name, lv.Priority, rv.Namespace, rv.Name, rv.Priority)

		/*
			     If one party satisfies the task minAvailable, the other party has a higher priority:
			                                            ｜pp.taskMinAvailable[lv.TemplateUID]<=0｜pp.taskMinAvailable[lv.TemplateUID]>0
			     ---------------------------------------｜--------------------------------------｜-----------------------------
			     pp.taskMinAvailable[rv.TemplateUID]<=0 ｜            priority compare          ｜        l prior to r
			     ---------------------------------------｜--------------------------------------｜-----------------------------
			     pp.taskMinAvailable[rv.TemplateUID]>0  ｜             r prior to l             ｜        priority compare

				for example:
				master:
					PriorityClassName: high-priority
					Replaces: 5
					MinAvailable: 3
					Pods: master-0、master-1、master-2、master-3、master-4
				work:
					PriorityClassName: low-priority
					Replaces: 3
					MinAvailable: 2
					Pods: work-0、work-1、work-2

				the right pods order should be:
					master-0、master-1、master-2、work-0、work-1、master-3、master-4、work-2、work-3
				   ｜-----------------MinAvailable-------------｜
		*/
		// if lv task pushed queue enough to MinAvailable, priority push rv to queue
		if pp.taskMinAvailable[lv.TemplateUID] <= 0 && pp.taskMinAvailable[rv.TemplateUID] > 0 {
			pp.taskMinAvailable[rv.TemplateUID]--
			return 1
		}
		if pp.taskMinAvailable[rv.TemplateUID] <= 0 && pp.taskMinAvailable[lv.TemplateUID] > 0 {
			pp.taskMinAvailable[lv.TemplateUID]--
			return -1
		}
		if lv.Priority == rv.Priority {
			return 0
		}

		if lv.Priority > rv.Priority {
			pp.taskMinAvailable[lv.TemplateUID]--
			return -1
		}
		pp.taskMinAvailable[rv.TemplateUID]--
		return 1
	}

	// Add Task Order function
	ssn.AddTaskOrderFn(pp.Name(), taskOrderFn)

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		klog.V(4).Infof("Priority JobOrderFn: <%v/%v> priority: %d, <%v/%v> priority: %d",
			lv.Namespace, lv.Name, lv.Priority, rv.Namespace, rv.Name, rv.Priority)

		if lv.Priority > rv.Priority {
			return -1
		}

		if lv.Priority < rv.Priority {
			return 1
		}

		return 0
	}

	ssn.AddJobOrderFn(pp.Name(), jobOrderFn)

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		preemptorJob := ssn.Jobs[preemptor.Job]

		var victims []*api.TaskInfo
		for _, preemptee := range preemptees {
			preempteeJob := ssn.Jobs[preemptee.Job]
			if preempteeJob.UID != preemptorJob.UID {
				if preempteeJob.Priority >= preemptorJob.Priority { // Preemption between Jobs within Queue
					klog.V(4).Infof("Can not preempt task <%v/%v>"+
						"because preemptee job has greater or equal job priority (%d) than preemptor (%d)",
						preemptee.Namespace, preemptee.Name, preempteeJob.Priority, preemptorJob.Priority)
				} else {
					victims = append(victims, preemptee)
				}
			} else { // same job's different tasks should compare task's priority
				if preemptee.Priority >= preemptor.Priority {
					klog.V(4).Infof("Can not preempt task <%v/%v>"+
						"because preemptee task has greater or equal task priority (%d) than preemptor (%d)",
						preemptee.Namespace, preemptee.Name, preemptee.Priority, preemptor.Priority)
				} else {
					victims = append(victims, preemptee)
				}
			}
		}

		klog.V(4).Infof("Victims from Priority plugins are %+v", victims)
		return victims, util.Permit
	}
	ssn.AddPreemptableFn(pp.Name(), preemptableFn)

	jobStarvingFn := func(obj interface{}) bool {
		ji := obj.(*api.JobInfo)
		return ji.ReadyTaskNum()+ji.WaitingTaskNum() < int32(len(ji.Tasks))
	}
	ssn.AddJobStarvingFns(pp.Name(), jobStarvingFn)
}

func (pp *priorityPlugin) OnSessionClose(ssn *framework.Session) {}
