/*
Copyright 2021 The Volcano Authors.

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

package tasktopology

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

type taskTopologyPlugin struct {
	arguments framework.Arguments

	weight   int
	managers map[api.JobID]*JobManager
}

// New function returns taskTopologyPlugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &taskTopologyPlugin{
		arguments: arguments,

		weight:   calculateWeight(arguments),
		managers: make(map[api.JobID]*JobManager),
	}
}

func (p *taskTopologyPlugin) Name() string {
	return PluginName
}

// TaskOrderFn returns -1 to make l prior to r.
//
// for example:
// A:
//  | bucket1   | bucket2   | out of bucket
//  | a1 a3     | a2        | a4
// B:
//  | bucket1   | out of bucket
//  | b1 b2     | b3
// the right task order should be:
//   a1 a3 a2 b1 b2 a4 b3
func (p *taskTopologyPlugin) TaskOrderFn(l interface{}, r interface{}) int {
	lv, ok := l.(*api.TaskInfo)
	if !ok {
		klog.Errorf("Object is not a taskinfo")
	}
	rv, ok := r.(*api.TaskInfo)
	if !ok {
		klog.Errorf("Object is not a taskinfo")
	}

	lvJobManager := p.managers[lv.Job]
	rvJobManager := p.managers[rv.Job]

	var lvBucket, rvBucket *Bucket
	if lvJobManager != nil {
		lvBucket = lvJobManager.GetBucket(lv)
	} else {
		klog.V(4).Infof("No job manager for job <ID: %s>, do not return task order.", lv.Job)
		return 0
	}
	if rvJobManager != nil {
		rvBucket = rvJobManager.GetBucket(rv)
	} else {
		klog.V(4).Infof("No job manager for job <ID: %s>, do not return task order.", rv.Job)
		return 0
	}

	// the one have bucket would always prior to another
	lvInBucket := lvBucket != nil
	rvInBucket := rvBucket != nil
	if lvInBucket != rvInBucket {
		if lvInBucket {
			return -1
		}
		return 1
	}

	// comparison between job is not the duty of this plugin
	if lv.Job != rv.Job {
		return 0
	}

	// task out of bucket have no order
	if !lvInBucket && !rvInBucket {
		return 0
	}

	// the big bucket should prior to small one
	lvHasTask := len(lvBucket.tasks)
	rvHasTask := len(rvBucket.tasks)
	if lvHasTask != rvHasTask {
		if lvHasTask > rvHasTask {
			return -1
		}
		return 1
	}

	lvBucketIndex := lvBucket.index
	rvBucketIndex := rvBucket.index
	// in the same bucket, the affinityOrder is ok
	if lvBucketIndex == rvBucketIndex {
		affinityOrder := lvJobManager.taskAffinityOrder(lv, rv)
		return -affinityOrder
	}

	// the old bucket should prior to young one
	if lvBucketIndex < rvBucketIndex {
		return -1
	}
	return 1
}

func (p *taskTopologyPlugin) calcBucketScore(task *api.TaskInfo, node *api.NodeInfo) (int, *JobManager, error) {
	// task could never fits the node
	maxResource := node.Idle.Clone().Add(node.Releasing)
	if req := task.Resreq; req != nil && maxResource.LessPartly(req, api.Zero) {
		return 0, nil, nil
	}

	jobManager, hasManager := p.managers[task.Job]
	if !hasManager {
		return 0, nil, nil
	}

	bucket := jobManager.GetBucket(task)
	// task out of bucket
	if bucket == nil {
		return 0, jobManager, nil
	}

	// 1. bound task in bucket is the base score of this node
	score := bucket.node[node.Name]

	// 2. task inter/self anti-affinity should be calculated
	if nodeTaskSet := jobManager.nodeTaskSet[node.Name]; nodeTaskSet != nil {
		taskName := getTaskName(task)
		affinityScore := jobManager.checkTaskSetAffinity(taskName, nodeTaskSet, true)
		if affinityScore < 0 {
			score += affinityScore
		}
	}
	klog.V(4).Infof("task %s/%s, node %s, additional score %d, task %d",
		task.Namespace, task.Name, node.Name, score, len(bucket.tasks))

	// 3. the other tasks in bucket take into considering
	score += len(bucket.tasks)
	if bucket.request == nil || bucket.request.LessEqual(maxResource, api.Zero) {
		return score, jobManager, nil
	}

	remains := bucket.request.Clone()
	// randomly (by map) take out task to make the bucket fits the node
	for bucketTaskID, bucketTask := range bucket.tasks {
		// current task should kept in bucket
		if bucketTaskID == task.Pod.UID || bucketTask.Resreq == nil {
			continue
		}
		remains.Sub(bucketTask.Resreq)
		score--
		if remains.LessEqual(maxResource, api.Zero) {
			break
		}
	}
	// here, the bucket remained request will always fit the maxResource
	return score, jobManager, nil
}

func (p *taskTopologyPlugin) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
	score, jobManager, err := p.calcBucketScore(task, node)
	if err != nil {
		return 0, err
	}
	fScore := float64(score * p.weight)
	if jobManager != nil && jobManager.bucketMaxSize != 0 {
		fScore = fScore * float64(v1alpha1.MaxNodeScore) / float64(jobManager.bucketMaxSize)
	}
	klog.V(4).Infof("task %s/%s at node %s has bucket score %d, score %f",
		task.Namespace, task.Name, node.Name, score, fScore)
	return fScore, nil
}

func (p *taskTopologyPlugin) AllocateFunc(event *framework.Event) {
	task := event.Task

	jobManager, hasManager := p.managers[task.Job]
	if !hasManager {
		return
	}
	jobManager.TaskBound(task)
}

func (p *taskTopologyPlugin) initBucket(ssn *framework.Session) {
	for jobID, job := range ssn.Jobs {
		if noPendingTasks(job) {
			klog.V(4).Infof("No pending tasks in job <%s/%s> by plugin %s.",
				job.Namespace, job.Name, PluginName)
			continue
		}

		jobTopology, err := readTopologyFromPgAnnotations(job)
		if err != nil {
			klog.V(4).Infof("Failed to read task topology from job <%s/%s> annotations, error: %s.",
				job.Namespace, job.Name, err.Error())
			continue
		}
		if jobTopology == nil {
			continue
		}

		manager := NewJobManager(jobID)
		manager.ApplyTaskTopology(jobTopology)
		manager.ConstructBucket(job.Tasks)

		p.managers[job.UID] = manager
	}
}

func affinityCheck(job *api.JobInfo, affinity [][]string) error {
	if job == nil || affinity == nil {
		return fmt.Errorf("empty input, job: %v, affinity: %v", job, affinity)
	}

	var taskNumber = len(job.Tasks)
	var taskRef = make(map[string]bool, taskNumber)
	for _, task := range job.Tasks {
		tmpStrings := strings.Split(task.Name, "-")
		if _, exist := taskRef[tmpStrings[len(tmpStrings)-2]]; !exist {
			taskRef[tmpStrings[len(tmpStrings)-2]] = true
		}
	}

	for _, aff := range affinity {
		affTasks := make(map[string]bool, len(aff))
		for _, task := range aff {
			if len(task) == 0 {
				continue
			}
			if _, exist := taskRef[task]; !exist {
				return fmt.Errorf("task %s do not exist in job <%s/%s>", task, job.Namespace, job.Name)
			}
			if _, exist := affTasks[task]; exist {
				return fmt.Errorf("task %s is duplicated in job <%s/%s>", task, job.Namespace, job.Name)
			}
			affTasks[task] = true
		}
	}

	return nil
}

func splitAnnotations(job *api.JobInfo, annotation string) ([][]string, error) {
	affinityStr := strings.Split(annotation, ";")
	if len(affinityStr) == 0 {
		return nil, nil
	}
	var affinity = make([][]string, len(affinityStr))
	for i, str := range affinityStr {
		affinity[i] = strings.Split(str, ",")
	}
	if err := affinityCheck(job, affinity); err != nil {
		klog.V(4).Infof("Job <%s/%s> affinity key invalid: %s.",
			job.Namespace, job.Name, err.Error())
		return nil, err
	}
	return affinity, nil
}

func readTopologyFromPgAnnotations(job *api.JobInfo) (*TaskTopology, error) {
	jobAffinityStr, affinityExist := job.PodGroup.Annotations[JobAffinityAnnotations]
	jobAntiAffinityStr, antiAffinityExist := job.PodGroup.Annotations[JobAntiAffinityAnnotations]
	taskOrderStr, taskOrderExist := job.PodGroup.Annotations[TaskOrderAnnotations]

	if !(affinityExist || antiAffinityExist || taskOrderExist) {
		return nil, nil
	}

	var jobTopology = TaskTopology{
		Affinity:     nil,
		AntiAffinity: nil,
		TaskOrder:    nil,
	}

	if affinityExist {
		affinities, err := splitAnnotations(job, jobAffinityStr)
		if err != nil {
			klog.V(4).Infof("Job <%s/%s> affinity key invalid: %s.",
				job.Namespace, job.Name, err.Error())
			return nil, err
		}
		jobTopology.Affinity = affinities
	}

	if antiAffinityExist {
		affinities, err := splitAnnotations(job, jobAntiAffinityStr)
		if err != nil {
			klog.V(4).Infof("Job <%s/%s> anti affinity key invalid: %s.",
				job.Namespace, job.Name, err.Error())
			return nil, err
		}
		jobTopology.AntiAffinity = affinities
	}

	if taskOrderExist {
		jobTopology.TaskOrder = strings.Split(taskOrderStr, ",")
		if err := affinityCheck(job, [][]string{jobTopology.TaskOrder}); err != nil {
			klog.V(4).Infof("Job <%s/%s> task order key invalid: %s.",
				job.Namespace, job.Name, err.Error())
			return nil, err
		}
	}

	return &jobTopology, nil
}

func (p *taskTopologyPlugin) OnSessionOpen(ssn *framework.Session) {
	start := time.Now()
	klog.V(3).Infof("start to init task topology plugin, weight[%d], defined order %v", p.weight, affinityPriority)

	p.initBucket(ssn)

	ssn.AddTaskOrderFn(p.Name(), p.TaskOrderFn)

	ssn.AddNodeOrderFn(p.Name(), p.NodeOrderFn)

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: p.AllocateFunc,
	})

	klog.V(3).Infof("finished to init task topology plugin, using time %v", time.Since(start))
}

func (p *taskTopologyPlugin) OnSessionClose(ssn *framework.Session) {
	p.managers = nil
}
