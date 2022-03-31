/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// AddJobOrderFn add job order function
func (ssn *Session) AddJobOrderFn(name string, cf api.CompareFn) {
	ssn.jobOrderFns[name] = cf
}

// AddQueueOrderFn add queue order function
func (ssn *Session) AddQueueOrderFn(name string, qf api.CompareFn) {
	ssn.queueOrderFns[name] = qf
}

// AddClusterOrderFn add queue order function
func (ssn *Session) AddClusterOrderFn(name string, qf api.CompareFn) {
	ssn.clusterOrderFns[name] = qf
}

// AddTaskOrderFn add task order function
func (ssn *Session) AddTaskOrderFn(name string, cf api.CompareFn) {
	ssn.taskOrderFns[name] = cf
}

// AddNamespaceOrderFn add namespace order function
func (ssn *Session) AddNamespaceOrderFn(name string, cf api.CompareFn) {
	ssn.namespaceOrderFns[name] = cf
}

// AddPreemptableFn add preemptable function
func (ssn *Session) AddPreemptableFn(name string, cf api.EvictableFn) {
	ssn.preemptableFns[name] = cf
}

// AddReclaimableFn add Reclaimable function
func (ssn *Session) AddReclaimableFn(name string, rf api.EvictableFn) {
	ssn.reclaimableFns[name] = rf
}

// AddJobReadyFn add JobReady function
func (ssn *Session) AddJobReadyFn(name string, vf api.ValidateFn) {
	ssn.jobReadyFns[name] = vf
}

// AddJobPipelinedFn add pipelined function
func (ssn *Session) AddJobPipelinedFn(name string, vf api.VoteFn) {
	ssn.jobPipelinedFns[name] = vf
}

// AddPredicateFn add Predicate function
func (ssn *Session) AddPredicateFn(name string, pf api.PredicateFn) {
	ssn.predicateFns[name] = pf
}

// AddBestNodeFn add BestNode function
func (ssn *Session) AddBestNodeFn(name string, pf api.BestNodeFn) {
	ssn.bestNodeFns[name] = pf
}

// AddNodeOrderFn add Node order function
func (ssn *Session) AddNodeOrderFn(name string, pf api.NodeOrderFn) {
	ssn.nodeOrderFns[name] = pf
}

// AddBatchNodeOrderFn add Batch Node order function
func (ssn *Session) AddBatchNodeOrderFn(name string, pf api.BatchNodeOrderFn) {
	ssn.batchNodeOrderFns[name] = pf
}

// AddNodeMapFn add Node map function
func (ssn *Session) AddNodeMapFn(name string, pf api.NodeMapFn) {
	ssn.nodeMapFns[name] = pf
}

// AddNodeReduceFn add Node reduce function
func (ssn *Session) AddNodeReduceFn(name string, pf api.NodeReduceFn) {
	ssn.nodeReduceFns[name] = pf
}

// AddOverusedFn add overused function
func (ssn *Session) AddOverusedFn(name string, fn api.ValidateFn) {
	ssn.overusedFns[name] = fn
}

// AddAllocatableFn add allocatable function
func (ssn *Session) AddAllocatableFn(name string, fn api.AllocatableFn) {
	ssn.allocatableFns[name] = fn
}

// AddJobValidFn add jobvalid function
func (ssn *Session) AddJobValidFn(name string, fn api.ValidateExFn) {
	ssn.jobValidFns[name] = fn
}

// AddJobEnqueueableFn add jobenqueueable function
func (ssn *Session) AddJobEnqueueableFn(name string, fn api.VoteFn) {
	ssn.jobEnqueueableFns[name] = fn
}

// AddJobEnqueuedFn add jobEnqueued function
func (ssn *Session) AddJobEnqueuedFn(name string, fn api.JobEnqueuedFn) {
	ssn.jobEnqueuedFns[name] = fn
}

// AddTargetJobFn add targetjob function
func (ssn *Session) AddTargetJobFn(name string, fn api.TargetJobFn) {
	ssn.targetJobFns[name] = fn
}

// AddReservedNodesFn add reservedNodesFn function
func (ssn *Session) AddReservedNodesFn(name string, fn api.ReservedNodesFn) {
	ssn.reservedNodesFns[name] = fn
}

// AddVictimTasksFns add victimTasksFns function
func (ssn *Session) AddVictimTasksFns(name string, fn api.VictimTasksFn) {
	ssn.victimTasksFns[name] = fn
}

// AddJobStarvingFns add jobStarvingFns function
func (ssn *Session) AddJobStarvingFns(name string, fn api.ValidateFn) {
	ssn.jobStarvingFns[name] = fn
}

// Reclaimable invoke reclaimable function of the plugins
func (ssn *Session) Reclaimable(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) []*api.TaskInfo {
	var victims []*api.TaskInfo
	var init bool

	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledReclaimable) {
				continue
			}
			rf, found := ssn.reclaimableFns[plugin.Name]
			if !found {
				continue
			}

			candidates, abstain := rf(reclaimer, reclaimees)
			if abstain == 0 {
				continue
			}
			if len(candidates) == 0 {
				victims = nil
				break
			}
			if !init {
				victims = candidates
				init = true
			} else {
				var intersection []*api.TaskInfo
				// Get intersection of victims and candidates.
				for _, v := range victims {
					for _, c := range candidates {
						if v.UID == c.UID {
							intersection = append(intersection, v)
						}
					}
				}

				// Update victims to intersection
				victims = intersection
			}
		}
		// Plugins in this tier made decision if victims is not nil
		if victims != nil {
			return victims
		}
	}

	return victims
}

// Preemptable invoke preemptable function of the plugins
func (ssn *Session) Preemptable(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
	var victims []*api.TaskInfo
	var init bool

	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledPreemptable) {
				continue
			}

			pf, found := ssn.preemptableFns[plugin.Name]
			if !found {
				continue
			}
			candidates, abstain := pf(preemptor, preemptees)
			if abstain == 0 {
				continue
			}
			// intersection will be nil if length is 0, don't need to do any more check
			if len(candidates) == 0 {
				victims = nil
				break
			}

			if !init {
				victims = candidates
				init = true
			} else {
				var intersection []*api.TaskInfo
				// Get intersection of victims and candidates.
				for _, v := range victims {
					for _, c := range candidates {
						if v.UID == c.UID {
							intersection = append(intersection, v)
						}
					}
				}

				// Update victims to intersection
				victims = intersection
			}
		}
		// Plugins in this tier made decision if victims is not nil
		if victims != nil {
			return victims
		}
	}

	return victims
}

// Overused invoke overused function of the plugins
func (ssn *Session) Overused(queue *api.QueueInfo) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			of, found := ssn.overusedFns[plugin.Name]
			if !found {
				continue
			}
			if of(queue) {
				return true
			}
		}
	}

	return false
}

// Allocatable invoke allocatable function of the plugins
func (ssn *Session) Allocatable(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			af, found := ssn.allocatableFns[plugin.Name]
			if !found {
				continue
			}
			if !af(queue, candidate) {
				return false
			}
		}
	}

	return true
}

// JobReady invoke jobready function of the plugins
func (ssn *Session) JobReady(obj interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobReady) {
				continue
			}
			jrf, found := ssn.jobReadyFns[plugin.Name]
			if !found {
				continue
			}

			if !jrf(obj) {
				return false
			}
		}
	}

	return true
}

// JobPipelined invoke pipelined function of the plugins
// Check if job has get enough resource to run
func (ssn *Session) JobPipelined(obj interface{}) bool {
	var hasFound bool
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobPipelined) {
				continue
			}
			jrf, found := ssn.jobPipelinedFns[plugin.Name]
			if !found {
				continue
			}

			res := jrf(obj)
			if res < 0 {
				return false
			}
			if res > 0 {
				hasFound = true
			}
		}
		// if plugin exists that votes permit, meanwhile other plugin votes abstention,
		// permit job to be pipelined, do not check next tier
		if hasFound {
			return true
		}
	}

	return true
}

// JobStarving invoke jobStarving function of the plugins
// Check if job still need more resource
func (ssn *Session) JobStarving(obj interface{}) bool {
	var hasFound bool
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobStarving) {
				continue
			}
			jrf, found := ssn.jobStarvingFns[plugin.Name]
			if !found {
				continue
			}
			hasFound = true

			if !jrf(obj) {
				return false
			}
		}
		// this tier registered function
		if hasFound {
			return true
		}
	}

	return false
}

// JobValid invoke jobvalid function of the plugins
func (ssn *Session) JobValid(obj interface{}) *api.ValidateResult {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			jrf, found := ssn.jobValidFns[plugin.Name]
			if !found {
				continue
			}

			if vr := jrf(obj); vr != nil && !vr.Pass {
				return vr
			}
		}
	}

	return nil
}

// JobEnqueueable invoke jobEnqueueableFns function of the plugins
func (ssn *Session) JobEnqueueable(obj interface{}) bool {
	var hasFound bool
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobEnqueued) {
				continue
			}
			fn, found := ssn.jobEnqueueableFns[plugin.Name]
			if !found {
				continue
			}

			res := fn(obj)
			if res < 0 {
				return false
			}
			if res > 0 {
				hasFound = true
			}
		}
		// if plugin exists that votes permit, meanwhile other plugin votes abstention,
		// permit job to be enqueueable, do not check next tier
		if hasFound {
			return true
		}
	}

	return true
}

// JobEnqueued invoke jobEnqueuedFns function of the plugins
func (ssn *Session) JobEnqueued(obj interface{}) {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobEnqueued) {
				continue
			}
			fn, found := ssn.jobEnqueuedFns[plugin.Name]
			if !found {
				continue
			}

			fn(obj)
		}
	}
}

// TargetJob invoke targetJobFns function of the plugins
func (ssn *Session) TargetJob(jobs []*api.JobInfo) *api.JobInfo {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledTargetJob) {
				continue
			}
			fn, found := ssn.targetJobFns[plugin.Name]
			if !found {
				continue
			}
			return fn(jobs)
		}
	}
	return nil
}

// VictimTasks invoke ReservedNodes function of the plugins
func (ssn *Session) VictimTasks() []*api.TaskInfo {
	var victims []*api.TaskInfo
	var init bool

	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledVictim) {
				continue
			}

			pf, found := ssn.victimTasksFns[plugin.Name]
			if !found {
				continue
			}
			candidates := pf()
			if !init {
				victims = candidates
				init = true
			} else {
				var intersection []*api.TaskInfo
				// Get intersection of victims and candidates.
				for _, v := range victims {
					for _, c := range candidates {
						if v.UID == c.UID {
							intersection = append(intersection, v)
						}
					}
				}

				// Update victims to intersection
				victims = intersection
			}
		}
		// Plugins in this tier made decision if victims is not nil
		if victims != nil {
			return victims
		}
	}

	return victims
}

// ReservedNodes invoke ReservedNodes function of the plugins
func (ssn *Session) ReservedNodes() {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledReservedNodes) {
				continue
			}
			fn, found := ssn.reservedNodesFns[plugin.Name]
			if !found {
				continue
			}
			fn()
		}
	}
}

// JobOrderFn invoke joborder function of the plugins
func (ssn *Session) JobOrderFn(l, r interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobOrder) {
				continue
			}
			jof, found := ssn.jobOrderFns[plugin.Name]
			if !found {
				continue
			}
			if j := jof(l, r); j != 0 {
				return j < 0
			}
		}
	}

	// If no job order funcs, order job by CreationTimestamp first, then by UID.
	lv := l.(*api.JobInfo)
	rv := r.(*api.JobInfo)
	if lv.CreationTimestamp.Equal(&rv.CreationTimestamp) {
		return lv.UID < rv.UID
	}
	return lv.CreationTimestamp.Before(&rv.CreationTimestamp)
}

// NamespaceOrderFn invoke namespaceorder function of the plugins
func (ssn *Session) NamespaceOrderFn(l, r interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNamespaceOrder) {
				continue
			}
			nof, found := ssn.namespaceOrderFns[plugin.Name]
			if !found {
				continue
			}
			if j := nof(l, r); j != 0 {
				return j < 0
			}
		}
	}

	// TODO(lminzhw): if all NamespaceOrderFn treat these two namespace as the same,
	// we should make the job order have its affect among namespaces.
	// or just schedule namespace one by one
	lv := l.(api.NamespaceName)
	rv := r.(api.NamespaceName)
	return lv < rv
}

// ClusterOrderFn invoke ClusterOrderFn function of the plugins
func (ssn *Session) ClusterOrderFn(l, r interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledClusterOrder) {
				continue
			}
			cof, found := ssn.clusterOrderFns[plugin.Name]
			if !found {
				continue
			}
			if j := cof(l, r); j != 0 {
				return j < 0
			}
		}
	}

	// If no cluster order funcs, order cluster by ClusterID
	lv := l.(*scheduling.Cluster)
	rv := r.(*scheduling.Cluster)
	return lv.Name < rv.Name
}

// QueueOrderFn invoke queueorder function of the plugins
func (ssn *Session) QueueOrderFn(l, r interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledQueueOrder) {
				continue
			}
			qof, found := ssn.queueOrderFns[plugin.Name]
			if !found {
				continue
			}
			if j := qof(l, r); j != 0 {
				return j < 0
			}
		}
	}

	// If no queue order funcs, order queue by CreationTimestamp first, then by UID.
	lv := l.(*api.QueueInfo)
	rv := r.(*api.QueueInfo)
	if lv.Queue.CreationTimestamp.Equal(&rv.Queue.CreationTimestamp) {
		return lv.UID < rv.UID
	}
	return lv.Queue.CreationTimestamp.Before(&rv.Queue.CreationTimestamp)
}

// TaskCompareFns invoke taskorder function of the plugins
func (ssn *Session) TaskCompareFns(l, r interface{}) int {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledTaskOrder) {
				continue
			}
			tof, found := ssn.taskOrderFns[plugin.Name]
			if !found {
				continue
			}
			if j := tof(l, r); j != 0 {
				return j
			}
		}
	}

	return 0
}

// TaskOrderFn invoke taskorder function of the plugins
func (ssn *Session) TaskOrderFn(l, r interface{}) bool {
	if res := ssn.TaskCompareFns(l, r); res != 0 {
		return res < 0
	}

	// If no task order funcs, order task by default func.
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)
	return helpers.CompareTask(lv, rv)
}

// PredicateFn invoke predicate function of the plugins
func (ssn *Session) PredicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledPredicate) {
				continue
			}
			pfn, found := ssn.predicateFns[plugin.Name]
			if !found {
				continue
			}
			err := pfn(task, node)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// BestNodeFn invoke bestNode function of the plugins
func (ssn *Session) BestNodeFn(task *api.TaskInfo, nodeScores map[float64][]*api.NodeInfo) *api.NodeInfo {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledBestNode) {
				continue
			}
			pfn, found := ssn.bestNodeFns[plugin.Name]
			if !found {
				continue
			}
			// Only the first plugin that enables and realizes bestNodeFn is allowed to choose best node for task
			if bestNode := pfn(task, nodeScores); bestNode != nil {
				return bestNode
			}
		}
	}
	return nil
}

// NodeOrderFn invoke node order function of the plugins
func (ssn *Session) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
	priorityScore := 0.0
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := ssn.nodeOrderFns[plugin.Name]
			if !found {
				continue
			}
			score, err := pfn(task, node)
			if err != nil {
				return 0, err
			}
			priorityScore += score
		}
	}
	return priorityScore, nil
}

// BatchNodeOrderFn invoke node order function of the plugins
func (ssn *Session) BatchNodeOrderFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
	priorityScore := make(map[string]float64, len(nodes))
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := ssn.batchNodeOrderFns[plugin.Name]
			if !found {
				continue
			}
			score, err := pfn(task, nodes)
			if err != nil {
				return nil, err
			}
			for nodeName, score := range score {
				priorityScore[nodeName] += score
			}
		}
	}
	return priorityScore, nil
}

func isEnabled(enabled *bool) bool {
	return enabled != nil && *enabled
}

// NodeOrderMapFn invoke node order function of the plugins
func (ssn *Session) NodeOrderMapFn(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
	nodeScoreMap := map[string]float64{}
	var priorityScore float64
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			if pfn, found := ssn.nodeOrderFns[plugin.Name]; found {
				score, err := pfn(task, node)
				if err != nil {
					return nodeScoreMap, priorityScore, err
				}
				priorityScore += score
			}
			if pfn, found := ssn.nodeMapFns[plugin.Name]; found {
				score, err := pfn(task, node)
				if err != nil {
					return nodeScoreMap, priorityScore, err
				}
				nodeScoreMap[plugin.Name] = score
			}
		}
	}
	return nodeScoreMap, priorityScore, nil
}

// NodeOrderReduceFn invoke node order function of the plugins
func (ssn *Session) NodeOrderReduceFn(task *api.TaskInfo, pluginNodeScoreMap map[string]k8sframework.NodeScoreList) (map[string]float64, error) {
	nodeScoreMap := map[string]float64{}
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledNodeOrder) {
				continue
			}
			pfn, found := ssn.nodeReduceFns[plugin.Name]
			if !found {
				continue
			}
			if err := pfn(task, pluginNodeScoreMap[plugin.Name]); err != nil {
				return nodeScoreMap, err
			}
			for _, hp := range pluginNodeScoreMap[plugin.Name] {
				nodeScoreMap[hp.Name] += float64(hp.Score)
			}
		}
	}
	return nodeScoreMap, nil
}
