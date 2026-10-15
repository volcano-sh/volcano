/*
Copyright 2026 The Volcano Authors.

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

package utils

import (
	"sort"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// pruneUnit is one removable step during victim pruning. A whole bundle is a
// single unit so a victim gang is never half evicted, while a safe bundle
// contributes one unit per pod.
type pruneUnit struct {
	tasks []*api.TaskInfo
}

// PruneRedundantVictims drops victims that the final nomination plan does not
// need. It only shrinks the input set: every returned task is a subset of the
// flattened selectedBundles, preserving the original order.
//
// The algorithm mirrors the contract described in volcano-sh/volcano#6065:
//
//  1. Find the viable nodes by running the reclaimer's pending tasks through
//     PrePredicateFn and PredicateForPreemptAction. Predicate errors recorded
//     by allocate are intentionally not used because they are usually missing
//     when reclaim is needed.
//  2. Drop victims one unit at a time. A unit is dropped when every pending
//     task still fits on the viable nodes, counted node by node, without the
//     resources that unit frees. Units are tried from the most-protected
//     queue first (reverse of SortBundlesForReclaim eviction order, which is
//     the order selectedBundles arrives in), and within a safe bundle from
//     the most disruptive pod first.
//  3. Callers must re-check the pruned set through BuildNominationPlanInDomain
//     and fall back to the original set when the predicates reject it.
//
// Pruning only reorders victims the eviction plugins have already allowed, so
// it never bypasses queueisolation, capacity, or other eviction filters. When
// viability cannot be established, the original victim list is returned.
func PruneRedundantVictims(ssn *framework.Session, pending []*api.TaskInfo, domainNodes []*api.NodeInfo, selectedBundles []*Bundle) []*api.TaskInfo {
	original := FlattenBundles(selectedBundles)
	if ssn == nil || len(pending) == 0 || len(domainNodes) == 0 || len(original) == 0 {
		return original
	}

	viableByTask, available, ok := buildPruneState(ssn, pending, domainNodes, original)
	if !ok {
		return original
	}

	units := buildPruneUnits(selectedBundles)
	if len(units) <= 1 {
		return original
	}

	dropped := make(map[api.TaskID]struct{})
	for _, unit := range units {
		without := cloneAvailable(available)
		for _, t := range unit.tasks {
			if avail, found := without[t.NodeName]; found {
				avail.SubWithoutAssert(t.Resreq)
			}
		}
		if !fitsPendingOnNodes(pending, viableByTask, without) {
			continue
		}
		// Unit is redundant: commit its removal.
		for _, t := range unit.tasks {
			dropped[t.UID] = struct{}{}
			if avail, found := available[t.NodeName]; found {
				avail.SubWithoutAssert(t.Resreq)
			}
		}
	}

	if len(dropped) == 0 {
		return original
	}
	pruned := make([]*api.TaskInfo, 0, len(original)-len(dropped))
	for _, t := range original {
		if _, ok := dropped[t.UID]; !ok {
			pruned = append(pruned, t)
		}
	}
	klog.V(3).Infof("PruneRedundantVictims: pruned %d/%d victims", len(dropped), len(original))
	return pruned
}

// buildPruneState computes per-pending-task viable nodes and the per-node
// available capacity (FutureIdle plus everything the current victims free).
func buildPruneState(ssn *framework.Session, pending []*api.TaskInfo, domainNodes []*api.NodeInfo, victims []*api.TaskInfo) (map[api.TaskID][]*api.NodeInfo, map[string]*api.Resource, bool) {
	viableByTask := make(map[api.TaskID][]*api.NodeInfo, len(pending))
	for _, task := range pending {
		if err := ssn.PrePredicateFn(task); err != nil {
			klog.V(5).Infof("PruneRedundantVictims: pre-predicate rejects task <%s/%s>, skip pruning: %v", task.Namespace, task.Name, err)
			return nil, nil, false
		}
		viable := make([]*api.NodeInfo, 0, len(domainNodes))
		for _, node := range domainNodes {
			if node == nil {
				continue
			}
			if err := ssn.PredicateForPreemptAction(task, node); err != nil {
				continue
			}
			viable = append(viable, node)
		}
		if len(viable) == 0 {
			klog.V(5).Infof("PruneRedundantVictims: no viable node for task <%s/%s>, skip pruning", task.Namespace, task.Name)
			return nil, nil, false
		}
		viableByTask[task.UID] = viable
	}

	available := make(map[string]*api.Resource, len(domainNodes))
	for _, node := range domainNodes {
		if node == nil {
			continue
		}
		available[node.Name] = node.FutureIdle().Clone()
	}
	for _, v := range victims {
		avail, found := available[v.NodeName]
		if !found {
			avail = api.EmptyResource()
			available[v.NodeName] = avail
		}
		avail.Add(v.Resreq)
	}
	return viableByTask, available, true
}

// buildPruneUnits orders removable units from the most-protected first.
// selectedBundles arrives in SortBundlesForReclaim eviction order (least
// protected first), so iterating it in reverse tries the most-protected queue
// first and, within a queue, the most disruptive bundle first (safe bundles
// sort before whole bundles in eviction order). Safe bundles are split per
// pod with the most disruptive pod first so elastic surplus replicas are
// preferred for removal.
func buildPruneUnits(selectedBundles []*Bundle) []pruneUnit {
	units := make([]pruneUnit, 0)
	for i := len(selectedBundles) - 1; i >= 0; i-- {
		b := selectedBundles[i]
		if b == nil || len(b.Tasks) == 0 {
			continue
		}
		if b.Type == BundleWhole {
			units = append(units, pruneUnit{tasks: b.Tasks})
			continue
		}
		ordered := append([]*api.TaskInfo(nil), b.Tasks...)
		sort.Slice(ordered, func(a, c int) bool {
			return moreDisruptive(ordered[a], ordered[c])
		})
		for _, t := range ordered {
			units = append(units, pruneUnit{tasks: []*api.TaskInfo{t}})
		}
	}
	return units
}

// fitsPendingOnNodes reports whether every pending task can be placed onto its
// viable nodes given the per-node available capacity, using first-fit in
// pending order. It is intentionally conservative: a false negative only
// prunes less, and the caller re-validates the pruned set through
// BuildNominationPlanInDomain, so a false positive can never grow evictions.
func fitsPendingOnNodes(pending []*api.TaskInfo, viableByTask map[api.TaskID][]*api.NodeInfo, available map[string]*api.Resource) bool {
	remaining := cloneAvailable(available)
	for _, task := range pending {
		placed := false
		for _, node := range viableByTask[task.UID] {
			avail, found := remaining[node.Name]
			if !found {
				continue
			}
			if !task.InitResreq.LessEqual(avail, api.Zero) {
				continue
			}
			avail.Sub(task.InitResreq)
			placed = true
			break
		}
		if !placed {
			return false
		}
	}
	return true
}

func cloneAvailable(src map[string]*api.Resource) map[string]*api.Resource {
	out := make(map[string]*api.Resource, len(src))
	for k, v := range src {
		if v == nil {
			out[k] = api.EmptyResource()
			continue
		}
		out[k] = v.Clone()
	}
	return out
}

// moreDisruptive orders tasks so the pod whose eviction frees the most
// resources is tried first for removal.
func moreDisruptive(l, r *api.TaskInfo) bool {
	if l == nil || r == nil {
		return r == nil && l != nil
	}
	if l.Resreq == nil || r.Resreq == nil {
		return r.Resreq == nil && l.Resreq != nil
	}
	if l.Resreq.MilliCPU != r.Resreq.MilliCPU {
		return l.Resreq.MilliCPU > r.Resreq.MilliCPU
	}
	if l.Resreq.Memory != r.Resreq.Memory {
		return l.Resreq.Memory > r.Resreq.Memory
	}
	ls, rs := scalarSum(l.Resreq), scalarSum(r.Resreq)
	if ls != rs {
		return ls > rs
	}
	return l.UID > r.UID
}

func scalarSum(res *api.Resource) float64 {
	sum := 0.0
	for _, v := range res.ScalarResources {
		sum += v
	}
	return sum
}
