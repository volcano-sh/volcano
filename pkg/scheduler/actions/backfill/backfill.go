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

type backfillContext struct {
	queues      *util.PriorityQueue                 // queue of *api.QueueInfo
	jobsByQueue map[api.QueueID]*util.PriorityQueue // queue of *api.JobInfo
	tasksByJob  map[api.JobID]*util.PriorityQueue   // queue of *api.TaskInfo
}

type Action struct {
	session                   *framework.Session
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
	backfill.session = ssn

	actx := backfill.buildBackfillContext()
	backfill.allocateResources(actx)
}

func (backfill *Action) buildBackfillContext() *backfillContext {
	ssn := backfill.session

	actx := &backfillContext{
		queues:      util.NewPriorityQueue(ssn.QueueOrderFn),
		jobsByQueue: map[api.QueueID]*util.PriorityQueue{},
		tasksByJob:  map[api.JobID]*util.PriorityQueue{},
	}

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

		tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
		for _, task := range job.TaskStatusIndex[api.Pending] {
			if !task.BestEffort || task.SchGated {
				continue
			}
			tasks.Push(task)
		}

		for _, task := range job.TaskStatusIndex[api.Pipelined] {
			if !task.BestEffort {
				continue
			}
			stmt := framework.NewStatement(ssn)
			if err := stmt.UnPipeline(task); err != nil {
				klog.Errorf("Failed to unpipeline task: %s", err.Error())
				continue
			}
			tasks.Push(task)
		}

		if tasks.Empty() {
			continue
		}

		if _, existed := actx.jobsByQueue[queue.UID]; !existed {
			actx.jobsByQueue[queue.UID] = util.NewPriorityQueue(ssn.JobOrderFn)
			actx.queues.Push(queue)
		}
		actx.jobsByQueue[queue.UID].Push(job)
		actx.tasksByJob[job.UID] = tasks
	}

	return actx
}

func (backfill *Action) allocateResources(actx *backfillContext) {
	ssn := backfill.session

	queues := actx.queues
	for !queues.Empty() {
		queue := queues.Pop().(*api.QueueInfo)

		jobs, found := actx.jobsByQueue[queue.UID]
		if !found || jobs.Empty() {
			continue
		}

		job := jobs.Pop().(*api.JobInfo)
		tasks, found := actx.tasksByJob[job.UID]
		if !found || tasks.Empty() {
			queues.Push(queue)
			continue
		}

		stmt := framework.NewStatement(ssn)
		backfill.allocateTasksForJob(stmt, queue, job, tasks)

		if len(stmt.Operations()) > 0 && ssn.JobReady(job) {
			stmt.Commit()
			if !tasks.Empty() {
				jobs.Push(job)
			}
		} else {
			stmt.Discard()
		}

		queues.Push(queue)
	}
}

func (backfill *Action) allocateTasksForJob(stmt *framework.Statement, queue *api.QueueInfo, job *api.JobInfo, tasks *util.PriorityQueue) {
	ssn := backfill.session
	predicateFunc := ssn.PredicateForAllocateAction
	ph := util.NewPredicateHelper()

	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)

		fe := api.NewFitErrors()
		if err := ssn.PrePredicateFn(task); err != nil {
			klog.V(3).Infof("PrePredicate for task %s/%s failed in backfill for: %v", task.Namespace, task.Name, err)
			for _, ni := range ssn.Nodes {
				fe.SetNodeError(ni.Name, err)
			}
			job.NodesFitErrors[task.UID] = fe
			continue
		}

		predicateNodes, fitErrors := ph.PredicateNodes(task, ssn.NodeList, predicateFunc, backfill.enablePredicateErrorCache, ssn.NodesInShard)
		if len(predicateNodes) == 0 {
			job.NodesFitErrors[task.UID] = fitErrors
			continue
		}

		node := predicateNodes[0]
		if len(predicateNodes) > 1 {
			candidateNodes := util.GetPredicatedNodeByShard(predicateNodes, ssn.NodesInShard)
			for _, nodes := range candidateNodes {
				nodeScores := util.PrioritizeNodes(task, nodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)
				node = ssn.BestNodeFn(task, nodeScores)
				if node == nil {
					node, _ = util.SelectBestNodeAndScore(nodeScores)
				}
				if node != nil {
					break
				}
			}
		}

		klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, node.Name)
		if err := stmt.Allocate(task, node); err != nil {
			klog.Errorf("Failed to bind Task %v on %v in Session %v", task.UID, node.Name, ssn.UID)
			if rollbackErr := stmt.UnAllocate(task); rollbackErr != nil {
				klog.Errorf("Failed to unallocate Task %v on %v in Session %v for %v", task.UID, node.Name, ssn.UID, rollbackErr)
			}
			fe.SetNodeError(node.Name, err)
			job.NodesFitErrors[task.UID] = fe
			continue
		}

		metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
		metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())

		if ssn.JobReady(job) {
			break
		}
	}
}

func (backfill *Action) UnInitialize() {}
