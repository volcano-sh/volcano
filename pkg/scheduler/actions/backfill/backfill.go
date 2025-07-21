/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced task selection with queue-based prioritization and systematic task ordering
- Added predicate error caching support for filtering out failed nodes in preempt/reclaim
- Added PrePredicate validation step and improved node selection with comprehensive predicate validation and scoring

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

package backfill

import (
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct {
	enablePredicateErrorCache bool
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache: true, // default to enable it
	}
}

func (backfill *Action) Name() string {
	return "backfill"
}

func (backfill *Action) Initialize() {}

func (backfill *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, backfill.Name())
	arguments.GetBool(&backfill.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
}

func (backfill *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Backfill ...")
	defer klog.V(5).Infof("Leaving Backfill ...")

	backfill.parseArguments(ssn)

	predicateFunc := ssn.PredicateForAllocateAction

	// TODO (k82cn): When backfill, it's also need to balance between Queues.
	pendingTasks := backfill.pickUpPendingTasks(ssn)
	for _, task := range pendingTasks {
		job := ssn.Jobs[task.Job]
		ph := util.NewPredicateHelper()
		fe := api.NewFitErrors()

		if err := ssn.PrePredicateFn(task); err != nil {
			klog.V(3).Infof("PrePredicate for task %s/%s failed in backfill for: %v", task.Namespace, task.Name, err)
			for _, ni := range ssn.Nodes {
				fe.SetNodeError(ni.Name, err)
			}
			job.NodesFitErrors[task.UID] = fe
			continue
		}

		predicateNodes, fitErrors := ph.PredicateNodes(task, ssn.NodeList, predicateFunc, backfill.enablePredicateErrorCache)
		if len(predicateNodes) == 0 {
			job.NodesFitErrors[task.UID] = fitErrors
			continue
		}

		node := predicateNodes[0]
		if len(predicateNodes) > 1 {
			nodeScores := util.PrioritizeNodes(task, predicateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)
			node = ssn.BestNodeFn(task, nodeScores)
			if node == nil {
				node, _ = util.SelectBestNodeAndScore(nodeScores)
			}
		}

		klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, node.Name)
		if err := ssn.Allocate(task, node); err != nil {
			klog.Errorf("Failed to bind Task %v on %v in Session %v", task.UID, node.Name, ssn.UID)
			fe.SetNodeError(node.Name, err)
			job.NodesFitErrors[task.UID] = fe
			continue
		}

		metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
		metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())

		// TODO (k82cn): backfill for other case.
	}
}

func (backfill *Action) UnInitialize() {}

func (backfill *Action) pickUpPendingTasks(ssn *framework.Session) []*api.TaskInfo {
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	jobs := map[api.QueueID]*util.PriorityQueue{}
	tasks := map[api.JobID]*util.PriorityQueue{}
	var pendingTasks []*api.TaskInfo
	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip backfill, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		queue, found := ssn.Queues[job.Queue]
		if !found {
			continue
		}

		for _, task := range job.TaskStatusIndex[api.Pending] {
			if !task.BestEffort {
				continue
			}

			if task.SchGated {
				continue
			}

			if _, existed := tasks[job.UID]; !existed {
				tasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			}
			tasks[job.UID].Push(task)
		}

		for _, task := range job.TaskStatusIndex[api.Pipelined] {
			if !task.BestEffort {
				continue
			}

			stmt := framework.NewStatement(ssn)
			err := stmt.UnPipeline(task)
			if err != nil {
				klog.Errorf("Failed to unpipeline task: %s", err.Error())
				continue
			}
			if _, existed := tasks[job.UID]; !existed {
				tasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			}
			tasks[job.UID].Push(task)
		}

		if _, existed := tasks[job.UID]; !existed {
			continue
		}

		if _, existed := jobs[queue.UID]; !existed {
			queues.Push(queue)
			jobs[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
		}
		jobs[job.Queue].Push(job)
	}

	for !queues.Empty() {
		queue, ok := queues.Pop().(*api.QueueInfo)
		if !ok {
			klog.V(3).Infof("QueueInfo transition failed, ignore it.")
			continue
		}
		for !jobs[queue.UID].Empty() {
			job, ok := jobs[queue.UID].Pop().(*api.JobInfo)
			if !ok {
				klog.Errorf("JobInfo transition failed, ignore it.")
				continue
			}
			for !tasks[job.UID].Empty() {
				pendingTasks = append(pendingTasks, tasks[job.UID].Pop().(*api.TaskInfo))
			}
		}
	}
	return pendingTasks
}
