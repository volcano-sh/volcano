/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package rescheduling

import (
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

func init() {
	reSchedulerCache = newReSchedulerCache()
}

func newReSchedulerCache() *DealReSchedulerCache {
	return &DealReSchedulerCache{
		FaultNodes: map[string]*FaultNode{},
		FaultJobs:  map[api.JobID]*FaultJob{},
	}
}

// GetReSchedulerCache return reschedule cache
func GetReSchedulerCache() *DealReSchedulerCache {
	return reSchedulerCache
}

func (reCache *DealReSchedulerCache) setFaultNodes(faultNodes map[string]*FaultNode) {
	reCache.FaultNodes = faultNodes
}

func (reCache *DealReSchedulerCache) setFaultJobs(faultJobs map[api.JobID]*FaultJob) {
	reCache.FaultJobs = faultJobs
}

func (reCache DealReSchedulerCache) getRecentReschedulingRecordsFromCm(buffer string) (
	map[api.JobID]*RescheduleReason, error) {
	rescheduleRecords := make(map[api.JobID]*RescheduleReason)
	if unmarshalErr := json.Unmarshal([]byte(buffer), &rescheduleRecords); unmarshalErr != nil {
		klog.V(util.LogDebugLev).Info("Unmarshal reschedule records from cache failed")
		return nil, fmt.Errorf("reschedule records convert from CM error: %s", util.SafePrint(unmarshalErr))
	}
	return rescheduleRecords, nil
}

// SetJobRecentRescheduleRecords get already recorded rescheduling records from cm, and cache it
func (reCache *DealReSchedulerCache) SetJobRecentRescheduleRecords(firstStartup *bool,
	client kubernetes.Interface) error {
	if reCache == nil {
		klog.V(util.LogErrorLev).Infof("SetJobRecentRescheduleRecords failed: %s", util.ArgumentError)
		return errors.New(util.ArgumentError)
	}
	if firstStartup != nil && *firstStartup {
		cm, err := k8s.GetConfigMap(client, RescheduleReasonCmNamespace, RescheduleReasonCmName)
		if err != nil {
			return fmt.Errorf("failed to get reschedule reason configmap, err: %s", err.Error())
		}
		recordedRecords, err := reCache.getRecentReschedulingRecordsFromCm(cm.Data[CmJobRescheduleReasonsKey])
		if err != nil {
			return fmt.Errorf("getRecentReschedulingRecordsFromCm %s", util.SafePrint(err))
		}
		reCache.JobRecentRescheduleRecords = recordedRecords
		klog.V(util.LogDebugLev).Infof("start sync old rescheduling records %#v", recordedRecords)
		return nil
	}
	return nil
}

func (reCache *DealReSchedulerCache) marshalCacheDataToString(data interface{}) (string, error) {
	dataBuffer, err := json.Marshal(data)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("marshalCacheDataToString err: %s", util.SafePrint(err))
		return "", err
	}
	return string(dataBuffer), nil
}

// getRealFaultJobs only return FaultJobs whose IsFaultJob is true
func (reCache DealReSchedulerCache) getRealFaultJobs() map[api.JobID]*FaultJob {
	realFaultJobs := make(map[api.JobID]*FaultJob)
	for _, fJob := range reCache.FaultJobs {
		if (!fJob.IsFaultJob && !fJob.IsNormalJobNeedRestart()) || fJob.ReScheduleKey == JobOffRescheduleLabelValue {
			continue // only save real-fault and reschedule-enabled jobs
		}

		faultReason := PodFailed
		for _, faultType := range fJob.FaultTypes {
			if faultType == NodeUnhealthy || faultType == NodeCardUnhealthy || faultType == SubHealthFault ||
				faultType == util.RelationFault {
				faultReason = faultType
				break
			}
		}
		fJob.faultReason = faultReason
		realFaultJobs[fJob.JobUID] = fJob
	}
	return realFaultJobs
}

// GetRealFaultNodes get the nodes whose isFaultNode property takes true value
func (reCache DealReSchedulerCache) GetRealFaultNodes() map[string]*FaultNode {
	realFaultNodes := make(map[string]*FaultNode)
	for _, fNode := range reCache.FaultNodes {
		if !fNode.IsFaultNode {
			continue
		}
		realFaultNodes[fNode.NodeName] = fNode
	}
	return realFaultNodes
}

func (reCache *DealReSchedulerCache) writeFaultNodesToCMString() (string, error) {
	realFaultNode := reCache.GetRealFaultNodes()
	if len(realFaultNode) == 0 {
		return "", nil
	}
	reCache.FaultNodes = realFaultNode
	nodeDataToCm, marshalErr := reCache.marshalCacheDataToString(getFaultNodeToCm(realFaultNode))
	if marshalErr != nil {
		return "", fmt.Errorf("writeFaultNodesToCM: nodeDataToCm failed %s", util.SafePrint(marshalErr))
	}
	return nodeDataToCm, nil
}

func getFaultNodeToCm(realFaultNode map[string]*FaultNode) []FaultNodeInfoToCm {
	faultNodeToCm := make([]FaultNodeInfoToCm, 0, len(realFaultNode))
	for _, fNode := range realFaultNode {
		faultNodeToCm = append(faultNodeToCm, initFaultNodeToCmByFaultNode(fNode))
	}
	return faultNodeToCm
}

func initFaultNodeToCmByFaultNode(fNode *FaultNode) FaultNodeInfoToCm {
	return FaultNodeInfoToCm{
		FaultDeviceList:     fNode.FaultDeviceList,
		NodeName:            fNode.NodeName,
		UnhealthyNPU:        fNode.UnhealthyNPU,
		NetworkUnhealthyNPU: fNode.NetworkUnhealthyNPU,
		NodeDEnable:         fNode.NodeDEnable,
		NodeHealthState:     fNode.NodeHealthState,
		UpdateTime:          fNode.UpdateTime,
	}
}

func (reCache *DealReSchedulerCache) getRemainTimesToCMString() (string, error) {
	if len(reCache.JobRemainRetryTimes) == 0 {
		return "", nil
	}
	nodeHBsData, err := reCache.marshalCacheDataToString(reCache.JobRemainRetryTimes)
	if err != nil {
		return "", fmt.Errorf("getRemainTimesToCMString: %s", util.SafePrint(err))
	}
	return nodeHBsData, nil
}

func (reCache *DealReSchedulerCache) writeRescheduleReasonsToCMString() (string, error) {
	if len(reCache.JobRecentRescheduleRecords) == 0 {
		return "", nil
	}
	rescheduleReasonStr, err := reCache.marshalCacheDataToString(reCache.JobRecentRescheduleRecords)
	if err != nil {
		return "", fmt.Errorf("writeRescheduleReasonsToCMString: %s", util.SafePrint(err))
	}
	if len(rescheduleReasonStr) > MaxKbOfRescheduleRecords {
		klog.V(util.LogWarningLev).Infof("reason of reschedule into configmap is more than %d Kb, "+
			"will reduce it", MaxKbOfRescheduleRecords)
	}
	// only keep every job newest server record, each time will cut the oldest record of each job
	// to make sure the returned reschedule reason str len is under MaxKbOfRescheduleRecords Kb,
	// each time will reduce the length by 1/10
	// by the way, to avoid dead loop, there is a loop limit
	for i := 0; len(rescheduleReasonStr) > MaxKbOfRescheduleRecords && i < MaxRescheduleRecordsNum; i++ {
		for jobId, reason := range reCache.JobRecentRescheduleRecords {
			// must keep the newest rescheduling record
			if len(reason.RescheduleRecords) <= 1 {
				continue
			}
			lastRecord := reason.RescheduleRecords[len(reason.RescheduleRecords)-1]
			reason.RescheduleRecords = reason.RescheduleRecords[:len(reason.RescheduleRecords)-1]
			klog.V(util.LogWarningLev).Infof("cut job %v reschedule reason of timestamp %d from cm, "+
				"to reduce records length", jobId, lastRecord.RescheduleTimeStamp)
		}
		// to avoid frequently marshal a 950 Kb json, time-consuming
		rescheduleReasonStr, err = reCache.marshalCacheDataToString(reCache.JobRecentRescheduleRecords)
		if err != nil {
			return "", fmt.Errorf("writeRescheduleReasonsToCMString: %s", util.SafePrint(err))
		}
		if len(rescheduleReasonStr) <= MaxKbOfRescheduleRecords {
			break
		}
	}
	return rescheduleReasonStr, nil
}

// WriteReSchedulerCacheToEnvCache write the modifications on cache data to env to update re-scheduling configmap
func (reCache *DealReSchedulerCache) WriteReSchedulerCacheToEnvCache(env *plugin.ScheduleEnv, jobType string) error {
	if reCache == nil || env == nil {
		return errors.New(util.ArgumentError)
	}
	env.OutputCache.Names[RePropertyName] = CmName
	env.OutputCache.Namespaces[RePropertyName] = CmNameSpace
	fNodeToCMString, err := reCache.writeFaultNodesToCMString()
	if err != nil {
		klog.V(util.LogDebugLev).Infof("WriteReSchedulerCacheToEnvCache: %s", util.SafePrint(err))
	}
	// retain real fault jobs in cache
	reCache.FaultJobs = getRealFaultJobForCache(reCache.getRealFaultJobs())
	jobRemainRetryTimes, err := reCache.getRemainTimesToCMString()
	if err != nil {
		klog.V(util.LogDebugLev).Infof("getRemainTimesToCMString: %s", util.SafePrint(err))
	}
	// update the reschedule reason cache
	if err := reCache.setRescheduleReasonToCache(env); err != nil {
		klog.V(util.LogDebugLev).Infof("setRescheduleReasonToCache: %s", util.SafePrint(err))
	}
	cmData, ok := env.OutputCache.Data[RePropertyName]
	if !ok {
		cmData = make(map[string]string, util.MapInitNum)
		env.OutputCache.Data[RePropertyName] = cmData
	}
	cmData[CmJobRemainRetryTimes] = jobRemainRetryTimes
	cmData[CmFaultNodeKind] = fNodeToCMString
	return nil
}

func (reCache *DealReSchedulerCache) setRescheduleReasonToCache(env *plugin.ScheduleEnv) error {
	env.OutputCache.Names[ReschedulingReasonKey] = RescheduleReasonCmName
	env.OutputCache.Namespaces[ReschedulingReasonKey] = RescheduleReasonCmNamespace
	jobRescheduleReasons, err := reCache.writeRescheduleReasonsToCMString()
	if err != nil {
		klog.V(util.LogDebugLev).Infof("writeRescheduleReasonsToCMString: %s", util.SafePrint(err))
		return fmt.Errorf("writeRescheduleReasonsToCMString: %s", util.SafePrint(err))
	}
	reasonCmData, ok := env.OutputCache.Data[ReschedulingReasonKey]
	if !ok {
		reasonCmData = make(map[string]string, util.MapInitNum)
		env.OutputCache.Data[ReschedulingReasonKey] = reasonCmData
	}
	reasonCmData[CmJobRescheduleReasonsKey] = jobRescheduleReasons
	return nil
}

// judgePublicFaultInReason return fTask has public fault
func judgePublicFaultInReason(fTask *miniFaultTask) bool {
	if fTask == nil {
		return false
	}
	for _, reason := range fTask.Reason {
		if reason.FaultType == PublicFaultType {
			return true
		}
	}
	return false
}
