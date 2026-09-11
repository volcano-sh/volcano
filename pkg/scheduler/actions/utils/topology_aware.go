/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

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
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	MinCandidateNodesPercentageKey = "minCandidateNodesPercentage"
	MinCandidateNodesAbsoluteKey   = "minCandidateNodesAbsolute"
	MaxCandidateNodesAbsoluteKey   = "maxCandidateNodesAbsolute"
)

// VictimsCollectorFn filters and ranks eligible victims for an initiator task.
type VictimsCollectorFn func(initiator *api.TaskInfo, candidates []*api.TaskInfo) []*api.TaskInfo

// SelectVictimsOptions controls dry-run victim selection behavior.
type SelectVictimsOptions struct {
	// CheckAllocatable enables SimulateAllocatableFn for the initiator's queue.
	// Prefer true for same-queue preemption; false for cross-queue reclaim.
	CheckAllocatable bool
	ActionName       string // used in log messages, e.g. "preempt" or "reclaim"
}

// Candidate is a dry-run eviction plan for one node.
type Candidate struct {
	Victims []*api.TaskInfo
	Name    string
}

// CandidateVictims returns c.Victims.
func (c *Candidate) CandidateVictims() []*api.TaskInfo {
	return c.Victims
}

// CandidateName returns c.Name.
func (c *Candidate) CandidateName() string {
	return c.Name
}

type candidateList struct {
	idx   int32
	items []*Candidate
}

func newCandidateList(size int) *candidateList {
	return &candidateList{idx: -1, items: make([]*Candidate, size)}
}

func (cl *candidateList) add(c *Candidate) {
	if idx := atomic.AddInt32(&cl.idx, 1); idx < int32(len(cl.items)) {
		cl.items[idx] = c
	}
}

func (cl *candidateList) size() int {
	n := int(atomic.LoadInt32(&cl.idx) + 1)
	if n >= len(cl.items) {
		n = len(cl.items)
	}
	return n
}

func (cl *candidateList) get() []*Candidate {
	return cl.items[:cl.size()]
}

// CandidateLimits holds sampling constraints for dry-run candidate discovery.
type CandidateLimits struct {
	WorkerNum                   int
	MinCandidateNodesPercentage int
	MinCandidateNodesAbsolute   int
	MaxCandidateNodesAbsolute   int
}

// CalculateNumCandidates returns how many successful candidates dry-run should collect.
func CalculateNumCandidates(numNodes int, limits CandidateLimits) int {
	n := (numNodes * limits.MinCandidateNodesPercentage) / 100

	if n < limits.MinCandidateNodesAbsolute {
		n = limits.MinCandidateNodesAbsolute
	}
	if n > limits.MaxCandidateNodesAbsolute {
		n = limits.MaxCandidateNodesAbsolute
	}
	if n > numNodes {
		n = numNodes
	}
	return n
}

// GetOffsetAndNumCandidates chooses a random offset and candidate count.
func GetOffsetAndNumCandidates(numNodes int, limits CandidateLimits) (int, int) {
	if numNodes <= 0 {
		return 0, 0
	}
	return rand.Intn(numNodes), CalculateNumCandidates(numNodes, limits)
}

// DryRunEviction runs parallel topology-aware victim selection on potential nodes.
func DryRunEviction(
	ssn *framework.Session,
	initiator *api.TaskInfo,
	currentQueue *api.QueueInfo,
	potentialNodes []*api.NodeInfo,
	offset int,
	numCandidates int,
	limits CandidateLimits,
	filter func(*api.TaskInfo) bool,
	collectVictims VictimsCollectorFn,
	opts SelectVictimsOptions,
) ([]*Candidate, map[string]api.Status, error) {
	candidates := newCandidateList(numCandidates)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nodeStatuses := make(map[string]api.Status)
	var statusesLock sync.Mutex
	var errs []error

	state := ssn.GetCycleState(initiator.UID)
	workerNum := limits.WorkerNum
	if workerNum <= 0 {
		workerNum = 16
	}

	checkNode := func(i int) {
		nodeInfoCopy := potentialNodes[(offset+i)%len(potentialNodes)].Clone()
		stateCopy := state.Clone()

		victims, status := SelectVictimsOnNode(ctx, stateCopy, initiator, currentQueue, nodeInfoCopy, ssn, filter, collectVictims, opts)
		if status.IsSuccess() {
			c := &Candidate{
				Victims: victims,
				Name:    nodeInfoCopy.Name,
			}
			candidates.add(c)
			if candidates.size() >= numCandidates {
				cancel()
			}
			return
		}
		statusesLock.Lock()
		if status.Code == api.Error {
			errs = append(errs, status.AsError())
		}
		nodeStatuses[nodeInfoCopy.Name] = *status
		statusesLock.Unlock()
	}

	workqueue.ParallelizeUntil(ctx, workerNum, len(potentialNodes), checkNode)
	return candidates.get(), nodeStatuses, utilerrors.NewAggregate(errs)
}

// FindCandidates discovers dry-run eviction candidates from predicated nodes.
func FindCandidates(
	ssn *framework.Session,
	initiator *api.TaskInfo,
	currentQueue *api.QueueInfo,
	predicateNodes []*api.NodeInfo,
	limits CandidateLimits,
	filter func(*api.TaskInfo) bool,
	collectVictims VictimsCollectorFn,
	opts SelectVictimsOptions,
) ([]*Candidate, map[string]api.Status, error) {
	if len(predicateNodes) == 0 {
		klog.V(3).Infof("No nodes are eligible to %s task %s/%s", opts.ActionName, initiator.Namespace, initiator.Name)
		return nil, nil, nil
	}
	klog.V(4).Infof("the predicateNodes number is %d for %s", len(predicateNodes), opts.ActionName)

	offset, numCandidates := GetOffsetAndNumCandidates(len(predicateNodes), limits)
	candidates, nodeStatuses, err := DryRunEviction(ssn, initiator, currentQueue, predicateNodes, offset, numCandidates, limits, filter, collectVictims, opts)

	nodeToStatusMap := make(map[string]api.Status, len(nodeStatuses))
	for node, nodeStatus := range nodeStatuses {
		nodeToStatusMap[node] = nodeStatus
	}
	return candidates, nodeToStatusMap, err
}

// RunTopologyAwareEviction discovers candidates, selects the best node, and applies evictions.
func RunTopologyAwareEviction(
	ssn *framework.Session,
	stmt *framework.Statement,
	initiator *api.TaskInfo,
	currentQueue *api.QueueInfo,
	predicateNodes []*api.NodeInfo,
	limits CandidateLimits,
	opts SelectVictimsOptions,
	filter func(*api.TaskInfo) bool,
	collectVictims VictimsCollectorFn,
	reasonPrefix string,
) (bool, error) {
	candidates, nodeToStatusMap, err := FindCandidates(ssn, initiator, currentQueue, predicateNodes, limits, filter, collectVictims, opts)
	if err != nil && len(candidates) == 0 {
		return false, err
	}
	if len(candidates) == 0 {
		return false, fmt.Errorf("no %s candidates that fit the pod, the status of the nodes are %v", opts.ActionName, nodeToStatusMap)
	}

	bestCandidate, err := SelectCandidate(ssn, initiator, candidates)
	if err != nil {
		return false, err
	}
	return ApplyTopologyAwareEviction(ssn, stmt, initiator, bestCandidate, reasonPrefix)
}

// PrepareCandidate evicts victims of the selected candidate into stmt.
func PrepareCandidate(c *Candidate, pod *v1.Pod, stmt *framework.Statement, reasonPrefix string) {
	for _, victim := range c.Victims {
		klog.V(3).Infof("Try to %s Task <%s/%s> for Task <%s/%s>",
			reasonPrefix, victim.Namespace, victim.Name, pod.Namespace, pod.Name)
		reason := fmt.Sprintf("%s by %s/%s", reasonPrefix, pod.Namespace, pod.Name)
		stmt.Evict(victim, reason)
	}
}

// SelectVictimsOnNode finds a minimal victim set on one node via simulation.
func SelectVictimsOnNode(
	ctx context.Context,
	state fwk.CycleState,
	initiator *api.TaskInfo,
	currentQueue *api.QueueInfo,
	nodeInfo *api.NodeInfo,
	ssn *framework.Session,
	filter func(*api.TaskInfo) bool,
	collectVictims VictimsCollectorFn,
	opts SelectVictimsOptions,
) ([]*api.TaskInfo, *api.Status) {
	var potentialVictims []*api.TaskInfo

	removeTask := func(rti *api.TaskInfo) error {
		if err := ssn.SimulateRemoveTaskFn(ctx, state, initiator, rti, nodeInfo); err != nil {
			return err
		}
		nodeInfo.RemoveTask(rti)
		return nil
	}

	addTask := func(ati *api.TaskInfo) error {
		if err := ssn.SimulateAddTaskFn(ctx, state, initiator, ati, nodeInfo); err != nil {
			return err
		}
		if err := nodeInfo.AddTask(ati); err != nil {
			return err
		}
		return nil
	}

	fits := func() bool {
		if opts.CheckAllocatable {
			if currentQueue == nil {
				klog.V(4).Infof("%s fit failed for task <%s/%s> on node <%s>: currentQueue is nil",
					opts.ActionName, initiator.Namespace, initiator.Name, nodeInfo.Name)
				return false
			}
			if !ssn.SimulateAllocatableFn(ctx, state, currentQueue, initiator) {
				klog.V(4).Infof("%s fit failed for task <%s/%s> on node <%s>: SimulateAllocatableFn false for queue <%s>",
					opts.ActionName, initiator.Namespace, initiator.Name, nodeInfo.Name, currentQueue.Name)
				return false
			}
		}
		futureIdle := nodeInfo.FutureIdle()
		if !initiator.InitResreq.LessEqual(futureIdle, api.Zero) {
			klog.V(4).Infof("%s fit failed for task <%s/%s> on node <%s>: insufficient resource, request <%v>, futureIdle <%v>",
				opts.ActionName, initiator.Namespace, initiator.Name, nodeInfo.Name, initiator.InitResreq, futureIdle)
			return false
		}
		if err := ssn.SimulatePredicateFn(ctx, state, initiator, nodeInfo); err != nil {
			klog.V(4).Infof("%s fit failed for task <%s/%s> on node <%s>: SimulatePredicateFn err=%v",
				opts.ActionName, initiator.Namespace, initiator.Name, nodeInfo.Name, err)
			return false
		}
		return true
	}

	var candidates []*api.TaskInfo
	for _, task := range nodeInfo.Tasks {
		if filter == nil {
			candidates = append(candidates, task.Clone())
		} else if filter(task) {
			candidates = append(candidates, task.Clone())
		}
	}

	klog.V(3).Infof("[%s] node <%s> all candidates for task <%s/%s>: %v",
		opts.ActionName, nodeInfo.Name, initiator.Namespace, initiator.Name, candidates)

	allVictims := collectVictims(initiator, candidates)
	if err := util.ValidateVictims(initiator, nodeInfo, allVictims); err != nil {
		klog.V(3).Infof("[%s] node <%s> no validated victims for task <%s/%s>: %v",
			opts.ActionName, nodeInfo.Name, initiator.Namespace, initiator.Name, err)
		return nil, api.AsStatus(fmt.Errorf("no validated victims on Node <%s>: %v", nodeInfo.Name, err))
	}

	klog.V(3).Infof("[%s] node <%s> allVictims for task <%s/%s>: %v",
		opts.ActionName, nodeInfo.Name, initiator.Namespace, initiator.Name, allVictims)

	victimsQueue := ssn.BuildVictimsPriorityQueue(allVictims, initiator)

	for !victimsQueue.Empty() {
		task := victimsQueue.Pop().(*api.TaskInfo)
		potentialVictims = append(potentialVictims, task)
		if err := removeTask(task); err != nil {
			return nil, api.AsStatus(err)
		}

		if fits() {
			klog.V(3).Infof("[%s] node <%s>: task <%s/%s> can be scheduled after evicting <%s/%s>, stop evicting more pods",
				opts.ActionName, nodeInfo.Name, initiator.Namespace, initiator.Name, task.Namespace, task.Name)
			break
		}
	}

	if len(potentialVictims) == 0 {
		if !fits() {
			return nil, api.AsStatus(fmt.Errorf("no %s victims found for incoming pod", opts.ActionName))
		}
		if err := ssn.SimulatePredicateFn(ctx, state, initiator, nodeInfo); err != nil {
			return nil, api.AsStatus(fmt.Errorf("failed to predicate pod %s/%s on node %s: %v", initiator.Namespace, initiator.Name, nodeInfo.Name, err))
		}
		return []*api.TaskInfo{}, &api.Status{Reason: ""}
	}

	if err := ssn.SimulatePredicateFn(ctx, state, initiator, nodeInfo); err != nil {
		return nil, api.AsStatus(fmt.Errorf("failed to predicate pod %s/%s on node %s: %v", initiator.Namespace, initiator.Name, nodeInfo.Name, err))
	}

	var victims []*api.TaskInfo
	klog.V(3).Infof("[%s] node <%s> potentialVictims for task <%s/%s>: %v",
		opts.ActionName, nodeInfo.Name, initiator.Namespace, initiator.Name, potentialVictims)

	reprievePod := func(pi *api.TaskInfo) (bool, error) {
		if err := addTask(pi); err != nil {
			klog.ErrorS(err, "Failed to add task", "task", klog.KObj(pi.Pod))
			return false, err
		}

		canFit := fits()
		if !canFit {
			if err := removeTask(pi); err != nil {
				return false, err
			}
			victims = append(victims, pi)
			klog.V(3).Infof("[%s] node <%s>: task <%s/%s> is a victim (reprieve failed)",
				opts.ActionName, nodeInfo.Name, pi.Namespace, pi.Name)
		}
		klog.V(4).Infof("reprievePod for task: %v, fits: %v", pi.Name, canFit)
		return canFit, nil
	}

	// Reverse potentialVictims to reprieve higher priority pods first.
	for i, j := 0, len(potentialVictims)-1; i < j; i, j = i+1, j-1 {
		potentialVictims[i], potentialVictims[j] = potentialVictims[j], potentialVictims[i]
	}

	for _, p := range potentialVictims {
		if _, err := reprievePod(p); err != nil {
			return nil, api.AsStatus(err)
		}
	}

	klog.V(4).Infof("[%s] node <%s> final victims for task <%s/%s>: %v",
		opts.ActionName, nodeInfo.Name, initiator.Namespace, initiator.Name, victims)
	return victims, &api.Status{Reason: ""}
}

// SelectCandidate chooses the best-fit candidate from the dry-run results.
// Plugin victim scores are used when available; otherwise built-in ordering applies,
// matching legacy preempt behavior (final fallback: candidates[0]).
func SelectCandidate(ssn *framework.Session, initiator *api.TaskInfo, candidates []*Candidate) (*Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	victimsMap := CandidatesToVictimsMap(candidates)
	var scores map[string]float64
	if ssn != nil {
		var err error
		if scores, err = ssn.BatchVictimNodeScore(initiator, victimsMap); err != nil {
			return nil, fmt.Errorf("batch victim node score for task <%s/%s>: %w",
				initiator.Namespace, initiator.Name, err)
		}
		if len(scores) == 1 {
			for nodeName := range scores {
				if victims, ok := victimsMap[nodeName]; ok {
					return &Candidate{
						Victims: victims,
						Name:    nodeName,
					}, nil
				}
			}
		}
	}

	candidateNode := pickOneNodeForEviction(filterMaxScoredNodes(victimsMap, scores))

	if victims, ok := victimsMap[candidateNode]; ok {
		return &Candidate{
			Victims: victims,
			Name:    candidateNode,
		}, nil
	}

	klog.Error(errors.New("no candidate selected"), "Should not reach here", "candidates", candidates)
	return candidates[0], nil
}

// CandidatesToVictimsMap maps node name to victims.
func CandidatesToVictimsMap(candidates []*Candidate) map[string][]*api.TaskInfo {
	m := make(map[string][]*api.TaskInfo, len(candidates))
	for _, c := range candidates {
		m[c.Name] = c.Victims
	}
	return m
}

func filterMaxScoredNodes(nodesToVictims map[string][]*api.TaskInfo, scores map[string]float64) map[string][]*api.TaskInfo {
	if len(scores) == 0 {
		return nodesToVictims
	}

	maxScore := math.Inf(-1)
	for node := range nodesToVictims {
		if s := scores[node]; s > maxScore {
			maxScore = s
		}
	}

	filtered := make(map[string][]*api.TaskInfo, len(nodesToVictims))
	for node, victims := range nodesToVictims {
		if math.Abs(scores[node]-maxScore) < 1e-9 {
			filtered[node] = victims
		}
	}
	if len(filtered) == 0 {
		return nodesToVictims
	}
	return filtered
}

func pickOneNodeForEviction(nodesToVictims map[string][]*api.TaskInfo) string {
	if len(nodesToVictims) == 0 {
		return ""
	}

	allCandidates := make([]string, 0, len(nodesToVictims))
	for node := range nodesToVictims {
		allCandidates = append(allCandidates, node)
	}

	scoreFuncs := []func(string) int64{
		func(node string) int64 {
			victims := nodesToVictims[node]
			if len(victims) == 0 {
				return 0
			}
			highestPodPriority := PodPriority(victims[0].Pod)
			return -int64(highestPodPriority)
		},
		func(node string) int64 {
			victims := nodesToVictims[node]
			if len(victims) == 0 {
				return 0
			}
			var sumPriorities int64
			for _, task := range victims {
				sumPriorities += int64(PodPriority(task.Pod)) + int64(math.MaxInt32+1)
			}
			return -sumPriorities
		},
		func(node string) int64 {
			return -int64(len(nodesToVictims[node]))
		},
		func(node string) int64 {
			victims := nodesToVictims[node]
			if len(victims) == 0 {
				return math.MaxInt64
			}
			earliestStartTimeOnNode := GetEarliestPodStartTime(victims)
			if earliestStartTimeOnNode == nil {
				klog.Error(errors.New("earliestStartTime is nil for node"), "Should not reach here", "node", node)
				return int64(math.MinInt64)
			}
			return earliestStartTimeOnNode.UnixNano()
		},
	}

	for _, f := range scoreFuncs {
		selectedNodes := []string{}
		maxScore := int64(math.MinInt64)
		for _, node := range allCandidates {
			score := f(node)
			if score > maxScore {
				maxScore = score
				selectedNodes = []string{}
			}
			if score == maxScore {
				selectedNodes = append(selectedNodes, node)
			}
		}
		if len(selectedNodes) == 1 {
			return selectedNodes[0]
		}
		allCandidates = selectedNodes
	}

	return allCandidates[0]
}

// GetEarliestPodStartTime returns the earliest start time among highest-priority victims.
func GetEarliestPodStartTime(tasks []*api.TaskInfo) *metav1.Time {
	if len(tasks) == 0 {
		klog.Error(nil, "victims.Pods is empty. Should not reach here")
		return nil
	}

	earliestPodStartTime := GetPodStartTime(tasks[0].Pod)
	maxPriority := PodPriority(tasks[0].Pod)

	for _, task := range tasks {
		if podPriority := PodPriority(task.Pod); podPriority == maxPriority {
			if podStartTime := GetPodStartTime(task.Pod); podStartTime.Before(earliestPodStartTime) {
				earliestPodStartTime = podStartTime
			}
		} else if podPriority > maxPriority {
			maxPriority = podPriority
			earliestPodStartTime = GetPodStartTime(task.Pod)
		}
	}

	return earliestPodStartTime
}

// GetPodStartTime returns start time of the given pod or now if unset.
func GetPodStartTime(pod *v1.Pod) *metav1.Time {
	if pod.Status.StartTime != nil {
		return pod.Status.StartTime
	}
	return &metav1.Time{Time: time.Now()}
}

// PodPriority returns priority of the given pod.
func PodPriority(pod *v1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

// ApplyTopologyAwareEviction commits the best candidate: evict victims then pipeline initiator.
func ApplyTopologyAwareEviction(
	ssn *framework.Session,
	stmt *framework.Statement,
	initiator *api.TaskInfo,
	best *Candidate,
	reasonPrefix string,
) (bool, error) {
	if best == nil || best.Name == "" {
		return false, fmt.Errorf("no candidate node for %s", reasonPrefix)
	}

	tmpStmt := framework.NewStatement(ssn)
	PrepareCandidate(best, initiator.Pod, tmpStmt, reasonPrefix)
	node, found := ssn.Nodes[best.Name]
	if !found || node == nil {
		tmpStmt.Discard()
		return false, fmt.Errorf("node %q not found for %s", best.Name, reasonPrefix)
	}
	if err := ssn.PredicateForPreemptAction(initiator, node); err != nil {
		klog.V(3).Infof("%s for task <%s/%s> final predicate on node <%s> failed: %v",
			reasonPrefix, initiator.Namespace, initiator.Name, best.Name, err)
		tmpStmt.Discard()
		return false, err
	}
	if err := tmpStmt.Pipeline(initiator, best.Name, true); err != nil {
		klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
			initiator.Namespace, initiator.Name, best.Name)
		tmpStmt.Discard()
		return false, err
	}
	stmt.Merge(tmpStmt)
	return true, nil
}
