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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// PreStartAction pre-processing actions for rescheduling
func (th *TorHandlerV2) PreStartAction(ssn *framework.Session) error {
	if th.Job == nil {
		return fmt.Errorf("prestart action is failed by job is nil")
	}
	if th.globalTorEnv == nil {
		return fmt.Errorf("prestart action is failed by globalTorEnv is nil")
	}
	if th.Job.SchedulingTaskNum == 0 || th.Job.SchedulingTaskNum == len(th.Job.Tasks) {
		return nil
	}
	// obtain the tors which job used and not used  before restart
	th.initUsedTorInfos()
	th.initTorBlackLists()
	return nil
}

// CheckNodeNPUByTask check nod npu meet task req
func (th *TorHandlerV2) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if th == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("CheckNodeNPUByTask err: %s.", err.Error())
		return err
	}
	klog.V(util.LogDebugLev).Infof("%s NodePredicate %s select successes.", th.GetPluginName(), node.Name)
	return th.checkTorJobSinglePodDelete(node)
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (th *TorHandlerV2) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo,
	scoreMap map[string]float64) error {
	if th == nil || task == nil || len(nodes) == 0 || scoreMap == nil || th.globalTorEnv == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes err: %s.", err.Error())
		return err
	}
	refreshScoreMap(nodes, scoreMap)
	nodeMaps := util.ChangeNodesToNodeMaps(nodes)
	return th.setTorAffinityJobNodesScore(task, nodeMaps, scoreMap)
}

// UseAnnotation select npu for task from node
func (th *TorHandlerV2) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if th == nil || task == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("UseAnnotation err: %s.", err.Error())
		return nil
	}
	th.setNslbV2PodAnnotation(task, node.Name)
	return &node
}

// setTorAffinityJobNodesScore nslb 2.0 rule
func (th *TorHandlerV2) setTorAffinityJobNodesScore(task *api.TaskInfo,
	nodeMaps map[string]*api.NodeInfo, scoreMap map[string]float64) error {
	if !*th.Job.JobReadyTag {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogDebugLev).Infof("ScoreBestNPUNodes err: %s.", err)
		return nil
	}
	defer func() {
		th.scoreBestNPUNodes(task, nodeMaps, scoreMap)
	}()
	if th.ServerList != nil {
		return nil
	}
	th.globalTorEnv.MarkTorListByJobV2(nodeMaps, th.Job.Name)
	if th.Job.SchedulingTaskNum != th.Job.NPUTaskNum {
		err := th.setServerListForPartialPods()
		if err != nil {
			*th.Job.JobReadyTag = false
		}
		return err
	}

	tmpTors := deepCopyTorList(th.globalTorEnv.Tors)
	result := th.setTorHandlerServerList(nodeMaps)
	if result != nil {
		// recovery the Tors in global
		th.globalTorEnv.RecoveryGlobalTor(tmpTors)
		klog.V(util.LogErrorLev).Infof("check job %s tor affinity failed: %s", th.Job.Name, result)
		*th.Job.JobReadyTag = false
	}
	klog.V(util.LogDebugLev).Infof("batchNodeOrderFn Get %s for NPU %+v.", task.Name, scoreMap)
	return result
}

// setJobAvailableNodes obtain the node that a job can be scheduled in nslb 2.0
func (th *TorHandlerV2) setTorHandlerServerList(nodeMaps map[string]*api.NodeInfo) error {
	if th == nil || th.globalTorEnv == nil || len(nodeMaps) == 0 {
		err := errors.New(util.ArgumentError)
		return fmt.Errorf("initTorHandlerV2 err: %s", err.Error())
	}
	nTaskNum := th.Job.NPUTaskNum
	netSliceNum := th.globalTorEnv.TorCount
	if nTaskNum < netSliceNum {
		if err := th.setNotOverTorCountJobServerList(nTaskNum); err == nil ||
			isFillJob(th.Job.Label, th.Job.NPUTaskNum) {
			return err
		}
	}
	if nTaskNum > getLargeModelMaxServerNum(th.globalTorEnv.Tors, th.globalTorEnv.GetSharedTorNum()) {
		if th.Job.Label[TorAffinityKey] == LargeModelTag {
			return errors.New("set tor failed by not enough nodes")
		}
		return th.setNormalJobServerList(nTaskNum)
	}

	n, tor := th.setExclusiveTorAndGetSharedTorNum(
		getNotShareAndFreeTorServer(th.globalTorEnv.Tors, descOrder), nTaskNum)
	if n == 0 {
		return nil
	}
	sharedTors := getSharedTorServer(th.globalTorEnv.Tors, ascOrder)
	if tor == nil || n <= getMaxSharedTorServerNum(sharedTors, th.globalTorEnv.GetSharedTorNum()) {
		th.setBestSharedTorServer(sharedTors, th.globalTorEnv.GetSharedTorNum(), n)
		return nil
	}
	th.setOneTorServer(tor, sharedTor, n)
	return nil
}

// setBestSharedTorServer get the best shared tor for job ,and set shared tor attr
func (th *TorHandlerV2) setBestSharedTorServer(tors []*plugin.Tor, sharedTorNum, serverNum int) {
	if len(tors) == 0 || (sharedTorNum != oneTor && sharedTorNum != twoTor) {
		return
	}
	if sharedTorNum == 1 {
		th.setOneTorServer(getOneSharedTorServer(tors, serverNum), 1, serverNum)
		return
	}
	th.setTwoSharedTorServer(tors, serverNum)
}

// setTwoSharedTorServer set 2 shared tor attr
func (th *TorHandlerV2) setTwoSharedTorServer(tors []*plugin.Tor, serverNum int) {
	if len(tors) == 0 {
		return
	}
	if len(tors) == 1 {
		th.setOneTorServer(tors[0], 1, serverNum)
		return
	}
	for i := 0; i < len(tors)-1; i++ {
		if (tors[i].FreeServerCount + tors[i+1].FreeServerCount) < serverNum {
			continue
		}
		th.setOneTorServer(tors[i], 1, tors[i].FreeServerCount)
		th.setOneTorServer(tors[i+1], 1, serverNum-tors[i].FreeServerCount)
	}
}

// setNormalJobServerList set the normal job server list in nslb 2.0
func (th *TorHandlerV2) setNormalJobServerList(nTaskNum int) error {
	tors := th.globalTorEnv.Tors
	sharedTorNum, _ := th.setExclusiveTorAndGetSharedTorNum(getNotShareAndFreeTorServer(tors, descOrder), nTaskNum)
	normalTorNum, tor := th.setTorAndGetSharedTorNum(getUnhealthyTorServer(tors, ascOrder), sharedTor, sharedTorNum)
	if normalTorNum == 0 {
		return nil
	}
	if tor != nil {
		th.setOneUnhealthySharedTor(tor, normalTorNum)
		return nil
	}
	n, tmpTor := th.setTorAndGetSharedTorNum(getHealthyTorUsedByNormalJob(tors, ascOrder), sharedTor, normalTorNum)
	if n == 0 {
		return nil
	}
	if tmpTor != nil {
		th.setOneUnhealthySharedTor(tmpTor, n)
		return nil
	}
	return errors.New("not enough node for normal job to schedule")
}

// setOneUnhealthySharedTor set 1  unhealthy shared tor attr
func (th *TorHandlerV2) setOneUnhealthySharedTor(tor *plugin.Tor, serverNum int) {
	t := initTempTor(tor, sharedTor, unhealthyTor)
	tor.IsSharedTor = sharedTor
	tor.IsHealthy = unhealthyTor
	var n int
	for _, sr := range tor.Servers {
		if sr.CurrentJob != nil && *sr.CurrentJob == th.Job.Name {
			t.Servers = append(t.Servers, sr)
			n++
		}
		if n == serverNum {
			th.ServerList = append(th.ServerList, t)
			return
		}
	}
}

// setExclusiveTorAndGetSharedTorNum set  exclusive Tor ,get  not scheduled pod num and last not filled tor
func (th *TorHandlerV2) setExclusiveTorAndGetSharedTorNum(tors []*plugin.Tor, serverNum int) (int, *plugin.Tor) {
	return th.setTorAndGetSharedTorNum(tors, exclusiveTor, serverNum)
}

// setTorAndGetSharedTorNum set tor attr and return not scheduled pod num , last not filled tor
func (th *TorHandlerV2) setTorAndGetSharedTorNum(tors []*plugin.Tor, isShared, serverNum int) (int, *plugin.Tor) {
	if len(tors) == 0 {
		return serverNum, nil
	}
	var isHealthyTor int
	for _, tor := range tors {
		if serverNum < tor.FreeServerCount {
			return serverNum, tor
		}
		isHealthyTor = healthyTor
		if isShared == sharedTor {
			isHealthyTor = unhealthyTor
		}
		// if isShared  represent sharedTor in this func
		tor.IsHealthy = isHealthyTor
		th.setOneTorServer(tor, isShared, tor.FreeServerCount)
		serverNum -= tor.FreeServerCount
	}
	return serverNum, nil
}

// setOneTorServer set 1 shared tor attr
func (th *TorHandlerV2) setOneTorServer(tor *plugin.Tor, isShared, serverNum int) {
	if tor == nil || serverNum <= 0 {
		return
	}
	t := initTempTor(tor, isShared, healthyTor)
	tor.IsSharedTor = isShared
	var n int
	for _, sr := range tor.Servers {
		if sr.CurrentJob != nil && *sr.CurrentJob == th.Job.Name {
			t.Servers = append(t.Servers, sr)
			n++
		}
		if n == serverNum {
			th.ServerList = append(th.ServerList, t)
			return
		}
	}
}

// setNotOverTorCountJobServerList set the job server List
// if the task num of the job is ont over the server num of a tor
func (th *TorHandlerV2) setNotOverTorCountJobServerList(nTaskNum int) error {
	var err error
	globalTor := th.globalTorEnv.Tors
	if err = th.setFillJobServerList(getNotShareTorServer(globalTor, ascOrder), nTaskNum); err == nil {
		return nil
	}
	if err = th.setFillJobServerList(getUnhealthyTorServer(globalTor, ascOrder), nTaskNum); err == nil {
		return nil
	}
	if err = th.setFillJobServerList(getSharedTorServer(globalTor, ascOrder), nTaskNum); err == nil {
		return nil
	}
	if err = th.setFillJobServerList(getNotShareAndFreeTorServer(globalTor, ascOrder), nTaskNum); err == nil {
		return nil
	}
	return err
}

// setFillJobServerList set the fill job server list in nslb 2.0
func (th *TorHandlerV2) setFillJobServerList(Tors []*plugin.Tor, taskNum int) error {
	var count int
	for i := 0; i < len(Tors); i++ {
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
	return fmt.Errorf("tor check failed not enough resource for fill job")
}

func (th *TorHandlerV2) setNslbV2PodAnnotation(task *api.TaskInfo, nodeName string) {
	task.Pod.Annotations[isSharedTor] = strconv.Itoa(freeTor)
	task.Pod.Annotations[isHealthy] = strconv.Itoa(healthyTor)
	torIp := th.globalTorEnv.GetTorIpMap()[nodeName]
	tor, getTor := th.globalTorEnv.GetTorMaps()[torIp]
	if getTor && len(th.ServerList) > 1 {
		task.Pod.Annotations[isSharedTor] = strconv.Itoa(tor.IsSharedTor)
		task.Pod.Annotations[isHealthy] = strconv.Itoa(tor.IsHealthy)
	}
	defer func() {
		if task.Pod.Annotations[isSharedTor] == strconv.Itoa(sharedTor) {
			task.Pod.Annotations[SharedTorIp] = tor.IP
		}
	}()
	if th.Job.SchedulingTaskNum == len(th.Job.Tasks) {
		return
	}
	if th.oldTorInfos.isSingleTorJob {
		return
	}
	if tor.IsSharedTor != freeTor {
		task.Pod.Annotations[isSharedTor] = strconv.Itoa(tor.IsSharedTor)
		task.Pod.Annotations[isHealthy] = strconv.Itoa(tor.IsHealthy)
		return
	}
	if th.oldTorInfos.sharedTorNumToAdd <= 0 {
		task.Pod.Annotations[isSharedTor] = strconv.Itoa(exclusiveTor)
		tor.IsSharedTor = exclusiveTor
		task.Pod.Annotations[isHealthy] = strconv.Itoa(healthyTor)
	}
}

func (th *TorHandlerV2) initUsedTorInfos() {
	torInfos := initJobTorInfos()
	usedTors := make(map[string]*plugin.Tor)
	usedSharedTor := make(sets.String)
	for _, task := range th.Job.Tasks {
		if task.NodeName != "" {
			torIp := th.globalTorEnv.GetTorIpMap()[task.NodeName]
			torInfos.serverNums[torIp]++
			usedTors[torIp] = th.globalTorEnv.GetTorMaps()[torIp]
		}
		if task.Annotation[isSharedTor] == freeTorAnno {
			torInfos.isSingleTorJob = true
		}
		torIp, ok := th.globalTorEnv.GetTorIpMap()[task.NodeName]
		if !ok {
			klog.V(util.LogWarningLev).Infof("cannot find tor ip by task %s node name, skip", task.Name)
			continue
		}
		tor := th.globalTorEnv.GetTorMaps()[torIp]
		if tor.IsSharedTor == sharedTor {
			usedSharedTor.Insert(tor.IP)
		}
	}
	torInfos.sharedTorNumToAdd = th.globalTorEnv.GetSharedTorNum() - usedSharedTor.Len()
	for _, tor := range th.globalTorEnv.GetTorMaps() {
		if _, ok := usedTors[tor.IP]; !ok {
			torInfos.unUsedTor = append(torInfos.unUsedTor, tor)
		}
	}
	torInfos.usedTorMaps = usedTors
	torInfos.usedTor = changeTorMapsToSlice(usedTors)
	th.oldTorInfos = torInfos
}

// setServerListForPartialPods sets the serverList for a task when not all of its pods are scheduled.
func (th *TorHandlerV2) setServerListForPartialPods() error {
	if th == nil || th.globalTorEnv == nil {
		return fmt.Errorf(util.ArgumentError)
	}
	// obtain the num of shared tors that Job will use
	sharedNum := getSharedTorNumFromTor(th.oldTorInfos.usedTor)
	if sharedNum > th.globalTorEnv.GetSharedTorNum() {
		return fmt.Errorf("shared tor num is over global shared tor num")
	}
	addSharedTorNum := th.globalTorEnv.GetSharedTorNum() - sharedNum
	serNum := getJobFreeServerNum(th.Job.Name, th.oldTorInfos.usedTor)
	// if the tor which job used before restart is enough for job, return
	if th.Job.SchedulingTaskNum <= serNum {
		th.ServerList = copyTorList(th.oldTorInfos.usedTor)
		setUsedTorAttr(th.ServerList, th.oldTorInfos.serverNums, addSharedTorNum)
		return nil
	}
	// if job used tor num is 1, and used tor node num is not enough for job. job end this rescheduling process
	if isFillJob(th.Job.Label, th.Job.NPUTaskNum) {
		return fmt.Errorf("not enough tor for fill job restart")
	}
	// if otherTor max node is lower than notScheduleNum. job end this rescheduling process
	notScheduleNum := th.Job.SchedulingTaskNum - serNum
	if getLargeModelMaxServerNum(th.oldTorInfos.unUsedTor, addSharedTorNum) < notScheduleNum {
		return fmt.Errorf("not enough tor for job restart")
	}
	// add the used tor and mark the used tor attr. if tor is shared, skip it. if tor is freeTor, mark it exclusive
	th.ServerList = copyTorList(th.oldTorInfos.usedTor)
	setUsedTorAttr(th.ServerList, th.oldTorInfos.serverNums, noneSharedTor)
	// add exclusive tor from the free tor in the other tor
	n, tor := th.setExclusiveTorAndGetSharedTorNum(
		getNotShareAndFreeTorServer(th.oldTorInfos.unUsedTor, ascOrder), notScheduleNum)
	if n == 0 {
		return nil
	}
	enableSharedTor := getSharedTorServer(th.oldTorInfos.unUsedTor, ascOrder)
	if tor == nil || n <= getMaxSharedTorServerNum(enableSharedTor, addSharedTorNum) {
		th.setBestSharedTorServer(enableSharedTor, addSharedTorNum, n)
		return nil
	}
	if addSharedTorNum > 0 {
		th.setOneTorServer(tor, sharedTor, n)
		return nil
	}
	th.setOneTorServer(tor, exclusiveTor, n)
	return nil
}

// checkTorJobSinglePodDelete valid node.
func (th *TorHandlerV2) checkTorJobSinglePodDelete(vcNode plugin.NPUNode) error {
	if th.oldTorInfos.torBlackList.Has(th.globalTorEnv.GetTorIpMap()[vcNode.Name]) {
		return fmt.Errorf("tor check failed node by nslb2.0")
	}
	return nil
}

func (th *TorHandlerV2) initTorBlackLists() {
	if th.oldTorInfos.torBlackList == nil {
		th.oldTorInfos.torBlackList = make(sets.String)
	}
	for _, tor := range th.globalTorEnv.GetTorMaps() {
		if _, ok := th.oldTorInfos.usedTorMaps[tor.IP]; ok {
			continue
		}
		if th.oldTorInfos.isSingleTorJob {
			th.oldTorInfos.torBlackList.Insert(tor.IP)
		}
		if tor.IsSharedTor == exclusiveTor || (tor.IsSharedTor == sharedTor && th.oldTorInfos.sharedTorNumToAdd <= 0) {
			th.oldTorInfos.torBlackList.Insert(tor.IP)
		}
	}
}

// getTorServer get tors by tor is shared and is healthy
func getTorServer(tors []*plugin.Tor, isShare int, isHealthy int, sortType string) []*plugin.Tor {
	tmpTors := initTorsByTorAttr(tors, isShare, isHealthy)
	sort.Slice(tmpTors, func(i, j int) bool {
		if sortType == descOrder {
			return tmpTors[i].FreeServerCount > tmpTors[j].FreeServerCount
		}
		return tmpTors[i].FreeServerCount < tmpTors[j].FreeServerCount
	})
	return tmpTors
}

// initTorsByTorAttr init a tors by tor is shared and is healthy
func initTorsByTorAttr(tors []*plugin.Tor, isShare, isHealthy int) []*plugin.Tor {
	var tmpTors []*plugin.Tor
	for _, tor := range tors {
		if (tor.IsSharedTor == isShare || isShare == allTor) && (tor.IsHealthy == isHealthy || isHealthy == allTor) {
			tmpTors = append(tmpTors, tor)
		}
	}
	return tmpTors
}

// getSharedTorServer get healthy shared tors
func getSharedTorServer(tors []*plugin.Tor, sortType string) []*plugin.Tor {
	return getTorServer(tors, sharedTor, healthyTor, sortType)
}

// getMaxSharedTorServerNum get max shared tor num a job can use
func getMaxSharedTorServerNum(tors []*plugin.Tor, sharedTorNum int) int {
	n := len(tors)
	if n == 0 || sharedTorNum <= 0 {
		return 0
	}
	if n == 1 {
		return tors[0].FreeServerCount
	}
	switch sharedTorNum {
	case oneTor:
		return tors[n-oneTor].FreeServerCount
	case twoTor:
		return tors[n-oneTor].FreeServerCount + tors[n-twoTor].FreeServerCount
	default:
		return 0
	}
}

// getNotShareTorServer get the exclusiveTor tors
func getNotShareTorServer(tors []*plugin.Tor, sortType string) []*plugin.Tor {
	return getTorServer(tors, exclusiveTor, healthyTor, sortType)

}

// getNotShareAndFreeTorServer get the free tors
func getNotShareAndFreeTorServer(tors []*plugin.Tor, sortType string) []*plugin.Tor {
	return getTorServer(tors, freeTor, healthyTor, sortType)
}

// getLargeModelMaxServerNum get the node num that nslb 2.0 job can use at most
func getLargeModelMaxServerNum(tors []*plugin.Tor, sharedTorNum int) int {
	var n int
	for _, tor := range getNotShareAndFreeTorServer(tors, descOrder) {
		n += tor.FreeServerCount
	}
	return n + getMaxSharedTorServerNum(getSharedTorServer(tors, ascOrder), sharedTorNum)
}

// getUnhealthyTorServer get unhealthy shared tors
func getUnhealthyTorServer(tors []*plugin.Tor, sortType string) []*plugin.Tor {
	return getTorServer(tors, sharedTor, unhealthyTor, sortType)
}

// getHealthyTorUsedByNormalJob get shared tors only used by normal job
func getHealthyTorUsedByNormalJob(tors []*plugin.Tor, sortType string) []*plugin.Tor {
	var tmpTors []*plugin.Tor
	allShareTor := getTorServer(tors, sharedTor, healthyTor, sortType)
	for _, tor := range allShareTor {
		if !tor.IsUsedByAcrossLargeModelJob() {
			tmpTors = append(tmpTors, tor)
		}
	}
	return tmpTors
}

func initTempTor(tor *plugin.Tor, isShared, isHealthy int) *plugin.Tor {
	return &plugin.Tor{
		IsSharedTor: isShared,
		IsHealthy:   isHealthy,
		Id:          tor.Id,
		IP:          tor.IP,
	}
}

func getOneSharedTorServer(tors []*plugin.Tor, serverNum int) *plugin.Tor {
	if len(tors) == 0 {
		return nil
	}
	for _, tor := range tors {
		if tor.FreeServerCount < serverNum {
			continue
		}
		return tor
	}
	return nil
}

func getJobFreeServerNum(jobUid api.JobID, tors []*plugin.Tor) int {
	var num int
	for _, tor := range tors {
		for _, s := range tor.Servers {
			if s.CurrentJob != nil && *s.CurrentJob == jobUid {
				num++
			}
		}
	}
	return num
}

func deepCopyTorList(t []*plugin.Tor) []*plugin.Tor {
	str, err := json.Marshal(t)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("deepCopyTorList Marshal %s", err)
		return nil
	}
	var tmpTor []*plugin.Tor
	if unMarErr := json.Unmarshal(str, &tmpTor); unMarErr != nil {
		klog.V(util.LogErrorLev).Infof("deepCopyTorList Unmarshal %s", unMarErr)
		return nil
	}
	return tmpTor
}
