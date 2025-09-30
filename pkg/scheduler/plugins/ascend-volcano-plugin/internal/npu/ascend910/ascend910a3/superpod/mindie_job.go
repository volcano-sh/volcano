/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package superpod is using for HuaWei Atlas 900 A3 SuperPod affinity schedule.
*/

package superpod

import (
	"container/heap"
	"fmt"
	"strconv"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

const (
	mindIEJobAppLabelKey = "app"
	mindIEJobIDLabelKey  = "jobID"
)

func (tp *module910SuperPod) isMindIEJob() bool {
	_, appExist := tp.Label[mindIEJobAppLabelKey]
	_, jobIdExist := tp.Label[mindIEJobIDLabelKey]
	return appExist && jobIdExist
}

func (tp *module910SuperPod) isIdleResourceFit() bool {
	resourceFit, ok := tp.Annotation["sp-fit"]
	if !ok {
		return false
	}

	return resourceFit == string(idlestResourceFitPolicy)
}

func (tp *module910SuperPod) selectSuperPodForMindIEJob(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string][]plugin.
	SuperNode, error) {
	superPodMap := tp.buildSuperPodGroups(nodes)

	totalVSP, unFit := tp.filterAndCount(superPodMap)
	totalRequiredSuperPod := tp.NPUTaskNum / tp.spBlock

	if totalVSP < totalRequiredSuperPod {
		return nil, fmt.Errorf("not enough virtual super-pod to schedule, required %d, total %d",
			totalRequiredSuperPod, totalVSP)
	}

	removeUnfitSuperPods(superPodMap, unFit)

	affinity := getJobAffinityLabel(task)
	klog.V(util.LogErrorLev).Infof("mindie job %s affinity %v", tp.Name, affinity)
	if len(affinity) > 0 {
		tp.calculateAffinities(superPodMap, affinity)
	}

	ng := tp.initPriorityQueue(superPodMap)
	selectNodes := selectNodesFromQueue(ng, totalRequiredSuperPod, len(affinity) > 0)

	return selectNodes, nil
}

func (tp *module910SuperPod) buildSuperPodGroups(nodes []*api.NodeInfo) map[int32]*nodeGroupItem {
	superPodMap := make(map[int32]*nodeGroupItem)
	idlestResFit := tp.isIdleResourceFit()
	for _, node := range nodes {
		nNode, ok := tp.Nodes[node.Name]
		if !ok {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes %s is not npu node",
				tp.GetPluginName(), node.Name)
			continue
		}

		if _, exist := superPodMap[nNode.SuperPodID]; !exist {
			superPodMap[nNode.SuperPodID] = &nodeGroupItem{
				group:        make(map[string]plugin.NPUNode),
				reserved:     tp.FrameAttr.ReservePodSize,
				podGroupNum:  tp.spBlock,
				superPodID:   nNode.SuperPodID,
				idlestResFit: idlestResFit,
			}
		}
		superPodMap[nNode.SuperPodID].group[node.Name] = nNode
	}
	return superPodMap
}

func (tp *module910SuperPod) filterAndCount(superPodMap map[int32]*nodeGroupItem) (int, []int32) {
	unFit := make([]int32, 0)
	totalVSP := 0
	for _, sp := range superPodMap {
		if len(sp.group) < tp.spBlock {
			unFit = append(unFit, sp.superPodID)
			continue
		}
		totalVSP += len(sp.group) / tp.spBlock
	}
	return totalVSP, unFit
}

func (tp *module910SuperPod) isAffinityNode(node plugin.NPUNode, affinity map[string]string) bool {
	for _, task := range node.Tasks {
		job, ok := tp.Jobs[task.Job]
		if !ok {
			continue
		}
		fit := 0
		klog.V(util.LogErrorLev).Infof("job %s label %v", job.Name, job.Label)
		for k, v := range affinity {
			if value, ok := job.Label[k]; ok && value == v {
				fit++
			}
		}
		if fit == len(affinity) {
			return true
		}
	}
	return false
}

func removeUnfitSuperPods(superPodMap map[int32]*nodeGroupItem, unFit []int32) {
	for _, id := range unFit {
		delete(superPodMap, id)
	}
}

func (tp *module910SuperPod) calculateAffinities(superPodMap map[int32]*nodeGroupItem, affinity map[string]string) {
	for _, node := range tp.Nodes {
		sp, ok := superPodMap[node.SuperPodID]
		if !ok {
			continue
		}
		if _, ok := sp.group[node.Name]; ok {
			continue
		}
		klog.V(util.LogErrorLev).Infof("node %s affinity %v", node.Name, affinity)
		if tp.isAffinityNode(node, affinity) {
			sp.affinities++
		}
	}
}

func (tp *module910SuperPod) initPriorityQueue(superPodMap map[int32]*nodeGroupItem) *nodeGroupList {
	ng := make(nodeGroupList, 0)
	heap.Init(&ng)
	for _, sp := range superPodMap {
		klog.V(util.LogErrorLev).Infof("super pod %d with total node %d and affinity node %d is fit this job: %s",
			sp.superPodID, len(sp.group), sp.affinities, tp.Name)
		heap.Push(&ng, sp)
	}
	return &ng
}

func selectNodesFromQueue(ng *nodeGroupList, totalRequiredSuperPod int, affable bool) map[string][]plugin.SuperNode {
	selectNodes := make(map[string][]plugin.SuperNode)
	for i := 0; i < totalRequiredSuperPod; i++ {
		if ng.Len() == 0 {
			break
		}
		n, ok := heap.Pop(ng).(*nodeGroupItem)
		if !ok {
			continue
		}
		j := 0
		selectedNodeName := make([]string, 0, n.podGroupNum)
		for _, node := range n.group {
			if j >= n.podGroupNum {
				break
			}
			selectNodes[strconv.Itoa(i)] = append(selectNodes[strconv.Itoa(i)], plugin.SuperNode{
				Name:       node.Name,
				SuperPodID: n.superPodID,
			})
			selectedNodeName = append(selectedNodeName, node.Name)
			j++
		}
		for _, nodeName := range selectedNodeName {
			delete(n.group, nodeName)
		}
		if affable {
			n.affinities += n.podGroupNum
		}

		if len(n.group) >= n.podGroupNum {
			heap.Push(ng, n)
		}
	}
	return selectNodes
}

func getJobAffinityLabel(task *api.TaskInfo) map[string]string {
	if task.Pod.Spec.Affinity == nil ||
		task.Pod.Spec.Affinity.PodAffinity == nil ||
		len(task.Pod.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
		return nil
	}
	weightedPodAffinity := task.Pod.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0]
	if weightedPodAffinity.PodAffinityTerm.LabelSelector == nil {
		return nil
	}
	return weightedPodAffinity.PodAffinityTerm.LabelSelector.MatchLabels
}

type nodeGroupItem struct {
	group        map[string]plugin.NPUNode
	affinities   int
	reserved     int
	podGroupNum  int
	index        int
	superPodID   int32
	idlestResFit bool
}

type nodeGroupList []*nodeGroupItem

// Len is the number of elements in the collection.
func (ng nodeGroupList) Len() int {
	return len(ng)
}

// Less reports whether the element with
// index i should sort before the element with index j.
func (ng nodeGroupList) Less(i, j int) bool {
	a, b := ng[i], ng[j]

	aFitExRes := len(a.group)-a.reserved >= a.podGroupNum
	bFitExRes := len(b.group)-b.reserved >= b.podGroupNum

	if aFitExRes && !bFitExRes {
		return true
	}
	if !aFitExRes && bFitExRes {
		return false
	}

	if a.affinities == b.affinities {
		if a.idlestResFit {
			return len(a.group) >= len(b.group)
		}
		return len(a.group) < len(b.group)
	}

	return a.affinities > b.affinities
}

// Swap swaps the elements with indexes i and j.
func (ng nodeGroupList) Swap(i, j int) {
	ng[i], ng[j] = ng[j], ng[i]
	ng[i].index = i
	ng[j].index = j
}

// Push appends the element x to the list.
func (ng *nodeGroupList) Push(x interface{}) {
	n := len(*ng)
	item, ok := x.(*nodeGroupItem)
	if !ok {
		return
	}
	item.index = n
	*ng = append(*ng, item)
}

// Pop removes and returns the last element from the list.
func (ng *nodeGroupList) Pop() interface{} {
	old := *ng
	n := len(old)
	item := old[n-1]
	item.index = -1
	*ng = old[0 : n-1]
	return item
}
