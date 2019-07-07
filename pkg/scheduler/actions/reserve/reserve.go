/*
Copyright 2017 The Kubernetes Authors.

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

package reserve

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/golang/glog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"

	"k8s.io/client-go/util/workqueue"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
)

var (
	// defaultReservedNodePercent defines the proportion of reserved nodes
	defaultReservedNodePercent = 50
	// defaultStarvingTimeThreshold defines the default time threshold
	// used to determine if the job is going to be starved
	// in seconds
	defaultStarvingTimeThreshold = 1
	// defaultMinimumNodeScore defines the minimum node score
	defaultMinimumNodeScore = 0
)

type reserveAction struct {
	ssn *framework.Session

	arguments framework.Arguments
}

// New init a reserveAction struct
func New(args framework.Arguments) framework.Action {
	return &reserveAction{
		arguments: args,
	}
}

// Name return name of reserveAction
func (ra *reserveAction) Name() string {
	return "reserve"
}

// Initialize init reserveAction
func (ra *reserveAction) Initialize() {}

// Execute exec reserveAction
func (ra *reserveAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Reserve ...")
	defer glog.V(3).Infof("Leaving Reserve ...")

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	reservedNodePercent := defaultReservedNodePercent
	ra.arguments.GetInt(&reservedNodePercent, conf.ReservedNodePercent)

	starvingJobTimeThreshold := defaultStarvingTimeThreshold
	ra.arguments.GetInt(&starvingJobTimeThreshold, conf.StarvingJobTimeThreshold)

	boundary := int(math.Ceil(float64(len(ssn.Nodes)) * float64(reservedNodePercent) / 100))
	reservedNodes := map[string]int{}

	for _, job := range ssn.Jobs {
		if job.PodGroup.Status.Phase == api.PodGroupPending {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			glog.V(4).Infof("Job <%s/%s> Queue <%s> skip reserve, reason: %v, message %v",
				job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if !isJobStarving(ssn, job, starvingJobTimeThreshold) {
			continue
		}

		queue, found := ssn.Queues[job.Queue]
		if found {
			queues.Push(queue)
		} else {
			glog.Warningf("Skip adding job <%s/%s> because its queue %s is not found",
				job.Namespace, job.Name, job.Queue)
			continue
		}

		if _, found := jobsMap[job.Queue]; !found {
			jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
		}

		glog.V(3).Infof("Added job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
		jobsMap[job.Queue].Push(job)
	}

	glog.V(3).Infof("Try to reserve resource to %d Queues", len(jobsMap))

	pendingTasks := map[api.JobID]*util.PriorityQueue{}

	allNodes := util.GetNodeList(ssn.Nodes)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		if !task.InitResreq.LessEqual(node.Allocatable.Clone()) {
			return fmt.Errorf("task <%s/%s> ResourceFit failed on node <%s>",
				task.Namespace, task.Name, node.Name)
		}

		return ssn.PredicateFn(task, node)
	}

	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)
		if ssn.Overused(queue) {
			glog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			glog.V(4).Infof("Can not find jobs for queue %s.", queue.Name)
			continue
		}
		glog.V(3).Infof("Try to reserve resource to jobs in Queue <%s>", queue.Name)

		job := jobs.Pop().(*api.JobInfo)
		if _, found := pendingTasks[job.UID]; !found {
			tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				if task.Resreq.IsEmpty() {
					glog.V(4).Infof("Task <%s/%s> is BestEffort task, skip it.",
						task.Namespace, task.Name)
					continue
				}

				tasks.Push(task)
			}
			pendingTasks[job.UID] = tasks
		}
		tasks := pendingTasks[job.UID]

		glog.V(3).Infof("Try to reserve resource to %d tasks of job <%s/%s>",
			tasks.Len(), job.Namespace, job.Name)

		stmt := ssn.Statement()
		for !tasks.Empty() {
			task := tasks.Pop().(*api.TaskInfo)
			predicateNodes, _ := util.PredicateNodes(task, allNodes, predicateFn)
			if len(predicateNodes) == 0 {
				glog.V(3).Infof("Predicate nodes for task <%s:%s> failed", task.Namespace, task.Name)
				continue
			}

			hostList := calculateNodeScore(task, predicateNodes)
			reserved := false
			for _, node := range hostList {
				if node.Score == defaultMinimumNodeScore {
					break
				}

				node, ok := ssn.Nodes[node.Host]
				if !ok {
					continue
				}

				if !node.Reserved.Clone().Add(task.InitResreq).LessEqual(node.Allocatable) {
					glog.V(3).Infof("Node <%s> allocatable resource can not satisfy the resource request "+
						"of the task <%s/%s>", node.Name, task.Namespace, task.Name)
					continue
				}

				if _, ok := reservedNodes[node.Name]; !ok {
					if len(reservedNodes) >= boundary {
						glog.V(3).Infof("Reserved node number has exceeded the upper limit <%d>.", boundary)
						break
					}
				}

				glog.V(3).Infof("Try to reserve resources for task <%s/%s> on node <%s>",
					task.Namespace, task.Name, node.Name)
				if err := stmt.Reserve(task, node.Name); err != nil {
					glog.Errorf("Failed to reserve resource for task <%s/%s> on node <%s> in Session %s, for: %v",
						task.Namespace, task.Name, node.Name, ssn.UID, err)
					continue
				}

				reservedNodes[node.Name]++
				reserved = true
				break

			}

			if ssn.JobReady(job) || ssn.JobConditionReady(job) {
				glog.V(3).Infof("Job <%s/%s> can be ready after reserve resources, commit it",
					job.Namespace, job.Name)
				stmt.Commit()
				if reserved {
					glog.V(3).Infof("Job <%s/%s> need to be re-push to the queue", job.Namespace, job.Name)
					jobs.Push(job)
				}

				break
			}
		}

		if !ssn.JobReady(job) && !ssn.JobConditionReady(job) {
			glog.V(3).Infof("Job <%s/%s> can not be ready after reserve resources, discard reservation",
				job.Namespace, job.Name)
			stmt.Discard()
		}

		// Added Queue back until no job in Queue.
		queues.Push(queue)
	}
}

// isJobStarving return whether the job is going to be starved
// if job is ready, do not think it might be starved
// if the task under the job has been reserved resources, and under this premise
// the job can be ready, the job will not be starved
// if the pending time of job is greater that the threshold, the job is considered to be starved
func isJobStarving(ssn *framework.Session, job *api.JobInfo, starvingJobTimeThreshold int) bool {
	if ssn.JobReady(job) {
		return false
	}

	if ssn.JobConditionReady(job) {
		return false
	}

	if time.Since(job.CreationTimestamp.Time) > time.Duration(starvingJobTimeThreshold)*time.Second {
		return true
	}

	return false
}

// calculateNodeScore calculate score for node, the score of the node is composed of the scores of cpu, memory and
// scalar resource, the calculation formula is
//
//           cpuScore + memScore
//           ------------------- + 2 * scalarScore
//                    2
//   score = -------------------------------------
//                             3
//
func calculateNodeScore(task *api.TaskInfo, nodes []*api.NodeInfo) schedulerapi.HostPriorityList {
	hostList := []schedulerapi.HostPriority{}
	var lock sync.Mutex

	calculateNode := func(i int) {
		node := nodes[i]
		allocatableResources := node.Allocatable.Clone()
		usedResources := node.Used.Clone()

		cpuScore, memScore, scalarScore := calculateResourceScore(task, usedResources, allocatableResources)

		nodeScore := ((cpuScore+memScore)/2 + 2*scalarScore) / 3
		glog.V(3).Infof("Node <%s> used resources cpu:memory <%g:%g> scalarResource <%v>, node allocatable "+
			"resources <%g:%g>, scalarResource <%v>. cpuScore <%g>, memoryScore <%g>, scalarScore <%g>, node score is <%g>.",
			node.Name, usedResources.MilliCPU, usedResources.Memory, usedResources.ScalarResources, task.InitResreq.MilliCPU,
			task.InitResreq.Memory, task.InitResreq.ScalarResources, cpuScore, memScore, scalarScore, nodeScore)

		lock.Lock()
		hostList = append(hostList, schedulerapi.HostPriority{
			Host:  node.Name,
			Score: int(nodeScore),
		})
		lock.Unlock()
	}

	workqueue.Parallelize(16, len(nodes), calculateNode)

	calculateNodeScoreReduce(hostList)

	return hostList
}

// calculateResourceScore calculate score based on cpu, memory and scalar resources
// for scalar resources, if the task does not request the corresponding resource, the corresponding
// score of resource is 0, the final calculated scalar resource score is the average of the various
// scalar resource scores
func calculateResourceScore(task *api.TaskInfo, used *api.Resource, allocatable *api.Resource) (float64, float64, float64) {
	cpuScore := calculateScore(used.MilliCPU, allocatable.MilliCPU)
	memScore := calculateScore(used.Memory, allocatable.Memory)

	var scalarScore float64
	scalarResourceNumber := 0
	for name, value := range allocatable.ScalarResources {
		var usedScalar float64
		// If resource was not used by task, score for resource is 0
		if _, ok := task.InitResreq.ScalarResources[name]; !ok {
			continue
		}

		if used, ok := used.ScalarResources[name]; ok {
			usedScalar = used
		} else {
			usedScalar = 0
		}

		scalarScore += calculateScore(usedScalar, value)
		scalarResourceNumber++
	}

	if scalarResourceNumber != 0 {
		scalarScore = scalarScore / float64(scalarResourceNumber)
	}

	return cpuScore, memScore, scalarScore
}

// calculateScore calculate score based on resources
// score ranged from 0 to 100, 0 is the minimum score, 100 is maximum score
// the higher the utilization rate of resources, the lower the score
func calculateScore(used float64, allocatable float64) float64 {
	if allocatable == 0 {
		return 0
	}

	if used > allocatable {
		return 0
	}

	score := used * 100 / allocatable

	return 100 - score
}

// calculateNodeScoreReduce re-order the score of the nodes, and set the score of the node to 10 or 0
// depending on whether the node is within the boundary
func calculateNodeScoreReduce(hostList schedulerapi.HostPriorityList) {
	num := hostList.Len()
	snapshotHost := make(schedulerapi.HostPriorityList, num)
	copy(snapshotHost, hostList)
	sort.Sort(snapshotHost)

	scoreMap := make(map[string]int)

	boundary := int(math.Ceil(float64(num) * float64(defaultReservedNodePercent) / 100))

	for i := 0; i < num-boundary; i++ {
		scoreMap[snapshotHost[i].Host] = defaultMinimumNodeScore
	}

	for i := num - 1; i >= num-boundary; i-- {
		if snapshotHost[i].Score != 0 {
			scoreMap[snapshotHost[i].Host] = 100
		} else {
			scoreMap[snapshotHost[i].Host] = defaultMinimumNodeScore
		}
	}

	for i := 0; i < num; i++ {
		hostList[i].Score = scoreMap[hostList[i].Host]
	}

	sort.Sort(sort.Reverse(hostList))

	return
}

// UnInitialize unInit reserveAction
func (ra *reserveAction) UnInitialize() {}
