/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package plugin is using for HuaWei Ascend pin affinity schedule.
*/
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/config"
)

// InitNPUSession init npu plugin and nodes.
func (sHandle *ScheduleHandler) InitNPUSession(ssn *framework.Session) error {
	if sHandle == nil || ssn == nil {
		klog.V(util.LogDebugLev).Infof("InitNPUSession failed: %s.", util.ArgumentError)
		return errors.New(util.ArgumentError)
	}
	klog.V(util.LogDebugLev).Infof("enter %s InitNPUSession.", PluginName)
	defer klog.V(util.LogDebugLev).Infof("leave %s InitNPUSession.", PluginName)

	sHandle.InitVolcanoFrameFromSsn(ssn)
	sHandle.initCmInformer()
	sHandle.InitNodesFromSsn(ssn)
	sHandle.InitJobsFromSsn(ssn)
	sHandle.initJobScheduleInfoRecorder()

	sHandle.InitTorNodeInfo(ssn)
	sHandle.initJobsPlugin()
	sHandle.initCache()
	sHandle.startFaultHandler(ssn)
	sHandle.preStartPlugin(ssn)
	return nil
}

// InitJobsFromSsn init all jobs in ssn.
func (sHandle *ScheduleHandler) InitJobsFromSsn(ssn *framework.Session) {
	if sHandle == nil || ssn == nil {
		klog.V(util.LogInfoLev).Infof("InitJobsFromSsn failed: %s.", util.ArgumentError)
		return
	}
	newJobs := make(map[api.JobID]SchedulerJob, util.MapInitNum)
	for jobID, jobInfo := range ssn.Jobs {
		// get ownerInfo, deployment job need
		ownerInfo, err := getOwnerInfo(jobInfo, sHandle.FrameAttr)
		if err != nil {
			klog.V(util.LogDebugLev).Infof("%s getOwnerInfo failed: %s.", jobInfo.Name, util.SafePrint(err))
			continue
		}
		sJob := SchedulerJob{
			Owner:             ownerInfo,
			SuperPods:         sHandle.Jobs[jobID].SuperPods,
			JobReadyTag:       util.PtrInit(true),
			UnscheduledReason: newUnscheduledReason(),
		}
		if err := sJob.init(jobInfo, sHandle); err != nil {
			klog.V(util.LogDebugLev).Infof("%s InitJobsFromSsn failed: %s.", jobInfo.Name, util.SafePrint(err))
			continue
		}
		newJobs[jobID] = sJob
	}
	sHandle.Jobs = newJobs
	return
}

// initJobScheduleInfoRecorder update job schedule info recorder.
func (sHandle *ScheduleHandler) initJobScheduleInfoRecorder() {
	tmpRecorder := NewJobScheduleInfoRecorder()
	for jobID, sJob := range sHandle.Jobs {
		// mark the job which server list has been recorded in logs
		if _, ok := sHandle.ServerListRecordFlag[jobID]; ok && sJob.Status == util.PodGroupRunning {
			tmpRecorder.ServerListRecordFlag[jobID] = struct{}{}
		}
		// mark the job which reset configmap has been set
		if _, ok := sHandle.ResetCMSetFlag[jobID]; ok && sJob.SchedulingTaskNum == 0 {
			tmpRecorder.ResetCMSetFlag[jobID] = struct{}{}
		}
		// default value is last session scheduled info that job is in job scheduling or pod scheduling
		tmpRecorder.PodScheduleFlag[jobID] = sHandle.PodScheduleFlag[jobID]
		// if job is need scheduled in this scheduling session, record job is job scheduling or pod scheduling
		// if job is no need scheduled, use last session recorder.
		if sJob.isPodScheduling() {
			tmpRecorder.PodScheduleFlag[jobID] = sJob.SchedulingTaskNum != len(sJob.Tasks)
		}
		// record job last session pending message, for onsessionclose to compare pending message is change
		tmpRecorder.PendingMessage[jobID] = sHandle.PendingMessage[jobID]
	}
	sHandle.JobScheduleInfoRecorder = tmpRecorder

}

func getOwnerInfo(jobInfo *api.JobInfo, vf VolcanoFrame) (OwnerInfo, error) {
	owner := getPodGroupOwnerRef(jobInfo.PodGroup.PodGroup)
	if owner.Kind != ReplicaSetType {
		return OwnerInfo{OwnerReference: owner}, nil
	}
	rs, err := getReplicaSet(vf, jobInfo.Namespace, owner.Name)
	if err != nil {
		return OwnerInfo{}, err
	}
	return OwnerInfo{OwnerReference: owner, Replicas: rs.Spec.Replicas, Annotations: rs.Annotations}, nil
}

func getReplicaSet(vf VolcanoFrame, namespace, name string) (*appsv1.ReplicaSet, error) {
	var rs *appsv1.ReplicaSet
	var ok bool
	key := namespace + "/" + name
	obj, exist, err := vf.informerFactory.Apps().V1().ReplicaSets().Informer().GetIndexer().GetByKey(key)
	if err != nil || !exist {
		klog.V(util.LogWarningLev).Infof("Get rs from indexer failed err: %s, exist: %v.", util.SafePrint(err), exist)
		rs, err = vf.KubeClient.AppsV1().ReplicaSets(namespace).Get(context.TODO(), name,
			metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
	} else {
		rs, ok = obj.(*appsv1.ReplicaSet)
		if !ok {
			return nil, errors.New("the object is not a replicaset")
		}
	}
	return rs, nil
}

func getPodGroupOwnerRef(pg scheduling.PodGroup) metav1.OwnerReference {
	for _, ref := range pg.OwnerReferences {
		if *ref.Controller == true {
			return ref
		}
	}
	return metav1.OwnerReference{}
}

// getJobTemplate get template of all possible segmentation jobs
func (sHandle *ScheduleHandler) getJobTemplate() map[string]map[string]util.VResource {
	jobTemplate := map[string]map[string]util.VResource{
		Ascend310P: {
			VNPUTempVir01:        {Aicore: 1, Aicpu: 1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir02:        {Aicore: util.NPUIndex2, Aicpu: util.NPUIndex2, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir02C1:      {Aicore: util.NPUIndex2, Aicpu: 1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir04:        {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir04C3:      {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir04C3NDVPP: {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledOff},
			VNPUTempVir04C4cDVPP: {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: AscendDVPPEnabledOn},
		},
		Ascend910: {
			VNPUTempVir02: {Aicore: util.NPUIndex2, Aicpu: 1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir04: {Aicore: util.NPUIndex4, Aicpu: 1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir08: {Aicore: util.NPUIndex8, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir16: {Aicore: util.NPUIndex16, Aicpu: util.NPUIndex7, DVPP: AscendDVPPEnabledNull},
		},
		ChipTypeB1: {
			VNPUTempVir06: {Aicore: util.NPUIndex6, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir03: {Aicore: util.NPUIndex3, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir12: {Aicore: util.NPUIndex12, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
		},
		ChipTypeB2C: {
			VNPUTempVir06: {Aicore: util.NPUIndex6, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir03: {Aicore: util.NPUIndex3, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir12: {Aicore: util.NPUIndex12, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
		},
		ChipTypeB2: {
			VNPUTempVir06: {Aicore: util.NPUIndex6, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir03: {Aicore: util.NPUIndex3, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir12: {Aicore: util.NPUIndex12, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
		},
		ChipTypeB3: {
			VNPUTempVir05: {Aicore: util.NPUIndex5, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUTempVir10: {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
		},
		ChipTypeB4: {
			VNPUB4TempVir05:     {Aicore: util.NPUIndex5, Aicpu: util.NPUIndex1, DVPP: AscendDVPPEnabledNull},
			VNPUB4TempVir10C3NM: {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledOff},
			VNPUB4TempVir10C4M:  {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex4, DVPP: AscendDVPPEnabledOn},
			VNPUB4TempVir10:     {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex3, DVPP: AscendDVPPEnabledNull},
		},
	}
	return jobTemplate
}

// InitVolcanoFrameFromSsn init frame parameter from ssn.
func (sHandle *ScheduleHandler) InitVolcanoFrameFromSsn(ssn *framework.Session) {
	if sHandle == nil || ssn == nil {
		klog.V(util.LogErrorLev).Infof("InitVolcanoFrameFromSsn failed: %s.", util.ArgumentError)
		return
	}
	configs := getConfigurationByKey(initConfsFromSsn(ssn.Configurations))
	sHandle.FrameAttr.UID = ssn.UID
	sHandle.FrameAttr.KubeClient = ssn.KubeClient()
	sHandle.FrameAttr.informerFactory = ssn.InformerFactory()
	sHandle.FrameAttr.VJobTemplate = sHandle.getJobTemplate()
	sHandle.initDynamicParameters(configs)
	sHandle.initStaticParameters(configs)
}

// initStaticParameters
func (sHandle *ScheduleHandler) initStaticParameters(configs map[string]string) {
	sHandle.FrameAttr.OnceInit.Do(func() {
		sHandle.FrameAttr.NslbVersion = getNslbVersion(configs)
		sHandle.FrameAttr.SharedTorNum = getShardTorNum(configs)
		sHandle.FrameAttr.UseClusterD = getUseClusterDConfig(configs)
		sHandle.FrameAttr.SelfMaintainAvailCard = getSelfMaintainAvailCard(configs)
		klog.V(util.LogWarningLev).Info("param nslbVersion, sharedTorNum, useClusterInfoManager and self-maintain-mount-card " +
			"init success. can not change them and it will not be changed during normal operation of the volcano")
		klog.V(util.LogWarningLev).Infof("init static parameters, nslbversion is <%v>, SharedTorNum <%v>, UseClusterD"+
			" is <%v>", sHandle.FrameAttr.NslbVersion, sHandle.FrameAttr.SharedTorNum, sHandle.FrameAttr.UseClusterD)
	})
}

// initDynamicParameters
func (sHandle *ScheduleHandler) initDynamicParameters(configs map[string]string) {
	if sHandle == nil || configs == nil {
		klog.V(util.LogInfoLev).Infof("InitCache failed: %s.", util.ArgumentError)
		return
	}
	sHandle.FrameAttr.SuperPodSize = getSizeOfSuperPod(configs)
	sHandle.FrameAttr.ReservePodSize = getReserveNodes(configs, sHandle.FrameAttr.SuperPodSize)
	sHandle.FrameAttr.GraceDeleteTime = getGraceDeleteTime(configs)
	sHandle.FrameAttr.PresetVirtualDevice = getPresetVirtualDeviceConfig(configs)
}

// initConfsFromSsn init confs from session
func initConfsFromSsn(confs []conf.Configuration) []config.Configuration {
	var out []byte
	var err error
	newConfs := make([]config.Configuration, len(confs))
	for idx, cfg := range confs {
		newCfg := &config.Configuration{}
		out, err = yaml.Marshal(cfg)
		if err != nil {
			klog.V(util.LogInfoLev).Infof("Marshal configuration failed: %s.", err)
			continue
		}
		if err = yaml.Unmarshal(out, newCfg); err != nil {
			klog.V(util.LogInfoLev).Infof("Unmarshal configuration failed: %s.", err)
			continue
		}
		newConfs[idx] = *newCfg
	}
	return newConfs
}

// initJobsPlugin init job by plugins.
func (sHandle *ScheduleHandler) initJobsPlugin() {
	for _, vcJob := range sHandle.Jobs {
		if vcJob.policyHandler == nil {
			klog.V(util.LogErrorLev).Infof("initJobsPlugin %s's plugin not register.", vcJob.Name)
			continue
		}
		if err := vcJob.policyHandler.InitMyJobPlugin(vcJob.SchedulerJobAttr, sHandle.ScheduleEnv); err != nil {
			return
		}
	}
}

// initCache init ScheduleHandler's cache.
func (sHandle *ScheduleHandler) initCache() {
	data := make(map[string]map[string]string, util.MapInitNum)
	data[util.RePropertyCacheName] = make(map[string]string, util.MapInitNum)
	data[util.JobRecovery] = make(map[string]string, util.MapInitNum)
	sHandle.OutputCache = ScheduleCache{
		Names:      make(map[string]string, util.MapInitNum),
		Namespaces: make(map[string]string, util.MapInitNum),
		Data:       data}
}

// preStartPlugin preStart plugin action.
func (sHandle *ScheduleHandler) preStartPlugin(ssn *framework.Session) {
	for _, job := range sHandle.Jobs {
		if err := job.policyHandler.PreStartAction(ssn); err != nil {
			if strings.Contains(err.Error(), util.ArgumentError) {
				continue
			}
			klog.V(util.LogErrorLev).Infof("PreStartPlugin %s %s.", job.Name, err)
		}
	}
}

func (sHandle *ScheduleHandler) saveCacheToCm() {
	for spName, cmName := range sHandle.ScheduleEnv.OutputCache.Names {
		nameSpace, okSp := sHandle.ScheduleEnv.OutputCache.Namespaces[spName]
		data, okData := sHandle.ScheduleEnv.OutputCache.Data[spName]
		if !okSp || !okData {
			klog.V(util.LogErrorLev).Infof("SaveCacheToCm %s no namespace or Data in cache.", spName)
			continue
		}

		data, err := k8s.UpdateConfigmapIncrementally(sHandle.FrameAttr.KubeClient, nameSpace, cmName, data)
		if err != nil {
			klog.V(util.LogInfoLev).Infof("get old %s configmap failed: %v, write new data into cm", spName, err)
		}
		var tmpCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: nameSpace,
			},
			Data: data,
		}
		if err := k8s.CreateOrUpdateConfigMap(sHandle.FrameAttr.KubeClient, tmpCM, cmName, nameSpace); err != nil {
			klog.V(util.LogErrorLev).Infof("CreateOrUpdateConfigMap : %s.", util.SafePrint(err))
		}
	}
}

// BeforeCloseHandler do the action before ssn close.
func (sHandle *ScheduleHandler) BeforeCloseHandler() {
	if sHandle == nil {
		klog.V(util.LogInfoLev).Infof("BeforeCloseHandler failed: %s.", util.ArgumentError)
		return
	}
	for _, job := range sHandle.Jobs {
		if job.SchedulingTaskNum == 0 {
			job.recordTorJobServerList(sHandle)
			job.updateResetConfigMap(sHandle)
		}
	}
	if sHandle.FaultHandle != nil {
		if err := sHandle.FaultHandle.PreStopAction(&sHandle.ScheduleEnv); err != nil {
			klog.V(util.LogErrorLev).Infof("PreStopPlugin  %s.", util.SafePrint(err))
		}
	}

	sHandle.saveCacheToCm()
	if sHandle.Tors == nil || sHandle.Tors.GetNSLBVersion() == defaultNSLBVersion {
		return
	}
	err := sHandle.cacheToShareCM()
	if err != nil {
		klog.V(util.LogErrorLev).Infof("cacheToShareCM error: %v", err)
	}
}

// initCmInformer init cm informer, support cluster info manager and device plugin
func (sHandle *ScheduleHandler) initCmInformer() {
	if sHandle.FrameAttr.KubeClient == nil {
		klog.V(util.LogErrorLev).Info("kube client in session is nil")
		return
	}
	k8s.InitCmInformer(sHandle.FrameAttr.KubeClient, sHandle.FrameAttr.UseClusterD)
}

// startFaultHandler initialize re-scheduler
func (sHandle *ScheduleHandler) startFaultHandler(ssn *framework.Session) {
	if sHandle.FaultHandle == nil {
		return
	}
	if preErr := sHandle.FaultHandle.Execute(&sHandle.ScheduleEnv, ssn); preErr != nil {
		klog.V(util.LogWarningLev).Infof("PreStartAction failed by %s", preErr)
		return
	}
}

// cacheToShareCM cache tors info to configmap
func (sHandle *ScheduleHandler) cacheToShareCM() error {
	data := make(map[string]string, 1)
	toShareMap := sHandleTorsToTorShareMap(sHandle)
	dataByte, err := json.Marshal(toShareMap)
	if err != nil {
		return fmt.Errorf("marshal tor configmap data error %v", err)
	}
	data[GlobalTorInfoKey] = string(dataByte[:])
	putCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: TorShareCMName,
		Namespace: cmNameSpace}, Data: data}
	if err := k8s.CreateOrUpdateConfigMap(sHandle.FrameAttr.KubeClient, putCM, TorShareCMName,
		cmNameSpace); err != nil {
		klog.V(util.LogInfoLev).Infof("cacheToShareCM CreateOrUpdateConfigMap error: %s", util.SafePrint(err))
	}
	return nil
}

func sHandleTorsToTorShareMap(sHandle *ScheduleHandler) map[string]TorShare {
	torShareMap := make(map[string]TorShare)
	if sHandle.Tors == nil || sHandle.Tors.Tors == nil {
		return torShareMap
	}
	var nodeJobs []NodeJobInfo
	var jobList []string
	var nodeJob NodeJobInfo
	for _, tor := range sHandle.Tors.Tors {
		nodeJobs = []NodeJobInfo{}
		for _, server := range tor.Servers {
			jobList = []string{}
			for jobName := range server.Jobs {
				jobList = append(jobList, jobName)
			}
			nodeJob = NodeJobInfo{
				NodeIp:   server.IP,
				NodeName: server.Name,
				JobName:  jobList,
			}
			nodeJobs = append(nodeJobs, nodeJob)
		}
		torShareMap[tor.IP] = TorShare{
			IsHealthy:   tor.IsHealthy,
			IsSharedTor: tor.IsSharedTor,
			NodeJobs:    nodeJobs,
		}
	}
	return torShareMap
}

func isContain(target string, strArray []string) bool {
	for _, each := range strArray {
		if each == target {
			return true
		}
	}
	return false
}

// BatchNodeOrderFn Score the selected nodes.
func (sHandle *ScheduleHandler) BatchNodeOrderFn(task *api.TaskInfo,
	nodes []*api.NodeInfo) (map[string]float64, error) {
	if sHandle == nil || task == nil || len(nodes) == 0 {
		klog.V(util.LogDebugLev).Infof("BatchNodeOrderFn failed: %s.", util.ArgumentError)
		return nil, errors.New(util.ArgumentError)
	}
	klog.V(util.LogDebugLev).Infof("Enter batchNodeOrderFn")
	defer klog.V(util.LogDebugLev).Infof("leaving batchNodeOrderFn")

	if !isNPUTask(task) {
		return nil, nil
	}
	if len(sHandle.Nodes) == 0 {
		klog.V(util.LogDebugLev).Infof("%s batchNodeOrderFn %s.", PluginName, util.ArgumentError)
		return nil, nil
	}
	// init score-map
	scoreMap := initScoreMap(nodes)
	vcJob, ok := sHandle.Jobs[task.Job]
	if !ok {
		klog.V(util.LogDebugLev).Infof("BatchNodeOrderFn %s not req npu.", task.Name)
		return scoreMap, nil
	}

	// 2.Get the best node and top by A,B,C,D rules and require numbers.
	errGet := vcJob.policyHandler.ScoreBestNPUNodes(task, nodes, scoreMap)
	if sHandle.FaultHandle != nil {
		sHandle.FaultHandle.ScoreBestNPUNodes(task, scoreMap)
	}
	for nodeName := range scoreMap {
		scoreMap[nodeName] *= scoreWeight
	}
	if errGet != nil {
		// get suitable node failed
		klog.V(util.LogErrorLev).Infof("batchNodeOrderFn task[%s] failed by err:[%s].", task.Name, util.SafePrint(errGet))
		return scoreMap, errGet
	}
	klog.V(util.LogInfoLev).Infof("batchNodeOrderFn Get task:%s for NPU %+v.", task.Name, scoreMap)

	return scoreMap, nil
}

// getConfigurationByKey called by GetConfigFromSchedulerConfigMap
func getConfigurationByKey(configurations []config.Configuration) map[string]string {
	for _, cf := range configurations {
		if cf.Name == util.CMInitParamKey {
			return cf.Arguments
		}
	}
	return map[string]string{}
}

// getSizeOfSuperPod get size of super pod
func getSizeOfSuperPod(configurations map[string]string) int {
	superPodSize := getSuperPodInfoFromConfig(sizeOfSuperPodKey, configurations)
	if superPodSize == 0 {
		klog.V(util.LogWarningLev).Infof(" super-pod-size configuration should be a number bigger than 0, "+
			"set default super-pod-size: %d", defaultSuperPodSize)
		superPodSize = defaultSuperPodSize
	}
	return superPodSize
}

// getReserveNodes get reserve nodes
func getReserveNodes(configurations map[string]string, superPodSize int) int {
	reserve := getSuperPodInfoFromConfig(reserveNodesKey, configurations)
	if reserve == 0 {
		reserve = defaultReserveNodes
	}
	if reserve >= superPodSize {
		validRes := 0
		if superPodSize > defaultReserveNodes {
			validRes = defaultReserveNodes
		}
		klog.V(util.LogWarningLev).Infof("reserve-nodes(%d) is larger than super-pod-size(%d), set reserve-nodes: %d",
			reserve, superPodSize, validRes)
		reserve = validRes
	}
	return reserve
}

func getSuperPodInfoFromConfig(key string, configurations map[string]string) int {
	if len(configurations) == 0 {
		klog.V(util.LogWarningLev).Info("volcano scheduler config init-params map is nil")
		return 0
	}
	value, ok := configurations[key]
	if !ok {
		klog.V(util.LogWarningLev).Infof("%s configuration not exist", key)
		return 0
	}

	res, err := strconv.Atoi(value)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("cannot convert %s configuration, err: %v", key, err)
		return 0
	}
	if res < 0 {
		klog.V(util.LogWarningLev).Infof(" %s configuration should not be negative number", key)
		return 0
	}
	return res
}

// checkGraceDeleteTimeValid used by GetGraceDeleteTime for validity checking
func checkGraceDeleteTimeValid(overTime int64) bool {
	if overTime < minGraceOverTime || overTime > maxGraceOverTime {
		klog.V(util.LogErrorLev).Infof("GraceOverTime value should be range [2, 3600], configured is [%d], "+
			"GraceOverTime will not be changed", overTime)
		return false
	}
	// use user's configuration to set grace over time
	klog.V(util.LogInfoLev).Infof("set GraceOverTime to new value [%d].", overTime)
	return true
}

// getGraceDeleteTime get grace delete time
func getGraceDeleteTime(conf map[string]string) int64 {
	klog.V(util.LogInfoLev).Info("enter GetGraceDeleteTime ...")
	defer klog.V(util.LogInfoLev).Info("leave GetGraceDeleteTime ...")
	if len(conf) == 0 {
		klog.V(util.LogErrorLev).Infof("GetGraceDeleteTime failed: %s, no conf", util.ArgumentError)
		return DefaultGraceOverTime
	}
	// get grace over time by user configuration
	overTimeStr, ok := conf[GraceOverTimeKey]
	if !ok {
		klog.V(util.LogErrorLev).Info("set GraceOverTime failed and will not be changed, " +
			"key grace-over-time doesn't exists.")
		return DefaultGraceOverTime
	}
	overTime, err := strconv.ParseInt(overTimeStr, util.Base10, util.BitSize64)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("set GraceOverTime failed and will not be changed, "+
			"grace-over-time is invalid [%s].", util.SafePrint(overTimeStr))
		return DefaultGraceOverTime
	}
	// check time validity
	if !checkGraceDeleteTimeValid(overTime) {
		return DefaultGraceOverTime
	}
	return overTime
}

// getUseClusterDConfig check use cluster info manager by config, default true
func getUseClusterDConfig(conf map[string]string) bool {
	useClusterInfoManager, ok := conf[util.UseClusterInfoManager]
	if !ok {
		klog.V(util.LogDebugLev).Info("CheckUseCIMByConfig doesn't exist useClusterInfoManager.")
		return true
	}
	return useClusterInfoManager == "true"
}

// getSelfMaintainAvailCard check volcano self maintain available card by config, default true
func getSelfMaintainAvailCard(conf map[string]string) bool {
	selfMaintainAvailCard, ok := conf[util.SelfMaintainAvailCard]
	if !ok {
		klog.V(util.LogDebugLev).Info("CheckUseCIMByConfig doesn't exist self-maintain-available-card.")
		return true
	}
	return selfMaintainAvailCard == "true"
}

// getPresetVirtualDeviceConfig get VNPU segmentEnable by init plugin parameters, return true if static
func getPresetVirtualDeviceConfig(conf map[string]string) bool {
	// get segmentEnable by user configuration
	segmentEnable, ok := conf[util.SegmentEnable]
	if !ok {
		klog.V(util.LogDebugLev).Info("checkVNPUSegmentEnable doesn't exist presetVirtualDevice.")
		return false
	}
	return segmentEnable == "true"
}

// getShardTorNum get shared tor num from configmap
func getShardTorNum(conf map[string]string) int {
	str := conf[keyOfSharedTorNum]
	sharedTorNum, err := strconv.Atoi(str)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("getSharedTorNum %s.", err)
		return shareTorNum2
	}
	if sharedTorNum != shareTorNum1 && sharedTorNum != shareTorNum2 {
		klog.V(util.LogWarningLev).Info("sharedTorNum is illegal. use default config")
		return shareTorNum2
	}
	return sharedTorNum
}

// getNslbVersion get nslb version from config
func getNslbVersion(conf map[string]string) string {
	nslbVersion := conf[keyOfNSLBVersion]
	if nslbVersion != defaultNSLBVersion && nslbVersion != NSLB2Version {
		klog.V(util.LogWarningLev).Info("nslbVersion is illegal. use default config")
		return defaultNSLBVersion
	}
	return nslbVersion
}
