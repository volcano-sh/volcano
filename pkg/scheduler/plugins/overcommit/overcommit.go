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
	"time"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
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
	idleResource     *api.Resource
	inqueueResource  *api.Resource
	overCommitFactor float64
}

// New function returns overcommit plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &overcommitPlugin{
		pluginArguments:  arguments,
		idleResource:     api.EmptyResource(),
		overCommitFactor: defaultOverCommitFactor,
	}
}

func (op *overcommitPlugin) Name() string {
	return PluginName
}

func (op *overcommitPlugin) OnSessionOpen(ssn *framework.Session) {
	overcommitStartTime := time.Now().UnixNano()
	op.pluginArguments.GetFloat64(&op.overCommitFactor, overCommitFactor)
	if op.overCommitFactor < 1.0 {
		klog.Warningf("invalid input %f for overcommit-factor, reason: overcommit-factor cannot be less than 1,"+
			" using default value: %f", op.overCommitFactor, defaultOverCommitFactor)
		op.overCommitFactor = defaultOverCommitFactor
	}
	klog.V(4).Infof("overcommit plugin starts, overCommitFactor: %f", op.overCommitFactor)
	defer klog.V(4).Infof("overcommit plugin finishes, execution time: %dns",
		time.Now().UnixNano()-overcommitStartTime)

	// calculate idle resources of total cluster, overcommit resources included
	total := api.EmptyResource()
	used := api.EmptyResource()
	for _, node := range ssn.Nodes {
		total.Add(node.Allocatable)
		used.Add(node.Used)
	}
	op.idleResource = total.Clone().Multi(op.overCommitFactor).Sub(used)

	// calculate inqueue job resources
	inqueue := api.EmptyResource()
	for _, job := range ssn.Jobs {
		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			inqueue.Add(api.NewResource(*job.PodGroup.Spec.MinResources))
		}
	}
	op.inqueueResource = inqueue.Clone()

	ssn.AddJobEnqueueableFn(op.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		idle := op.idleResource
		inqueue := api.EmptyResource()
		inqueue.Add(op.inqueueResource)
		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("job <%s/%s> is bestEffort, allow it be inqueue", job.Namespace, job.Name)
			return 0
		}

		//TODO: if allow 1 more job to be inqueue beyond overcommit-factor, large job may be inqueue and create pods
		jobMinReq := api.NewResource(*job.PodGroup.Spec.MinResources)
		if inqueue.Add(jobMinReq).LessEqual(idle) {
			klog.V(4).Infof("sufficient resources, allow job <%s/%s> be inqueue", job.Namespace, job.Name)
			op.inqueueResource.Add(jobMinReq)
			return 0
		}
		klog.V(4).Infof("idle resource in cluster is overused, ignore job <%s/%s>",
			job.Namespace, job.Name)
		return -1
	})
}

func (op *overcommitPlugin) OnSessionClose(ssn *framework.Session) {
	op.idleResource = nil
	op.inqueueResource = nil
}
