/*
Copyright 2019 The Kubernetes Authors.

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

package enqueue

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"

	"volcano.sh/volcano/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// idleResMultiplierKey
const idleResMultiplierKey = "overcommit-factor"

// default idle resource multiplier value
const defaultIdleResMultiplierValue = 1.2

// enqueueActionName
const enqueueActionName = "enqueue"

type enqueueAction struct {
	multiplier float64
}

func New() *enqueueAction {
	return &enqueueAction{multiplier: defaultIdleResMultiplierValue}
}

func (enqueue *enqueueAction) Name() string {
	return enqueueActionName
}

func (enqueue *enqueueAction) Initialize(config *conf.SchedulerConf) {
	if config.Version == framework.SchedulerConfigVersion2 {
		ret, err := getEnqueueActMultiplier(config.V2Conf.Actions)
		if err == nil {
			enqueue.multiplier = ret
		}
	}
}

func (enqueue *enqueueAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Enqueue ...")
	defer glog.V(3).Infof("Leaving Enqueue ...")

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}

	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	for _, job := range ssn.Jobs {
		if queue, found := ssn.Queues[job.Queue]; !found {
			glog.Errorf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		} else {
			if _, existed := queueMap[queue.UID]; !existed {
				glog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
					queue.Name, job.Namespace, job.Name)

				queueMap[queue.UID] = queue
				queues.Push(queue)
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupPending {
			if _, found := jobsMap[job.Queue]; !found {
				jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			glog.V(3).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
			jobsMap[job.Queue].Push(job)
		}
	}

	glog.V(3).Infof("Try to enqueue PodGroup to %d Queues", len(jobsMap))

	emptyRes := api.EmptyResource()
	nodesIdleRes := api.EmptyResource()
	for _, node := range ssn.Nodes {
		nodesIdleRes.Add(node.Allocatable.Clone().Sub(node.Used))
	}
	nodesIdleRes.Multi(enqueue.multiplier)

	for {
		if queues.Empty() {
			break
		}

		if nodesIdleRes.Less(emptyRes) {
			glog.V(3).Infof("Node idle resource is overused, ignore it.")
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		// Found "high" priority job
		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			continue
		}
		job := jobs.Pop().(*api.JobInfo)

		inqueue := false

		if job.PodGroup.Spec.MinResources == nil {
			inqueue = true
		} else {
			pgResource := api.NewResource(*job.PodGroup.Spec.MinResources)
			if ssn.JobEnqueueable(job) && pgResource.LessEqual(nodesIdleRes) {
				nodesIdleRes.Sub(pgResource)
				inqueue = true
			}
		}

		if inqueue {
			job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			ssn.Jobs[job.UID] = job
		}

		// Added Queue back until no job in Queue.
		queues.Push(queue)
	}
}

func (enqueue *enqueueAction) UnInitialize() {}

func getEnqueueActMultiplier(actOpt []conf.ActionOption) (float64, error) {

	actionOpt, err := getEnqueueActionOption(actOpt)

	if err != nil {
		return 0, err
	}

	if actionOpt.Arguments != nil {
		val, ok := actionOpt.Arguments[idleResMultiplierKey]
		if ok {
			value, err := strconv.ParseFloat(val, 64)
			if err != nil {
				glog.Warningf("Could not parse argument: %s for key %s, with err %v", val, idleResMultiplierKey, err)
				return 0, err
			}
			return value, nil
		}
	}
	return 0, fmt.Errorf("The required key %s is not there in config", idleResMultiplierKey)

}

func getEnqueueActionOption(actOpts []conf.ActionOption) (conf.ActionOption, error) {
	var actionOpt conf.ActionOption
	for _, actionOpt = range actOpts {
		if strings.Compare(enqueueActionName, strings.TrimSpace(actionOpt.Name)) == 0 {
			return actionOpt, nil
		}
	}
	return actionOpt, fmt.Errorf("The required action %s is not there in config", enqueueActionName)
}
