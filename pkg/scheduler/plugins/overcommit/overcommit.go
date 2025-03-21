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

package overcommit

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName is name of plugin
	PluginName = "overcommit"
	// overCommitFactor is resource overCommit factor for enqueue action
	// It determines the number of `pending` pods that the scheduler will tolerate
	// when the resources of the cluster is insufficient
	overCommitFactor = "overcommit-factor"
	// defaultOverCommitFactor defines the default overCommit resource factor for enqueue action
	defaultOverCommitFactor = 1.2
)

type overcommitPlugin struct {
	// Arguments given for the plugin
	pluginArguments  framework.Arguments
	totalResource    *api.Resource
	idleResource     *api.Resource
	inqueueResource  *api.Resource
	overCommitFactor float64
}

// New function returns overcommit plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &overcommitPlugin{
		pluginArguments:  arguments,
		totalResource:    api.EmptyResource(),
		idleResource:     api.EmptyResource(),
		inqueueResource:  api.EmptyResource(),
		overCommitFactor: defaultOverCommitFactor,
	}
}

func (op *overcommitPlugin) Name() string {
	return PluginName
}

/*
User should give overcommit-factor through overcommit plugin arguments as format below:

actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: overcommit
    arguments:
    overcommit-factor: 1.0
*/
func (op *overcommitPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter overcommit plugin ...")
	defer klog.V(5).Infof("Leaving overcommit plugin.")

	op.pluginArguments.GetFloat64(&op.overCommitFactor, overCommitFactor)
	if op.overCommitFactor < 1.0 {
		klog.Warningf("Invalid input %f for overcommit-factor, reason: overcommit-factor cannot be less than 1,"+
			" using default value: %f.", op.overCommitFactor, defaultOverCommitFactor)
		op.overCommitFactor = defaultOverCommitFactor
	}

	op.totalResource.Add(ssn.TotalResource)
	// calculate idle resources of total cluster, overcommit resources included
	used := api.EmptyResource()
	for _, node := range ssn.Nodes {
		used.Add(node.Used)
	}
	op.idleResource = op.totalResource.Clone().Multi(op.overCommitFactor).SubWithoutAssert(used)

	for _, job := range ssn.Jobs {
		// calculate inqueue job resources
		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue && job.PodGroup.Spec.MinResources != nil {
			// deduct the resources of scheduling gated tasks in a job when calculating inqueued resources
			// so that it will not block other jobs from being inqueued.
			op.inqueueResource.Add(job.DeductSchGatedResources(job.GetMinResources()))
			continue
		}
		// calculate inqueue resource for running jobs
		// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
		// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
		if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
			job.PodGroup.Spec.MinResources != nil &&
			int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			inqueued := util.GetInqueueResource(job, job.Allocated)
			op.inqueueResource.Add(job.DeductSchGatedResources(inqueued))
		}
	}

	ssn.AddJobEnqueueableFn(op.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		idle := op.idleResource
		inqueue := api.EmptyResource()
		inqueue.Add(op.inqueueResource)
		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("Job <%s/%s> is bestEffort, permit to be inqueue.", job.Namespace, job.Name)
			return util.Permit
		}

		//TODO: if allow 1 more job to be inqueue beyond overcommit-factor, large job may be inqueue and create pods
		jobMinReq := job.GetMinResources()
		if inqueue.Add(jobMinReq).LessEqualWithDimension(idle, jobMinReq) { // only compare the requested resource
			klog.V(4).Infof("Sufficient resources, permit job <%s/%s> to be inqueue", job.Namespace, job.Name)
			return util.Permit
		}
		klog.V(4).Infof("Resource in cluster is overused, reject job <%s/%s> to be inqueue",
			job.Namespace, job.Name)
		ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnenqueueable), "resource in cluster is overused")
		job.JobFitErrors = "resource in cluster is overused"
		return util.Reject
	})

	ssn.AddJobEnqueuedFn(op.Name(), func(obj interface{}) {
		job := obj.(*api.JobInfo)
		if job.PodGroup.Spec.MinResources == nil {
			return
		}
		jobMinReq := job.GetMinResources()
		op.inqueueResource.Add(job.DeductSchGatedResources(jobMinReq))
	})
}

func (op *overcommitPlugin) OnSessionClose(ssn *framework.Session) {
	op.totalResource = nil
	op.idleResource = nil
	op.inqueueResource = nil
}
