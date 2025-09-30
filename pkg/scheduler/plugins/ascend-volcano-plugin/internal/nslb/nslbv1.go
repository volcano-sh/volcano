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
Package nslb is using for HuaWei Ascend pin tor affinity.
*/
package nslb

import (
	"errors"
	"fmt"
	"sort"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// PreStartAction pre-processing actions for rescheduling
func (th *TorHandlerV1) PreStartAction(ssn *framework.Session) error {
	if th.Job == nil {
		return fmt.Errorf("prestart action is failed by job is nil")
	}
	if th.globalTorEnv == nil {
		return fmt.Errorf("prestart action is failed by globalTorEnv is nil")
	}
	if th.Job.SchedulingTaskNum == 0 || th.Job.SchedulingTaskNum == len(th.Job.Tasks) {
		return nil
	}
	th.initEnableSliceId()
	return nil
}

func (th *TorHandlerV1) initEnableSliceId() {
	usedTorCount := make([]int, th.globalTorEnv.TorCount)
	for _, task := range th.Job.Tasks {
		if task.NodeName != "" {
			server := th.globalTorEnv.GetServerMaps()[task.NodeName]
			if server == nil {
				continue
			}
			if server.SliceId < 0 || server.SliceId >= th.globalTorEnv.TorCount {
				continue
			}
			usedTorCount[server.SliceId]++
		}
	}
	th.enableSliceId = getMinIndex(usedTorCount)
}

// CheckNodeNPUByTask check nod npu meet task req
func (th *TorHandlerV1) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if th == nil || task == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("CheckNodeNPUByTask err: %s.", err.Error())
		return err
	}
	klog.V(util.LogDebugLev).Infof("%s NodePredicate %s select successes.", th.GetPluginName(), node.Name)
	if th.Job.SchedulingTaskNum < len(th.Job.Tasks) {
		return th.checkTorJobSinglePodDelete(node)
	}
	return nil
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (th *TorHandlerV1) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo,
	scoreMap map[string]float64) error {
	if th == nil || task == nil || len(nodes) == 0 || scoreMap == nil || th.globalTorEnv == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes err: %s.", err.Error())
		return err
	}
	refreshScoreMap(nodes, scoreMap)
	if th.Job.SchedulingTaskNum < len(th.Job.Tasks) {
		return nil
	}
	nodeMaps := util.ChangeNodesToNodeMaps(nodes)
	return th.setTorAffinityJobNodesScore(task, nodeMaps, scoreMap)
}

// setTorAffinityJobNodesScore nslb 1.0 rule
func (th *TorHandlerV1) setTorAffinityJobNodesScore(task *api.TaskInfo,
	nodeMaps map[string]*api.NodeInfo, scoreMap map[string]float64) error {
	if !*th.Job.JobReadyTag {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogDebugLev).Infof("ScoreBestNPUNodes %s.", err)
		return nil
	}

	result := th.setTorHandlerServerList(nodeMaps)
	if result != nil {
		klog.V(util.LogErrorLev).Infof("check job %s tor affinity failed: %s", th.Job.Name, result)
		switch th.Job.Label[TorAffinityKey] {
		case LargeModelTag:
			*th.Job.JobReadyTag = false
		case NormalSchema:
			th.setNormalJobServerList(th.Job.SchedulingTaskNum)
		default:
			return nil
		}
	}
	th.scoreBestNPUNodes(task, nodeMaps, scoreMap)
	klog.V(util.LogDebugLev).Infof("batchNodeOrderFn set %s for NPU %+v.", task.Name, scoreMap)
	return result
}

func (th *TorHandlerV1) setTorHandlerServerList(nodeMaps map[string]*api.NodeInfo) error {
	if len(th.ServerList) != 0 {
		klog.V(util.LogDebugLev).Infof("InitTorHandlerV1 len(serverList):%d", len(th.ServerList))
		return nil
	}
	schedulingTaskNum := th.Job.SchedulingTaskNum
	th.globalTorEnv.MarkTorListByJobV1(nodeMaps, th.Job.Name, schedulingTaskNum)
	fullTorNum := th.globalTorEnv.SetTorFreeServerCountAndGetFullTor(th.Job.Name)
	sort.Slice(th.globalTorEnv.Tors, func(i, j int) bool {
		return th.globalTorEnv.Tors[i].FreeServerCount > th.globalTorEnv.Tors[j].FreeServerCount
	})
	netSliceNum := th.globalTorEnv.TorCount
	if schedulingTaskNum < netSliceNum {
		if err := th.setFillJobServerList(th.globalTorEnv.Tors, schedulingTaskNum); err == nil ||
			isFillJob(th.Job.Label, th.Job.NPUTaskNum) {
			return err
		}
	}
	taskRow, taskColumn := getTaskRowAndTaskColumn(schedulingTaskNum, netSliceNum)
	if taskRow == -1 {
		return fmt.Errorf("taskRow and taskColumn is illegal")
	}
	if taskRow+1 <= fullTorNum {
		th.setJobServerList(th.globalTorEnv.Tors, taskRow, taskColumn)
		th.markMulJobServerList()
		return nil
	}
	logicTor, fullTorNum := th.globalTorEnv.GetLogicTorsAndFullTorNum(th.Job.Name, taskColumn,
		taskRow, netSliceNum)
	if logicTor == nil {
		return fmt.Errorf("logicTor is illegal")
	}
	if taskRow < 1 && taskColumn != netSliceNum-1 {
		err := th.setFillJobServerList(logicTor, schedulingTaskNum)
		th.markMulJobServerList()
		return err
	}

	th.setJobServerList(logicTor, taskRow, taskColumn)
	th.markMulJobServerList()
	return nil
}

// setJobServerList set job server list and update the job in sHandler
func (th *TorHandlerV1) setJobServerList(tors []*plugin.Tor, taskRow, taskColumn int) {
	if th == nil || len(tors) == 0 {
		klog.V(util.LogDebugLev).Infof("setJobServerList failed:%s", util.ArgumentError)
		return
	}
	if taskRow >= len(tors) {
		klog.V(util.LogDebugLev).Infof("invalid taskRow: %d, tors length: %d", taskRow, len(tors))
		return
	}
	tmpTors := copyTorList(tors[:taskRow])
	tmpTor := &plugin.Tor{}
	tmpTor.Servers = append(tmpTor.Servers, tors[taskRow].Servers[:taskColumn+1]...)
	tmpTors = append(tmpTors, tmpTor)
	th.ServerList = tmpTors
}

// markMulJobServerList mark the job if the server job used is over 1 tor
func (th *TorHandlerV1) markMulJobServerList() {
	if th.ServerList == nil {
		return
	}
	for _, tor := range th.ServerList {
		if tor.Servers == nil {
			continue
		}
		for _, server := range tor.Servers {
			server.IsUsedByMulJob = true
		}
	}
}

// setNormalJobServerList set the server list of normal job in nslb 1.0
func (th *TorHandlerV1) setNormalJobServerList(schedulingTaskNum int) {
	th.ServerList = []*plugin.Tor{}
	var count int
	for _, tor := range th.globalTorEnv.Tors {
		tmpTor := &plugin.Tor{IP: tor.IP, Id: tor.Id}
		for _, server := range tor.Servers {
			if server.CurrentJob != nil && *server.CurrentJob == th.Job.Name {
				tmpTor.Servers = append(tmpTor.Servers, server)
				count++
			}
			if count != schedulingTaskNum {
				continue
			}
			th.ServerList = append(th.ServerList, tmpTor)
			if len(th.ServerList) > 1 {
				th.markMulJobServerList()
			}
			return
		}
		th.ServerList = append(th.ServerList, tmpTor)
	}
	*th.Job.JobReadyTag = false
}

// checkTorJobSinglePodDelete valid node.
func (th *TorHandlerV1) checkTorJobSinglePodDelete(vcNode plugin.NPUNode) error {
	if isFillJob(th.Job.Label, th.Job.NPUTaskNum) {
		return fmt.Errorf("check node err by: large model job can not over tor")
	}
	server, isTorNode := th.globalTorEnv.GetServerMaps()[vcNode.Name]
	if !isTorNode {
		return fmt.Errorf("node is not in tor node list by not get server")
	}

	torIp, getTorIp := th.globalTorEnv.GetTorIpMap()[vcNode.Name]
	if !getTorIp {
		return fmt.Errorf("node is not in tor node list by not get tor ip")
	}

	tor, isTor := th.globalTorEnv.GetTorMaps()[torIp]
	if !isTor {
		return fmt.Errorf("node is not in tor node list by not get tor")
	}

	if th.enableSliceId != server.SliceId || tor.HasAcrossJob(false, th.Job.Name) {
		return fmt.Errorf("node sliceId is not meet task require")
	}
	return nil
}

func getTaskRowAndTaskColumn(nTaskNum int, netSliceNum int) (int, int) {
	if netSliceNum == 0 {
		return -1, -1
	}
	taskRow := nTaskNum / netSliceNum
	if nTaskNum%netSliceNum == 0 {
		taskRow = nTaskNum/netSliceNum - 1
	}
	taskColumn := (nTaskNum%netSliceNum + netSliceNum - 1) % netSliceNum
	return taskRow, taskColumn
}

func getMinIndex(t []int) int {
	var minVal int
	var minValIndex int
	for i, v := range t {
		if minVal < v {
			minValIndex = i
		}
	}
	return minValIndex
}
