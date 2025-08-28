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

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (th *TorSingleLevelHandler) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo,
	scoreMap map[string]float64) error {
	if th == nil || task == nil || len(nodes) == 0 || scoreMap == nil || th.globalTorEnv == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes err: %s.", err.Error())
		return err
	}
	refreshScoreMap(nodes, scoreMap)
	nodeMaps := util.ChangeNodesToNodeMaps(nodes)
	klog.V(util.LogDebugLev).Infof("validNPUJob job is now use tor affinity")
	return th.setSingleLayerTorJobNodesScore(task, nodeMaps, scoreMap)
}

// setSingleLayerTorJobNodesScore single layer switch networking rule
func (th *TorSingleLevelHandler) setSingleLayerTorJobNodesScore(task *api.TaskInfo,
	nodeMaps map[string]*api.NodeInfo, scoreMap map[string]float64) error {
	if th == nil || !*th.Job.JobReadyTag {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogDebugLev).Infof("ScoreBestNPUNodes %s.", err)
		return nil
	}
	err := th.setSingleLayerJobServerList(nodeMaps)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("check job %s tor affinity failed: %s",
			th.Job.Name, err)
		*th.Job.JobReadyTag = false
	}
	th.scoreBestNPUNodes(task, nodeMaps, scoreMap)
	klog.V(util.LogDebugLev).Infof("batchNodeOrderFn set %s for NPU %+v.", task.Name, scoreMap)
	return err
}

// setSingleLayerJobServerList check the single layer tor whether meet the job require and set the job server list
func (th *TorSingleLevelHandler) setSingleLayerJobServerList(nodeMaps map[string]*api.NodeInfo) error {
	if th.ServerList != nil {
		return nil
	}
	if th == nil || th.globalTorEnv == nil || len(nodeMaps) == 0 {
		err := errors.New(util.ArgumentError)
		return fmt.Errorf("initTorHandlerV1 err: %s", err.Error())
	}
	if th.Job.NPUTaskNum > th.globalTorEnv.TorCount {
		return fmt.Errorf("job's task number is bigger than torCount")
	}
	th.globalTorEnv.MarkTorListByJobV1(nodeMaps, th.Job.Name, th.Job.SchedulingTaskNum)
	_ = th.globalTorEnv.SetTorFreeServerCountAndGetFullTor(th.Job.Name)
	n := th.Job.SchedulingTaskNum
	sort.Slice(th.globalTorEnv.Tors, func(i, j int) bool {
		return th.globalTorEnv.Tors[i].FreeServerCount > th.globalTorEnv.Tors[j].FreeServerCount
	})
	return th.setFillJobServerList(th.globalTorEnv.Tors, n)
}
