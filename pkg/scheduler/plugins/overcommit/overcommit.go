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
	"strings"

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
	// This field is used as the default key in factorMaps
	overCommitFactor = "overcommit-factor"
	// defaultOverCommitFactor defines the default overCommit resource factor for enqueue action
	defaultOverCommitFactor = 1.2
)

const (
	// overCommitFactorPrefix is the prefix of resource overCommit factor
	// We use this prefix to segment the rules for custom resources
	// in the configuration file.
	overCommitFactorPrefix = "overcommit-factor."
)

// overcommitFactors defines the resource overCommit factors
type overcommitFactors struct {
	// factorMaps defines the resource overCommit factors
	// key: resource, example: "cpu", "memory", "ephemeral-storage", "nvidia.com/gpu"
	// value: overCommit factors
	// when initializing, we will store a default value into this map
	// key: "overcommit-factor", value: defaultOverCommitFactor
	factorMaps map[string]float64
}

type overcommitPlugin struct {
	// pluginArguments Arguments given for the plugin
	pluginArguments framework.Arguments
	totalResource   *api.Resource
	idleResource    *api.Resource
	inqueueResource *api.Resource
	// overCommitFactor is the different resource overCommit factors
	overCommitFactors *overcommitFactors
}

// New function returns overcommit plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &overcommitPlugin{
		pluginArguments: arguments,
		totalResource:   api.EmptyResource(),
		idleResource:    api.EmptyResource(),
		inqueueResource: api.EmptyResource(),
		overCommitFactors: &overcommitFactors{
			factorMaps: map[string]float64{
				overCommitFactor: defaultOverCommitFactor,
			},
		},
	}
}

func (op *overcommitPlugin) Name() string {
	return PluginName
}

/*
User should give overcommit factors through overcommit plugin arguments as format below:

Example:

actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: overcommit
    arguments:
    overcommit-factor.cpu: 1.2
    overcommit-factor.memory: 1.0
    overcommit-factor: 1.2
*/
func (op *overcommitPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter overcommit plugin ...")
	defer klog.V(5).Infof("Leaving overcommit plugin.")

	// parse plugin arguments
	op.parse()

	// validate plugin arguments
	op.validate()

	op.totalResource.Add(ssn.TotalResource)
	// calculate idle resources of total cluster, overcommit resources included
	used := api.EmptyResource()
	for _, node := range ssn.Nodes {
		used.Add(node.Used)
	}

	op.idleResource = op.totalResource.Clone().
		ScaleResourcesWithRatios(op.overCommitFactors.factorMaps, op.overCommitFactors.factorMaps[overCommitFactor]).SubWithoutAssert(used)

	for _, job := range ssn.Jobs {
		// calculate inqueue job resources
		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue && job.PodGroup.Spec.MinResources != nil {
			op.inqueueResource.Add(api.NewResource(*job.PodGroup.Spec.MinResources))
			continue
		}
		// calculate inqueue resource for running jobs
		// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
		// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
		if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
			job.PodGroup.Spec.MinResources != nil &&
			int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			inqueued := util.GetInqueueResource(job, job.Allocated)
			op.inqueueResource.Add(inqueued)
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
		jobMinReq := api.NewResource(*job.PodGroup.Spec.MinResources)
		if inqueue.Add(jobMinReq).LessEqualWithDimension(idle, jobMinReq) { // only compare the requested resource
			klog.V(4).Infof("Sufficient resources, permit job <%s/%s> to be inqueue", job.Namespace, job.Name)
			return util.Permit
		}
		klog.V(4).Infof("Resource in cluster is overused, reject job <%s/%s> to be inqueue",
			job.Namespace, job.Name)
		ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), "resource in cluster is overused")
		return util.Reject
	})

	ssn.AddJobEnqueuedFn(op.Name(), func(obj interface{}) {
		job := obj.(*api.JobInfo)
		if job.PodGroup.Spec.MinResources == nil {
			return
		}
		jobMinReq := api.NewResource(*job.PodGroup.Spec.MinResources)
		op.inqueueResource.Add(jobMinReq)
	})
}

func (op *overcommitPlugin) OnSessionClose(ssn *framework.Session) {
	op.totalResource = nil
	op.idleResource = nil
	op.inqueueResource = nil
}

// parseFactor iterates through the arguments map and extracts values based on the keys with specific prefixes.
// If a key matches overCommitFactor, its corresponding value is directly added to the target map.
// For keys starting with overCommitFactorPrefix,
// the suffix after the prefix is extracted and used as the key in the target map along with the corresponding value.
func (op *overcommitPlugin) parseFactor(arguments framework.Arguments, target map[string]float64) {
	for key, value := range arguments {
		switch v := value.(type) {
		case float64:
			if key == overCommitFactor {
				// If the key is equal to overCommitFactor,
				// directly add the value to the target map
				target[overCommitFactor] = v
			}

			if strings.HasPrefix(key, overCommitFactorPrefix) {
				// If the key starts with overCommitFactorPrefix
				// Extract the suffix after the prefix
				// Update target map with the extracted suffix and corresponding value
				suffix := strings.TrimPrefix(key, overCommitFactorPrefix)
				target[suffix] = v
			}
		case int:
			// Handle int values by converting them to float64
			floatValue := float64(v)
			if key == overCommitFactor {
				target[overCommitFactor] = floatValue
			}

			if strings.HasPrefix(key, overCommitFactorPrefix) {
				suffix := strings.TrimPrefix(key, overCommitFactorPrefix)
				target[suffix] = floatValue
			}
		default:
			// we should log the unexpected value type here to prevent panics
			klog.Warningf("Unexpected value type for key %s: %T\n", key, value)
		}
	}
}

func (op *overcommitPlugin) parse() {
	op.parseFactor(op.pluginArguments, op.overCommitFactors.factorMaps)
}

// validate is used to validate the input parameters,
// and if the input parameters are invalid, use the default value.
func (op *overcommitPlugin) validate() {
	for k, v := range op.overCommitFactors.factorMaps {
		if v < 1.0 {
			klog.Warningf("Invalid input %f for %v overcommit factor, reason: %v overcommit factor cannot be less than 1,"+
				" using default value: %f.", v, k, k, defaultOverCommitFactor)
			op.overCommitFactors.factorMaps[k] = defaultOverCommitFactor
		}
	}
}
