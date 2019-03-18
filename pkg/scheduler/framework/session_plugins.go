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
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

func (ssn *Session) AddJobOrderFn(name string, cf api.CompareFn) {
	ssn.jobOrderFns[name] = cf
}

func (ssn *Session) AddQueueOrderFn(name string, qf api.CompareFn) {
	ssn.queueOrderFns[name] = qf
}

func (ssn *Session) AddTaskOrderFn(name string, cf api.CompareFn) {
	ssn.taskOrderFns[name] = cf
}

func (ssn *Session) AddPreemptableFn(name string, cf api.EvictableFn) {
	ssn.preemptableFns[name] = cf
}

func (ssn *Session) AddReclaimableFn(name string, rf api.EvictableFn) {
	ssn.reclaimableFns[name] = rf
}

func (ssn *Session) AddJobReadyFn(name string, vf api.ValidateFn) {
	ssn.jobReadyFns[name] = vf
}

func (ssn *Session) AddPredicateFn(name string, pf api.PredicateFn) {
	ssn.predicateFns[name] = pf
}

func (ssn *Session) AddNodeOrderFn(name string, pf api.NodeOrderFn) {
	ssn.nodeOrderFns[name] = pf
}

func (ssn *Session) AddOverusedFn(name string, fn api.ValidateFn) {
	ssn.overusedFns[name] = fn
}

func (ssn *Session) AddJobValidFn(name string, fn api.ValidateExFn) {
	ssn.jobValidFns[name] = fn
}

func (ssn *Session) Reclaimable(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) []*api.TaskInfo {
	var victims []*api.TaskInfo
	var init bool

	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.ReclaimableDisabled {
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

func (ssn *Session) Preemptable(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
	var victims []*api.TaskInfo
	var init bool

	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.PreemptableDisabled {
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

func (ssn *Session) JobReady(obj interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.JobReadyDisabled {
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

func (ssn *Session) JobOrderFn(l, r interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.JobOrderDisabled {
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
	} else {
		return lv.CreationTimestamp.Before(&rv.CreationTimestamp)
	}
}

func (ssn *Session) QueueOrderFn(l, r interface{}) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.QueueOrderDisabled {
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
	} else {
		return lv.Queue.CreationTimestamp.Before(&rv.Queue.CreationTimestamp)
	}
}

func (ssn *Session) TaskCompareFns(l, r interface{}) int {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.TaskOrderDisabled {
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

func (ssn *Session) TaskOrderFn(l, r interface{}) bool {
	if res := ssn.TaskCompareFns(l, r); res != 0 {
		return res < 0
	}

	// If no task order funcs, order task by CreationTimestamp first, then by UID.
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)
	if lv.Pod.CreationTimestamp.Equal(&rv.Pod.CreationTimestamp) {
		return lv.UID < rv.UID
	} else {
		return lv.Pod.CreationTimestamp.Before(&rv.Pod.CreationTimestamp)
	}
}

func (ssn *Session) PredicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.PredicateDisabled {
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

func (ssn *Session) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (int, error) {
	priorityScore := 0
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.NodeOrderDisabled {
				continue
			}
			pfn, found := ssn.nodeOrderFns[plugin.Name]
			if !found {
				continue
			}
			score, err := pfn(task, node)
			if err != nil {
				return 0, err
			} else {
				priorityScore = priorityScore + score
			}
		}
	}
	return priorityScore, nil
}
