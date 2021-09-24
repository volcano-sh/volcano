package ccinuma

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "cci-numa-aware"
	// Weight indicates the weight of cci-numa-aware plugin.
	Weight = "weight"
)

type cciNumaPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	taskAssignedResMap map[api.TaskID]map[string]ResNumaSetType
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	return &cciNumaPlugin{
		pluginArguments: arguments,
		taskAssignedResMap: make(map[api.TaskID]map[string]ResNumaSetType),
	}
}

func (pp *cciNumaPlugin) Name() string {
	return PluginName
}

func calculateWeight(args framework.Arguments) int {
	weight := 1
	args.GetInt(&weight, Weight)
	return weight
}

func GenerateNodeNumatopoInfo(nodes map[string]*api.NodeInfo) map[string]*api.NumatopoInfo {
	nodeNumaInfo := make(map[string]*api.NumatopoInfo)
	for _, nodeInfo := range nodes {
		nodeNumaInfo[nodeInfo.Name] = nodeInfo.NumaSchedulerInfo.DeepCopy()
	}
	return nodeNumaInfo
}

func (pp *cciNumaPlugin) assignedRes(task *api.TaskInfo) {
	if len(task.NodeName) == 0 {
		return
	}

	AssignedResMap := pp.taskAssignedResMap[task.UID][task.NodeName]
	for resName := range AssignedResMap {
		for numaId, quantity := range AssignedResMap[resName] {
			if _, ok := task.NumaInfo.ResMap[numaId]; !ok {
				task.NumaInfo.ResMap[numaId] = make(v1.ResourceList)
			}

			task.NumaInfo.ResMap[numaId][resName] =  api.ResFloat642Quantity(resName, quantity)
		}
	}
}

func (pp *cciNumaPlugin) releaseRes(task *api.TaskInfo) {
	if len(task.NodeName) == 0 {
		return
	}

	task.NumaInfo.ResMap = make(map[int]v1.ResourceList)
}

func (pp *cciNumaPlugin) OnSessionOpen(ssn *framework.Session) {
	weight := calculateWeight(pp.pluginArguments)
	nodeNumaInfo := GenerateNodeNumatopoInfo(ssn.Nodes)
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			task := event.Task
			nodeInfo := nodeNumaInfo[task.NodeName]
			AssignedResMap := pp.taskAssignedResMap[task.UID][task.NodeName]

			for resName := range AssignedResMap {
				for numaID, quantity := range AssignedResMap[resName] {
					nodeInfo.NumaResMap[string(resName)].UsedPerNuma[numaID] += quantity
				}
			}

			pp.assignedRes(task)
		},
		DeallocateFunc: func(event *framework.Event) {
			task := event.Task
			nodeInfo := nodeNumaInfo[task.NodeName]
			AssignedResMap := pp.taskAssignedResMap[task.UID][task.NodeName]

			for resName := range AssignedResMap {
				for numaID, quantity := range AssignedResMap[resName] {
					nodeInfo.NumaResMap[string(resName)].UsedPerNuma[numaID] -= quantity
				}
			}

			pp.releaseRes(task)
		},
	})

	batchNodeOrderFn := func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		nodeScores := make(map[string]float64, len(nodeInfo))

		workqueue.ParallelizeUntil(context.TODO(), 16, len(nodeInfo), func(index int) {
			hints := make([][]TopologyHint, 0)
			nodeNumaIdleMap := GetNodeNumaRes(nodeNumaInfo[nodeInfo[index].Name])
			var numaSlice []int
			for key := range nodeNumaIdleMap[v1.ResourceCPU] {
				numaSlice = append(numaSlice, key)
			}

			if task.Resreq.MilliCPU > 0 {
				hints = append(hints, GenerateTopologyHints(task.Resreq.MilliCPU, nodeNumaIdleMap[v1.ResourceCPU]))
			}
			if task.Resreq.Memory > 0 {
				hints = append(hints, GenerateTopologyHints(task.Resreq.MilliCPU, nodeNumaIdleMap[v1.ResourceMemory]))
			}
			for resName, quantity := range task.Resreq.ScalarResources {
				if quantity == 0 {
					continue
				}
				hints = append(hints, GenerateTopologyHints(quantity, nodeNumaIdleMap[resName]))
			}

			bestHit := MergeFilteredHints(task, nodeNumaIdleMap, numaSlice, hints)
			taskAssignedRes := AllocateNumaResForTask(task, bestHit, nodeNumaIdleMap)
			pp.taskAssignedResMap[task.UID][nodeInfo[index].Name] = taskAssignedRes
		})

		scoreList := GetNodeNumaNumForTask(nodeInfo, pp.taskAssignedResMap[task.UID])
		util.NormalizeScore(90, true, scoreList)

		for nodeName, score := range scoreList {
			score *= (score + 10) * int64(weight)
			nodeScores[nodeName] = float64(score)
		}

		klog.V(4).Infof("numa-aware plugin Score for task %s/%s is: %v",
			task.Namespace, task.Name, nodeScores)

		return nodeScores, nil
	}

	ssn.AddBatchNodeOrderFn(pp.Name(), batchNodeOrderFn)
}

func (pp *cciNumaPlugin) OnSessionClose(ssn *framework.Session) {
}