/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

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

package reclaim

import (
	"fmt"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	schedutil "volcano.sh/volcano/pkg/scheduler/util"
)

func (ra *Action) topologyAwareReclaim(
	ssn *framework.Session,
	stmt *framework.Statement,
	reclaimor *api.TaskInfo,
	job *api.JobInfo,
) (bool, error) {
	currentQueue := ssn.Queues[job.Queue]

	totalNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(reclaimor)
	predicateHelper := schedutil.NewPredicateHelper()
	predicateNodes, _ := predicateHelper.PredicateNodes(reclaimor, totalNodes, ssn.PredicateForPreemptAction, ra.enablePredicateErrorCache, ssn.NodesInShard)
	klog.V(4).InfoS("TopologyAwareReclaim predicate selected nodes for task",
		"task", fmt.Sprintf("%s/%s", reclaimor.Namespace, reclaimor.Name),
		"selectedNodes", len(predicateNodes))

	limits := utils.CandidateLimits{
		WorkerNum:                   ra.topologyAwareReclaimWorkerNum,
		MinCandidateNodesPercentage: ra.minCandidateNodesPercentage,
		MinCandidateNodesAbsolute:   ra.minCandidateNodesAbsolute,
		MaxCandidateNodesAbsolute:   ra.maxCandidateNodesAbsolute,
	}
	opts := utils.SelectVictimsOptions{
		// Cross-queue reclaim: victim removal does not free reclaimor queue quota.
		CheckAllocatable: false,
		ActionName:       ra.Name(),
	}

	filter := func(task *api.TaskInfo) bool {
		return isReclaimVictimTask(ssn, task, job)
	}

	collectVictims := func(initiator *api.TaskInfo, candidates []*api.TaskInfo) []*api.TaskInfo {
		return ssn.Reclaimable(initiator, candidates)
	}

	assigned, err := utils.RunTopologyAwareEviction(ssn, stmt, reclaimor, currentQueue, predicateNodes, limits, opts, filter, collectVictims, ra.Name())
	if err != nil {
		klog.V(3).Infof("topologyAwareReclaim failed for task %s/%s: %v", reclaimor.Namespace, reclaimor.Name, err)
		return false, err
	}
	if assigned {
		klog.V(3).Infof("topologyAwareReclaim succeeded for task %s/%s", reclaimor.Namespace, reclaimor.Name)
	}
	return assigned, nil
}
