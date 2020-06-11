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
	schedulerapi "k8s.io/kube-scheduler/extender/v1"

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
func (ssn *Session) AddJobPipelinedFn(name string, vf api.ValidateFn) {
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

// AddJobValidFn add jobvalid function
func (ssn *Session) AddJobValidFn(name string, fn api.ValidateExFn) {
	ssn.jobValidFns[name] = fn
}

// AddJobEnqueueableFn add jobenqueueable function
func (ssn *Session) AddJobEnqueueableFn(name string, fn api.ValidateFn) {
	ssn.jobEnqueueableFns[name] = fn
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
			candidates := rf(reclaimer, reclaimees)
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
			candidates := pf(preemptor, preemptees)
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
func (ssn *Session) JobPipelined(obj interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledJobPipelined) {
				continue
			}
			jrf, found := ssn.jobPipelinedFns[plugin.Name]
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
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			fn, found := ssn.jobEnqueueableFns[plugin.Name]
			if !found {
				continue
			}

			if res := fn(obj); !res {
				return res
			}
		}
	}

	return true
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

	// If no task order funcs, order task by CreationTimestamp first, then by UID.
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)
	if lv.Pod.CreationTimestamp.Equal(&rv.Pod.CreationTimestamp) {
		return lv.UID < rv.UID
	}
	return lv.Pod.CreationTimestamp.Before(&rv.Pod.CreationTimestamp)

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
func (ssn *Session) NodeOrderReduceFn(task *api.TaskInfo, pluginNodeScoreMap map[string]schedulerapi.HostPriorityList) (map[string]float64, error) {
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
				nodeScoreMap[hp.Host] += float64(hp.Score)
			}
		}
	}
	return nodeScoreMap, nil
}
