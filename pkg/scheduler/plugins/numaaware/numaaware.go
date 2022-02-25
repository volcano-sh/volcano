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

package numaaware

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	v1qos "k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/policy"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/provider/cpumanager"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "numa-aware"
	// NumaTopoWeight indicates the weight of numa-aware plugin.
	NumaTopoWeight = "weight"
)

type numaPlugin struct {
	sync.Mutex
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	hintProviders   []policy.HintProvider
	assignRes       map[api.TaskID]map[string]api.ResNumaSets // map[taskUID]map[nodename][resourceName]cpuset.CPUSet
	nodeResSets     map[string]api.ResNumaSets                // map[nodename][resourceName]cpuset.CPUSet
	taskBindNodeMap map[api.TaskID]string
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	plugin := &numaPlugin{
		pluginArguments: arguments,
		assignRes:       make(map[api.TaskID]map[string]api.ResNumaSets),
		taskBindNodeMap: make(map[api.TaskID]string),
	}

	plugin.hintProviders = append(plugin.hintProviders, cpumanager.NewProvider())
	return plugin
}

func (pp *numaPlugin) Name() string {
	return PluginName
}

func calculateWeight(args framework.Arguments) int {
	weight := 1
	args.GetInt(&weight, NumaTopoWeight)
	return weight
}

func (pp *numaPlugin) OnSessionOpen(ssn *framework.Session) {
	weight := calculateWeight(pp.pluginArguments)
	numaNodes := api.GenerateNumaNodes(ssn.Nodes)
	pp.nodeResSets = api.GenerateNodeResNumaSets(ssn.Nodes)

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			node := pp.nodeResSets[event.Task.NodeName]
			if _, ok := pp.assignRes[event.Task.UID]; !ok {
				return
			}

			resNumaSets, ok := pp.assignRes[event.Task.UID][event.Task.NodeName]
			if !ok {
				return
			}

			node.Allocate(resNumaSets)
			pp.taskBindNodeMap[event.Task.UID] = event.Task.NodeName
		},
		DeallocateFunc: func(event *framework.Event) {
			node := pp.nodeResSets[event.Task.NodeName]
			if _, ok := pp.assignRes[event.Task.UID]; !ok {
				return
			}

			resNumaSets, ok := pp.assignRes[event.Task.UID][event.Task.NodeName]
			if !ok {
				return
			}

			delete(pp.taskBindNodeMap, event.Task.UID)
			node.Release(resNumaSets)
		},
	})

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		if v1qos.GetPodQOS(task.Pod) != v1.PodQOSGuaranteed {
			klog.V(3).Infof("task %s isn't Guaranteed pod", task.Name)
			return nil
		}

		if fit, err := filterNodeByPolicy(task, node, pp.nodeResSets); !fit {
			return err
		}

		resNumaSets := pp.nodeResSets[node.Name].Clone()

		taskPolicy := policy.GetPolicy(node, numaNodes[node.Name])
		allResAssignMap := make(map[string]cpuset.CPUSet)
		for _, container := range task.Pod.Spec.Containers {
			providersHints := policy.AccumulateProvidersHints(&container, node.NumaSchedulerInfo, resNumaSets, pp.hintProviders)
			hit, admit := taskPolicy.Predicate(providersHints)
			if !admit {
				return fmt.Errorf("plugin %s predicates failed for task %s container %s on node %s",
					pp.Name(), task.Name, container.Name, node.Name)
			}

			klog.V(4).Infof("[numaaware] hits for task %s container '%v': %v on node %s, besthit: %v",
				task.Name, container.Name, providersHints, node.Name, hit)
			resAssignMap := policy.Allocate(&container, &hit, node.NumaSchedulerInfo, resNumaSets, pp.hintProviders)
			for resName, assign := range resAssignMap {
				allResAssignMap[resName] = allResAssignMap[resName].Union(assign)
				resNumaSets[resName] = resNumaSets[resName].Difference(assign)
			}
		}

		pp.Lock()
		defer pp.Unlock()
		if _, ok := pp.assignRes[task.UID]; !ok {
			pp.assignRes[task.UID] = make(map[string]api.ResNumaSets)
		}

		pp.assignRes[task.UID][node.Name] = allResAssignMap

		klog.V(4).Infof(" task %s's on node<%s> resAssignMap: %v",
			task.Name, node.Name, pp.assignRes[task.UID][node.Name])

		return nil
	}

	ssn.AddPredicateFn(pp.Name(), predicateFn)

	batchNodeOrderFn := func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		nodeScores := make(map[string]float64, len(nodeInfo))
		if task.NumaInfo == nil || task.NumaInfo.Policy == "" || task.NumaInfo.Policy == "none" {
			return nodeScores, nil
		}

		if _, found := pp.assignRes[task.UID]; !found {
			return nodeScores, nil
		}

		scoreList := getNodeNumaNumForTask(nodeInfo, pp.assignRes[task.UID])
		util.NormalizeScore(100, true, scoreList)

		for nodeName, score := range scoreList {
			score *= int64(weight)
			nodeScores[nodeName] = float64(score)
		}

		klog.V(4).Infof("numa-aware plugin Score for task %s/%s is: %v",
			task.Namespace, task.Name, nodeScores)
		return nodeScores, nil
	}

	ssn.AddBatchNodeOrderFn(pp.Name(), batchNodeOrderFn)
}

func filterNodeByPolicy(task *api.TaskInfo, node *api.NodeInfo, nodeResSets map[string]api.ResNumaSets) (fit bool, err error) {
	if !(task.NumaInfo == nil || task.NumaInfo.Policy == "" || task.NumaInfo.Policy == "none") {
		if node.NumaSchedulerInfo == nil {
			return false, fmt.Errorf("numa info is empty")
		}

		if node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.CPUManagerPolicy] != "static" {
			return false, fmt.Errorf("cpu manager policy isn't static")
		}

		if task.NumaInfo.Policy != node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.TopologyManagerPolicy] {
			return false, fmt.Errorf("task topology polocy[%s] is different with node[%s]",
				task.NumaInfo.Policy, node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.TopologyManagerPolicy])
		}

		if _, ok := nodeResSets[node.Name]; !ok {
			return false, fmt.Errorf("no topo information")
		}

		if nodeResSets[node.Name][string(v1.ResourceCPU)].Size() == 0 {
			return false, fmt.Errorf("cpu allocatable map is empty")
		}
	} else {
		if node.NumaSchedulerInfo == nil {
			return false, nil
		}

		if node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.CPUManagerPolicy] != "static" {
			return false, nil
		}

		if (node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.TopologyManagerPolicy] == "none") ||
			(node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.TopologyManagerPolicy] == "") {
			return false, nil
		}
	}

	return true, nil
}

func getNodeNumaNumForTask(nodeInfo []*api.NodeInfo, resAssignMap map[string]api.ResNumaSets) map[string]int64 {
	nodeNumaNumMap := make(map[string]int64)
	var mx sync.RWMutex
	workqueue.ParallelizeUntil(context.TODO(), 16, len(nodeInfo), func(index int) {
		node := nodeInfo[index]
		assignCpus := resAssignMap[node.Name][string(v1.ResourceCPU)]

		mx.Lock()
		defer mx.Unlock()
		nodeNumaNumMap[node.Name] = int64(getNumaNodeCntForCPUID(assignCpus, node.NumaSchedulerInfo.CPUDetail))
	})

	return nodeNumaNumMap
}

func getNumaNodeCntForCPUID(cpus cpuset.CPUSet, cpuDetails topology.CPUDetails) int {
	mask, _ := bitmask.NewBitMask()
	s := cpus.ToSlice()

	for _, cpuID := range s {
		mask.Add(cpuDetails[cpuID].NUMANodeID)
	}

	return mask.Count()
}

func (pp *numaPlugin) OnSessionClose(ssn *framework.Session) {
	if len(pp.taskBindNodeMap) == 0 {
		return
	}

	allocatedResSet := make(map[string]api.ResNumaSets)
	for taskID, nodeName := range pp.taskBindNodeMap {
		if _, existed := pp.assignRes[taskID]; !existed {
			continue
		}

		if _, existed := pp.assignRes[taskID][nodeName]; !existed {
			continue
		}

		if _, existed := allocatedResSet[nodeName]; !existed {
			allocatedResSet[nodeName] = make(api.ResNumaSets)
		}

		resSet := pp.assignRes[taskID][nodeName]
		for resName, set := range resSet {
			if _, existed := allocatedResSet[nodeName][resName]; !existed {
				allocatedResSet[nodeName][resName] = cpuset.NewCPUSet()
			}

			allocatedResSet[nodeName][resName] = allocatedResSet[nodeName][resName].Union(set)
		}
	}

	klog.V(4).Infof("[numaPlugin]allocatedResSet: %v", allocatedResSet)
	ssn.UpdateSchedulerNumaInfo(allocatedResSet)
}
