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
	"fmt"
	"sort"
	"strconv"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

func initJobTorInfos() jobUsedTorInfos {
	return jobUsedTorInfos{
		serverNums:        map[string]int{},
		usedTor:           []*plugin.Tor{},
		unUsedTor:         []*plugin.Tor{},
		isSingleTorJob:    false,
		sharedTorNumToAdd: noneSharedTor,
	}
}

func copyTorList(t []*plugin.Tor) []*plugin.Tor {
	tmpTors := make([]*plugin.Tor, len(t))
	copy(tmpTors, t)
	return tmpTors
}

// getSharedTorNumFromTor get a tors`s shared tor num
func getSharedTorNumFromTor(tors []*plugin.Tor) int {
	var count int
	for _, tor := range tors {
		if tor.IsSharedTor == sharedTor {
			count++
		}
	}
	return count
}

// setUsedTorAttr set the tor attr used before job restarted
func setUsedTorAttr(tors []*plugin.Tor, torNums map[string]int, addSharedTorNum int) {
	for _, tor := range tors {
		if tor.IsSharedTor != freeTor {
			continue
		}
		if tor.FreeServerCount > torNums[tor.IP]+1 && addSharedTorNum != 0 {
			tor.IsSharedTor = sharedTor
			addSharedTorNum--
			continue
		}
		tor.IsSharedTor = exclusiveTor
	}
}

func setNodeScoreByTorAttr(sMap map[string]float64, nodeName string, sl *plugin.Tor) {
	if sMap == nil {
		return
	}
	sMap[nodeName] = halfTorAffinityNodeScore
	if sl.IsSharedTor == sharedTor {
		sMap[nodeName] = sharedTorAffinityNodeScore
	}
}

func changeTorMapsToSlice(torMaps map[string]*plugin.Tor) []*plugin.Tor {
	tors := make([]*plugin.Tor, 0, len(torMaps))
	for _, tor := range torMaps {
		tors = append(tors, tor)
	}
	return tors
}

// GetPluginName return tor handler plugin name
func (th *TorHandler) GetPluginName() string { return th.pluginName }

func (th *TorHandler) scoreBestNPUNodes(task *api.TaskInfo, nodeMaps map[string]*api.NodeInfo,
	sMap map[string]float64) {
	th.sortJobServerListBySliceId()
	th.setNodeRankIndex()
	if sMap == nil {
		return
	}
	for _, sl := range th.ServerList {
		for _, server := range sl.Servers {
			setNodeScoreByTorAttr(sMap, server.Name, sl)
			if _, exist := nodeMaps[server.Name]; exist && server.NodeRank == task.Pod.Annotations[podRankIndex] {
				sMap[server.Name] = maxTorAffinityNodeScore
				return
			}
		}
	}
	klog.V(util.LogInfoLev).Infof("ScoreBestNPUNodes task<%s> sMap<%v>", task.Name, sMap)
	return
}

// sortJobServerListBySliceId sort JobServer list by SliceId
func (th *TorHandler) sortJobServerListBySliceId() {
	for _, tor := range th.ServerList {
		sort.Slice(tor.Servers, func(i, j int) bool {
			return tor.Servers[i].SliceId < tor.Servers[j].SliceId
		})
	}
}

// setNodeRankIndex set rank index for job
func (th *TorHandler) setNodeRankIndex() {
	var rankIndex int
	for _, tor := range th.ServerList {
		for _, server := range tor.Servers {
			if server.NodeRank != "" {
				return
			}
			server.NodeRank = strconv.Itoa(rankIndex)
			rankIndex++
		}
	}
}

// setFillJobServerList set the fill job server list in nslb 1.0 and single layer switch networking rule
func (th *TorHandler) setFillJobServerList(Tors []*plugin.Tor, taskNum int) error {
	var count int
	for i := len(Tors) - 1; i >= 0; i-- {
		if Tors[i].FreeServerCount < taskNum {
			continue
		}
		tmpTor := &plugin.Tor{}
		for _, k := range Tors[i].Servers {
			if k.CurrentJob != nil && *k.CurrentJob == th.Job.Name {
				count++
				tmpTor.Servers = append(tmpTor.Servers, k)
			}
			if count == taskNum {
				break
			}
		}
		th.ServerList = append(th.ServerList, tmpTor)
		return nil
	}
	return fmt.Errorf("tor check failed not enough resource for job")
}

// isFillJob job is fill job
func isFillJob(label map[string]string, nTaskNum int) bool {
	return label[TorAffinityKey] == LargeModelTag && nTaskNum < fillJobMaxNPUTaskNum
}

func refreshScoreMap(nodes []*api.NodeInfo, scoreMap map[string]float64) {
	if scoreMap == nil {
		return
	}
	for _, node := range nodes {
		scoreMap[node.Name] = 0
	}
}
