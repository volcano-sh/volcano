/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2023 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced preemption logic with support for both inter-job and intra-job priority-based preemption
- Added job starving detection functionality to improve resource allocation efficiency

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
	"regexp"
	"strconv"
	"strings"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "priority"

type priorityPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return priority plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &priorityPlugin{pluginArguments: arguments}
}

func (pp *priorityPlugin) Name() string {
	return PluginName
}

func taskOrderFn(l interface{}, r interface{}) int {
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)

	// if NodeIPAware feature is enabled, sort tasks by rank first
	if utilfeature.DefaultFeatureGate.Enabled(features.NodeIPAware) {
		if ret := compareTaskByRank(lv, rv); ret != 0 {
			return ret
		}
	}

	klog.V(4).Infof("Priority TaskOrder: <%v/%v> priority is %v, <%v/%v> priority is %v",
		lv.Namespace, lv.Name, lv.Priority, rv.Namespace, rv.Name, rv.Priority)

	if lv.Priority == rv.Priority {
		// if NodeIPAware feature is enabled, sort tasks by name suffix
		if utilfeature.DefaultFeatureGate.Enabled(features.NodeIPAware) {
			return compareTaskByNameSuffix(lv, rv)
		}
		return api.OrderingEqual
	}

	if lv.Priority > rv.Priority {
		return api.OrderingLess
	}

	return api.OrderingGreater
}

func (pp *priorityPlugin) OnSessionOpen(ssn *framework.Session) {
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

var replicaTypeMap = map[string]int{
	"launcher": 1,
	"master":   2,
	"worker":   3,
	"ps":       4,
}

func compareTaskByRank(a, b *api.TaskInfo) int {
	rankA := a.GetRank()
	rankB := b.GetRank()

	// If RANK is specified
	if rankA != "" || rankB != "" {
		return compareNumberStr(rankA, rankB)
	}
	return api.OrderingEqual
}

func compareTaskByNameSuffix(a, b *api.TaskInfo) int {
	// If RANK is not specified (primarily for MPI tasks, as ranktable parsing
	// can be complex), sort by pod name index for graceful handling.
	// FOR PyTorchJob, the pods that created are named as
	// xxx-master-0
	// xxx-worker-0
	// xxx-worker-1
	//
	// FOR MPIJob, the pods that created are named as
	// xxx-launcher
	// xxx-worker-0
	// xxx-worker-1

	var getReplicateType = func(name string) string {
		subStrs := strings.Split(name, "-")
		if len(subStrs) < 2 {
			return ""
		}
		if subStrs[len(subStrs)-1] == "launcher" {
			return "launcher"
		}
		return subStrs[len(subStrs)-2]
	}

	aRT := getReplicateType(a.Name)
	bRT := getReplicateType(b.Name)
	if aRT != bRT {
		// Tasks with a replica type that has a lower integer value come first.
		if replicaTypeMap[aRT] < replicaTypeMap[bRT] {
			return api.OrderingLess
		}
		return api.OrderingGreater
	}

	aNum := extractTrailingNumber(a.Name)
	bNum := extractTrailingNumber(b.Name)
	return compareNumberStr(aNum, bNum)
}
func compareNumberStr(rankA, rankB string) int {
	rankAId, aErr := strconv.Atoi(rankA)
	rankBId, bErr := strconv.Atoi(rankB)
	if aErr == nil && bErr == nil {
		switch {
		case rankAId < rankBId:
			return api.OrderingLess
		case rankAId > rankBId:
			return api.OrderingGreater
		default:
			return api.OrderingEqual
		}
	}
	if aErr == nil && bErr != nil {
		return api.OrderingGreater
	}

	if aErr != nil && bErr == nil {
		return api.OrderingLess
	}
	return api.OrderingEqual
}

var trailingNumberRegex = regexp.MustCompile(`-(\d+)$`)

func extractTrailingNumber(str string) string {
	matches := trailingNumberRegex.FindStringSubmatch(str)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}
