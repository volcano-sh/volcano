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

package preempt

import (
	"fmt"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

func (pmpt *Action) topologyAwarePreempt(
	ssn *framework.Session,
	stmt *framework.Statement,
	preemptor *api.TaskInfo,
	filter func(*api.TaskInfo) bool,
	predicateNodes []*api.NodeInfo,
) (bool, error) {
	job, found := ssn.Jobs[preemptor.Job]
	if !found {
		return false, fmt.Errorf("not found Job %s in Session", preemptor.Job)
	}
	currentQueue := ssn.Queues[job.Queue]

	limits := utils.CandidateLimits{
		WorkerNum:                   pmpt.topologyAwarePreemptWorkerNum,
		MinCandidateNodesPercentage: pmpt.minCandidateNodesPercentage,
		MinCandidateNodesAbsolute:   pmpt.minCandidateNodesAbsolute,
		MaxCandidateNodesAbsolute:   pmpt.maxCandidateNodesAbsolute,
	}
	opts := utils.SelectVictimsOptions{
		CheckAllocatable: true,
		ActionName:       pmpt.Name(),
	}
	collectVictims := func(initiator *api.TaskInfo, candidates []*api.TaskInfo) []*api.TaskInfo {
		victims := ssn.Preemptable(initiator, candidates)
		metrics.UpdatePreemptionVictimsCount(len(victims))
		return victims
	}

	assigned, err := utils.RunTopologyAwareEviction(ssn, stmt, preemptor, currentQueue, predicateNodes, limits, opts, filter, collectVictims, pmpt.Name())
	if err != nil {
		klog.V(3).Infof("topologyAwarePreempt failed for Task <%s/%s>: %v", preemptor.Namespace, preemptor.Name, err)
		return false, err
	}
	if assigned {
		metrics.RegisterPreemptionAttempts()
	}
	return assigned, nil
}
