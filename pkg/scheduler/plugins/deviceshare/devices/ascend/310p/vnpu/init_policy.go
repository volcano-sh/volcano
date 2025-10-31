/*
Copyright 2025 The Volcano Authors.

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

package vnpu310p

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/ascend310p/vnpu"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/third_party/ascend-for-volcano/common/k8s"
	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
	"volcano.sh/volcano/third_party/ascend-for-volcano/config"
	"volcano.sh/volcano/third_party/ascend-for-volcano/plugin"
)

func InitVNPUDevice(device *vnpu.NPUDevices, ssn *framework.Session, nodeInfo *api.NodeInfo) error {
	if ssn == nil {
		klog.V(util.LogDebugLev).Infof("InitVNPUDevice failed: %s.", util.ArgumentError)
		return errors.New(util.ArgumentError)
	}

	klog.V(util.LogDebugLev).Infof("enter %s InitVNPUDevice.", "DeviceShare")
	defer klog.V(util.LogDebugLev).Infof("leave %s InitNPUSession.", "DeviceShare")

	// use information in ssn and nodeInfo to initialize device struct, and exclude api package
	initVolcanoFrameFromSsn(device, ssn)

	initCmInformer(device)

	initNodeFromSsn(device, nodeInfo)

	return nil
}

// initCmInformer init cm informer, support cluster info manager and device plugin
func initCmInformer(device *vnpu.NPUDevices) {
	if device.FrameAttr.KubeClient == nil {
		klog.V(util.LogErrorLev).Info("kube client in session is nil")
		return
	}
	k8s.InitCmInformer(device.FrameAttr.KubeClient, device.FrameAttr.UseClusterD)
}

func initVolcanoFrameFromSsn(device *vnpu.NPUDevices, ssn *framework.Session) {
	if ssn == nil {
		klog.V(util.LogErrorLev).Infof("InitVolcanoFrameFromSsn failed: %s.", util.ArgumentError)
		return
	}

	configs := getConfigurationByKey(initConfsFromSsn(ssn.Configurations))
	device.FrameAttr.UID = ssn.UID
	device.FrameAttr.KubeClient = ssn.KubeClient()
	device.FrameAttr.InformerFactory = ssn.InformerFactory()
	device.FrameAttr.VJobTemplate = map[string]map[string]vnpu.VResource{
		util.Ascend310P: {
			plugin.VNPUTempVir01:        {Aicore: 1, Aicpu: 1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir02:        {Aicore: util.NPUIndex2, Aicpu: util.NPUIndex2, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir02C1:      {Aicore: util.NPUIndex2, Aicpu: 1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir04:        {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir04C3:      {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir04C3NDVPP: {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledOff},
			plugin.VNPUTempVir04C4cDVPP: {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledOn},
		},
		util.Ascend910: {
			plugin.VNPUTempVir02: {Aicore: util.NPUIndex2, Aicpu: 1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir04: {Aicore: util.NPUIndex4, Aicpu: 1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir08: {Aicore: util.NPUIndex8, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir16: {Aicore: util.NPUIndex16, Aicpu: util.NPUIndex7, DVPP: plugin.AscendDVPPEnabledNull},
		},
		plugin.ChipTypeB1: {
			plugin.VNPUTempVir06: {Aicore: util.NPUIndex6, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir03: {Aicore: util.NPUIndex3, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir12: {Aicore: util.NPUIndex12, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
		},
		plugin.ChipTypeB2C: {
			plugin.VNPUTempVir06: {Aicore: util.NPUIndex6, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir03: {Aicore: util.NPUIndex3, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir12: {Aicore: util.NPUIndex12, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
		},
		plugin.ChipTypeB2: {
			plugin.VNPUTempVir06: {Aicore: util.NPUIndex6, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir03: {Aicore: util.NPUIndex3, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir12: {Aicore: util.NPUIndex12, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
		},
		plugin.ChipTypeB3: {
			plugin.VNPUTempVir05: {Aicore: util.NPUIndex5, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUTempVir10: {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
		},
		plugin.ChipTypeB4: {
			plugin.VNPUB4TempVir05:     {Aicore: util.NPUIndex5, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			plugin.VNPUB4TempVir10C3NM: {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledOff},
			plugin.VNPUB4TempVir10C4M:  {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledOn},
			plugin.VNPUB4TempVir10:     {Aicore: util.NPUIndex10, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
		},
	}
	initDynamicParameters(device, configs)
	initStaticParameters(device, configs)
}

// initStaticParameters
func initStaticParameters(device *vnpu.NPUDevices, configs map[string]string) {
	device.FrameAttr.OnceInit.Do(func() {
		device.FrameAttr.UseClusterD = getUseClusterDConfig(configs)
		device.FrameAttr.SelfMaintainAvailCard = getSelfMaintainAvailCard(configs)
		klog.V(util.LogWarningLev).Infof("init static parameters, UseClusterD"+
			" is <%v>", device.FrameAttr.UseClusterD)
	})
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

// initDynamicParameters
func initDynamicParameters(device *vnpu.NPUDevices, configs map[string]string) {
	if device == nil || configs == nil {
		klog.V(util.LogInfoLev).Infof("InitCache failed: %s.", util.ArgumentError)
		return
	}
	device.FrameAttr.PresetVirtualDevice = getPresetVirtualDeviceConfig(configs)
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

// getConfigurationByKey called by GetConfigFromSchedulerConfigMap
func getConfigurationByKey(configurations []config.Configuration) map[string]string {
	for _, cf := range configurations {
		if cf.Name == util.CMInitParamKey {
			return cf.Arguments
		}
	}
	return map[string]string{}
}

func initNodeFromSsn(device *vnpu.NPUDevices, nodeInfo *api.NodeInfo) {
	klog.V(util.LogDebugLev).Infof("Entering initNodeFron Ssn function")

	// 1.obtain device infos, and if node not in session, its device info should not keep in cache
	deviceInfos := k8s.GetDeviceInfoAndSetInformerStart(nodeInfo, device.FrameAttr.UseClusterD,
		device.FrameAttr.SelfMaintainAvailCard)
	// 2. obtain node infos of noded configmap
	nodeInfosOfNodeD := k8s.GetNodeDInfo(nodeInfo)
	// 3. obtain switch infos of switch configmap
	//switchInfos := k8s.GetSwitchInfos(nodeInfo)

	if err := initNPUNodeByNodeInf(device, nodeInfo, deviceInfos, nodeInfosOfNodeD, device.FrameAttr.VJobTemplate); err != nil &&
		!strings.Contains(err.Error(), vnpu.NoneResourceErr) {
		klog.V(util.LogErrorLev).Infof("InitNodeFromSsn %s %s, not put in nodes.", nodeInfo.Name, err)
	}
}

// initNPUNodeByNodeInf init NPU node from node info and cm.
func initNPUNodeByNodeInf(device *vnpu.NPUDevices, npuNode *api.NodeInfo, deviceInfo k8s.NodeDeviceInfoWithID,
	nodeInfoOfNodeD k8s.NodeDNodeInfo, vJobTemplate map[string]map[string]vnpu.VResource) error {
	klog.V(util.LogDebugLev).Infof("Entering initNPUNodeByNodeInf function")

	if device == nil || npuNode == nil {
		klog.V(util.LogInfoLev).Infof("InitNPUNodeByNodeInf failed: %s.", util.ArgumentError)
		return errors.New(util.ArgumentError)
	}
	capability := getNPUNodeCapacity(npuNode)
	if !util.IsMapHasNPUResource(capability, util.HwPreName) {
		return fmt.Errorf("node %s npu resource is not enable", npuNode.Name)
	}
	if deviceInfo.DeviceList == nil {
		return fmt.Errorf("node %s device info or clusterd info is not enable", npuNode.Name)
	}
	device.NodeInf.Name = npuNode.Name
	device.Capability = capability
	device.BaseDeviceInfo = npuNode.Node.Annotations[util.BaseDeviceInfoKey]
	device.NodeInf.Allocate = npuNode.Allocatable.ScalarResources
	device.Idle = npuNode.Idle.ScalarResources
	device.Label = npuNode.Node.Labels
	device.Address = getNPUNodeAddress(npuNode)
	//device.Tasks = npuNode.Tasks
	syncAnnotation(device, npuNode, nodeInfoOfNodeD)
	updateNPUNodeDeviceInfos(device, deviceInfo)

	if setVNPUErr := setNodeVNPUInfo(device, npuNode, vJobTemplate); setVNPUErr != nil {
		klog.V(util.LogDebugLev).Infof("setNodeVNPUInfo %s %s", npuNode.Name, setVNPUErr)
	}
	klog.V(util.LogDebugLev).Infof("initNPUNodeByNodeInf <%s> success %#v", npuNode.Name, device.NodeInf)
	return nil
}

func setNodeVNPUInfo(device *vnpu.NPUDevices, ni *api.NodeInfo, jobTemplate map[string]map[string]vnpu.VResource) error {
	device.NPUDevice = vnpu.NPUDevice{
		Chips:            make(map[int]*vnpu.VChip, util.MapInitNum),
		UnhealthyChipIds: make(map[int]struct{}),
	}

	if !checkDyVNodeResourceInitialized(device) {
		return fmt.Errorf("setNodeVNPUInfo %s: DyVNode resource not initialized", device.NodeInf.Name)
	}

	// 1. get chipKind like Ascend910, chipLabel like Ascend310P-8
	if err := setChipPropertiesFromNPUNode(device); err != nil {
		return fmt.Errorf("setNodeVNPUInfo %s: %v", device.NodeInf.Name, err)
	}

	// 2. get resource capacity, totalChipNum, freeChipNum
	if err := setTotalResAndChipNumByTemplates(device); err != nil {
		return fmt.Errorf("setNodeVNPUInfo node %s: %v", device.NodeInf.Name, err)
	}

	// 3. create vChips on node and update vChip resource
	if err := initVChips(device, ni, jobTemplate); err != nil {
		return fmt.Errorf("setNodeVNPUInfo node %s: %v", device.NodeInf.Name, err)
	}

	device.ValidVNode = true
	klog.V(util.LogDebugLev).Infof("setNodeVNPUInfo %s initialisation success:<%#v>", device.NodeInf.Name, device.NPUDevice)
	return nil
}

func initVChips(device *vnpu.NPUDevices, ni *api.NodeInfo, taskTemplate map[string]map[string]vnpu.VResource) error {
	chipTotalRes := getVChipTotalRes(device)
	if err := createNodeNewVChips(device, chipTotalRes); err != nil {
		klog.V(util.LogDebugLev).Infof("vNode %s %s.", device.NodeInf.Name, util.SafePrint(err))
	} // 3. create new VChip by freeCardID whole card

	if err := setUnhealthyChipIds(device); err != nil {
		klog.V(util.LogDebugLev).Infof("vNode %s %s.", device.NodeInf.Name, err)
	}
	for _, ti := range ni.Tasks {
		if !isNPUTask(ti) {
			continue
		}
		addNPUResource(device, ti.Pod, chipTotalRes, taskTemplate)
	} // 4. update VChips and create VChips for chips being occupied

	return nil
}

// addNPUResource update all pod resource to node
func addNPUResource(device *vnpu.NPUDevices, pod *v1.Pod, chipTotalRes vnpu.VResource,
	taskTemplate map[string]map[string]vnpu.VResource) {
	coreNameStr, ok := pod.Annotations[util.AscendNPUCore]
	if !ok {
		klog.V(util.LogDebugLev).Infof("addNPUResource pod %s %s no value", pod.Name, util.AscendNPUCore)
		return
	}

	if isPodWholeCardFromAscendCore(coreNameStr) {
		addNPUResourceWholeCard(device, pod)
		return
	}
	addNPUResourceVNPUCard(device, pod, chipTotalRes, taskTemplate)
}

// addNPUResourceVNPUCard ascendStr Ascend310P-4c.3cpu.ndvpp-100(VNPUID)-1(physic ID)_1（vgroupID）
func addNPUResourceVNPUCard(device *vnpu.NPUDevices, pod *v1.Pod, chipTotalRes vnpu.VResource,
	taskTemplate map[string]map[string]vnpu.VResource) {
	// 1. get physics id
	physicsID, err := getCardPhysicsIDFromAscendCore(pod, false)
	if err != nil || len(physicsID) != util.NPUIndex1 {
		klog.V(util.LogErrorLev).Infof("addNPUResourceVNPUCard get pod<%s> card physics id failed", pod.Name)
		return
	}
	_, isCardunhealthy := device.UnhealthyChipIds[physicsID[0]]
	if isCardunhealthy {
		klog.V(util.LogErrorLev).Infof("addNPUResourceVNPUCard get pod<%s> card is unhealthy", pod.Name)
		return
	}
	// 2. add chip to node
	curVChip, ok := device.Chips[physicsID[0]]
	if !ok {
		curVChip = NewVChip(device, physicsID[0], chipTotalRes)
		device.Chips[physicsID[0]] = curVChip
	}
	curVChip.Unstable = curVChip.IsPodResUnstable(pod) || curVChip.Unstable
	curVChip.AddRealCardID(pod.Annotations[util.AscendNPUPodRealUse])
	curVChip.AddPodToPodMap(pod)
	curVChip.SetSegmentFlag(true)

	// 3. get resource of pod
	podVResource := getPodUsedRes(device, pod, taskTemplate)
	if podVResource == nil {
		klog.V(util.LogErrorLev).Infof("addNPUResource resolving pod<%s> resource failed", pod.Name)
		return
	}
	// 4. update node properties
	curVChip.UsedRes.Add(*podVResource)
	curVChip.FreeRes.Sub(*podVResource)
	curVChip.UpdateDVPP(podVResource.DVPP)
}

func getPodUsedRes(device *vnpu.NPUDevices, pod *v1.Pod, taskTemplate map[string]map[string]vnpu.VResource) *vnpu.VResource {
	realStr, ok := pod.Annotations[util.AscendNPUCore]
	if !ok {
		klog.V(util.LogErrorLev).Infof("getPodUsedRes get pod<%s> %s value failed", pod.Name,
			util.AscendNPUCore)
		return nil
	}
	ascendRealSplit := strings.Split(realStr, "-")
	if len(ascendRealSplit) != util.NPUIndex2 {
		klog.V(util.LogErrorLev).Infof("getPodUsedRes get pod<%s> %s format error", pod.Name, realStr)
		return nil
	}
	if device.ChipKind == util.Ascend310P {
		return getResourceFromTemplate(device.ChipKind, ascendRealSplit[1], taskTemplate)
	}
	return getResourceFromTemplate(device.ChipType, ascendRealSplit[1], taskTemplate)
}

// getResourceFromTemplate nodeType like Ascend310P, templateString like "vir04_3c_ndvpp"
func getResourceFromTemplate(nodeType string, templateString string,
	taskTemplate map[string]map[string]vnpu.VResource) *vnpu.VResource {
	taskNodeTemplate, ok := taskTemplate[nodeType]
	if !ok {
		return nil
	}
	taskResource, ok := taskNodeTemplate[templateString]
	if !ok {
		return nil
	}
	return &taskResource
}

// NewVChip create new vChip
func NewVChip(device *vnpu.NPUDevices, id int, totalRes vnpu.VResource) *vnpu.VChip {
	if device == nil {
		klog.V(util.LogDebugLev).Infof("NewVChip failed: %s", util.ArgumentError)
		return nil
	}
	chipName := device.ChipKind + "-" + strconv.Itoa(id)
	vChip := vnpu.VChip{
		PodMap:   make(map[string]*v1.Pod, util.MapInitNum),
		Name:     chipName,
		Kind:     device.ChipKind,
		CoreNum:  device.AiCorePerChip,
		TotalRes: totalRes,
		FreeRes:  totalRes,
	}
	vChip.TotalRes.DVPP = plugin.AscendDVPPEnabledOff
	vChip.UsedRes.DVPP = plugin.AscendDVPPEnabledOff
	vChip.FreeRes.DVPP = plugin.AscendDVPPEnabledOn

	if strings.HasPrefix(device.ServerType, util.ServerTypeDual) {
		vChip.SetIsDual(true)
	}

	return &vChip
}

// getCardPhysicsIDFromAscendCore get card physics id from 0,1/0-vir04
func getCardPhysicsIDFromAscendCore(pod *v1.Pod, isWholeCard bool) ([]int, error) {
	physicsIDs := make([]int, 0)
	if pod == nil {
		return physicsIDs, fmt.Errorf("pod is nil")
	}
	coreNameStr, ok := pod.Annotations[util.AscendNPUCore]
	if !ok {
		return physicsIDs, fmt.Errorf("getCardPhysicsIDFromAscendCore vnpu device <%s> get %s value failed",
			pod.Name, util.AscendNPUCore)
	}

	if !isWholeCard {
		phyCardID, err := getVNPUCardIDFromAscendCore(coreNameStr)
		if err != nil {
			return physicsIDs, fmt.Errorf("getCardPhysicsIDFromAscendCore vnpu device <%s> get id failed",
				coreNameStr)
		}
		physicsIDs = append(physicsIDs, phyCardID)
		return physicsIDs, nil
	}
	coreNameSplit := strings.Split(coreNameStr, ",")
	for _, id := range coreNameSplit {
		phyCardID, err := strconv.Atoi(id)
		if err != nil {
			return physicsIDs, fmt.Errorf("getCardPhysicsIDFromAscendCore device <%s> get physics id failed",
				coreNameStr)
		}
		physicsIDs = append(physicsIDs, phyCardID)
	}
	return physicsIDs, nil
}

func getVNPUCardIDFromAscendCore(coreNameStr string) (int, error) {
	coreNameSplit := strings.Split(coreNameStr, "-")
	if len(coreNameSplit) != util.NPUIndex2 {
		return 0, fmt.Errorf("getVNPUCardIDFromAscendCore vnpu real device <%s> format error", coreNameStr)
	}
	phyCardID, err := strconv.Atoi(coreNameSplit[0])
	if err != nil {
		return 0, fmt.Errorf("getVNPUCardIDFromAscendCore vnpu device <%s> get physics id failed", coreNameStr)
	}
	return phyCardID, nil
}

// addNPUResourceWholeCard Ascend910-0,Ascend910-1
func addNPUResourceWholeCard(device *vnpu.NPUDevices, pod *v1.Pod) {
	physicsID, err := getCardPhysicsIDFromAscendCore(pod, true)
	if err != nil || len(physicsID) == 0 {
		return
	}
	for _, id := range physicsID {
		_, isCardunhealthy := device.UnhealthyChipIds[id]
		if isCardunhealthy {
			continue
		}
		// 1. get resource of pod, which is chip total resource
		podVResource := getVChipTotalRes(device)

		// 2. get chip id
		curVChip, ok := device.Chips[id]
		if !ok {
			curVChip = NewVChip(device, id, podVResource)
			device.Chips[id] = curVChip
		}

		// 3. update node
		curVChip.Unstable = curVChip.IsPodResUnstable(pod) || curVChip.Unstable
		curVChip.AddRealCardID(strconv.Itoa(id))
		curVChip.AddPodToPodMap(pod)
		curVChip.UsedRes.Add(podVResource)
		curVChip.FreeRes.Sub(podVResource)
	}
}

// isPodWholeCardFromAscendCore judge if card is whole card 0,1/0-vir04
func isPodWholeCardFromAscendCore(coreCardName string) bool {
	temp := strings.Split(coreCardName, ",")
	for _, cardName := range temp {
		singleCardTemp := strings.Split(cardName, "-")
		if len(singleCardTemp) == util.NPUIndex1 {
			return true
		}
	}
	return false
}

// isNPUTask to judge the task either is NPU task or not.
func isNPUTask(nT *api.TaskInfo) bool {
	for k := range nT.Resreq.ScalarResources {
		// must contain "huawei.com/"
		if strings.Contains(string(k), util.HwPreName) {
			return true
		}
	}
	return false
}

func setUnhealthyChipIds(device *vnpu.NPUDevices) error {
	unhealthyCardIDs, getErr := getCardIDsFromNodeAndDeviceInfo(device, vnpu.UnhealthyCardSuffix)
	if getErr != nil {
		return fmt.Errorf("getFreeCardIDsFromDeviceInfo %s", getErr)
	}
	for _, unhealthyCardID := range unhealthyCardIDs {
		device.NPUDevice.UnhealthyChipIds[unhealthyCardID] = struct{}{}
	}
	return nil
}

func getCardIDsFromNodeAndDeviceInfo(device *vnpu.NPUDevices, cardHealthTypeSuffix string) ([]int, error) {
	// 1. get health chips
	ChipsStr, ok := device.Annotation[util.HwPreName+device.NPUDevice.ChipKind+cardHealthTypeSuffix]
	if !ok {
		klog.V(util.LogDebugLev).Infof("%s get healthy card failed", device.NodeInf.Name)
		return nil, fmt.Errorf("no key: %s", util.HwPreName+device.NPUDevice.ChipKind)
	}

	CardIDs := make([]int, 0)
	Chips := strings.Split(ChipsStr, ",")
	for _, chip := range Chips {
		if chip == "" {
			continue
		}
		strID := strings.TrimPrefix(chip, device.NPUDevice.ChipKind+"-")
		chipID, aErr := strconv.Atoi(strID)
		if aErr != nil {
			klog.V(util.LogDebugLev).Infof("%s %s covert to int %s", chip, strID, util.SafePrint(aErr))
			continue
		}
		CardIDs = append(CardIDs, chipID)
	}

	if len(CardIDs) == 0 {
		return nil, fmt.Errorf("nil cards in %s", device.NodeInf.Name)
	}
	return CardIDs, nil
}

func createNodeNewVChips(device *vnpu.NPUDevices, chipTotalRes vnpu.VResource) error {
	healthyCardIDs, getErr := getCardIDsFromNodeAndDeviceInfo(device, vnpu.CardHealthySuffix)
	if getErr != nil {
		return fmt.Errorf("getFreeCardIDsFromDeviceInfo %s", util.SafePrint(getErr))
	}
	klog.V(util.LogDebugLev).Infof("createNodeNewVChips healthy chips: %#v", healthyCardIDs)
	for _, freeCardID := range healthyCardIDs {
		device.NPUDevice.Chips[freeCardID] = NewVChip(device, freeCardID, chipTotalRes)
	}
	return nil
}

// setTotalResAndChipNumByTemplates set totalRes, totalChipNum and serverType
func setTotalResAndChipNumByTemplates(device *vnpu.NPUDevices) error {
	// 1. get and set total AiCore from capability like Capacity: huawei.com/npu-core: 56
	totalCore, ok := device.Capability[util.AscendNPUCore]
	if !ok {
		return fmt.Errorf("getTotalResFromNpuNode no resource <%s>", util.AscendNPUCore)
	}

	device.NPUDevice.TotalRes.Aicore = int(totalCore / util.NPUHexKilo)
	klog.V(util.LogDebugLev).Infof("DEBUG: node %s, totalCore from Capability: %f", device.NodeInf.Name, totalCore)
	klog.V(util.LogDebugLev).Infof("DEBUG: node %s, after division: %d", device.NodeInf.Name, int(totalCore/util.NPUHexKilo))

	numCorePerChip, err := getVChipCoreNum(device)
	if err != nil || numCorePerChip == 0 {
		return fmt.Errorf("getTotalChipNum error: %v or numCorePerChip zero number: %d",
			util.SafePrint(err), numCorePerChip)
	}
	device.AiCorePerChip = numCorePerChip

	// 2.2 get totalChipNum use totalChipNum = totalCoreNum / coreNumPerChip
	totalChipNum, err := getTotalChipNum(device)
	if err != nil {
		return fmt.Errorf("getTotalResFromNpuNode failed: %v", err)
	}
	device.NPUDevice.TotalChipNum = totalChipNum

	// 2.2 get cpuNum per chip use totalAiCpuNum = aicpuNumPerChip * totalChipNum
	templates := initTemplate()
	cpuPerChip := getCpuNumPerChip(device, templates)
	if cpuPerChip == util.ErrorInt {
		return errors.New("getTotalResFromNpuNode get aicpu from template failed")
	}
	device.NPUDevice.TotalRes.Aicpu = cpuPerChip * totalChipNum
	device.NPUDevice.TotalRes.DVPP = plugin.AscendDVPPEnabledOff

	return nil
}

func getCpuNumPerChip(device *vnpu.NPUDevices, templates []VTemplate) int {
	cpuPerChip := util.ErrorInt
	for _, temp := range templates {
		if (temp.ChipKind != device.NPUDevice.ChipKind && temp.ChipKind != device.NPUDevice.ChipType) ||
			temp.AICore*device.TotalChipNum != device.NPUDevice.TotalRes.Aicore {
			continue
		}
		cpuPerChip = temp.AICPU
	}
	return cpuPerChip
}

type VTemplate struct {
	// ChipKind Ascend910/Ascend310P
	ChipKind   string
	AICore     int
	AICPU      int
	DVPPEnable string
}

func initTemplate() []VTemplate {
	nodeTemplate := make([]VTemplate, util.NPUIndex7)
	if len(nodeTemplate) < util.NPUIndex7 {
		return nodeTemplate
	}
	nodeTemplate[0] = VTemplate{
		ChipKind: util.Ascend310P,
		AICore:   util.NPUIndex8,
		AICPU:    util.NPUIndex7,
	}
	nodeTemplate[util.NPUIndex1] = VTemplate{
		ChipKind: plugin.ChipTypeB1,
		AICore:   util.CoreNum25,
		AICPU:    util.CpuNum6,
	}
	nodeTemplate[util.NPUIndex2] = VTemplate{
		ChipKind: plugin.ChipTypeB2C,
		AICore:   util.CoreNum24,
		AICPU:    util.CpuNum6,
	}
	nodeTemplate[util.NPUIndex3] = VTemplate{
		ChipKind: plugin.ChipTypeB3,
		AICore:   util.CoreNum20,
		AICPU:    util.CpuNum6,
	}
	nodeTemplate[util.NPUIndex4] = VTemplate{
		ChipKind: plugin.ChipTypeB4,
		AICore:   util.CoreNum20,
		AICPU:    util.CpuNum6,
	}
	nodeTemplate[util.NPUIndex5] = VTemplate{
		ChipKind: plugin.ChipTypeB2,
		AICore:   util.CoreNum24,
		AICPU:    util.CpuNum6,
	}
	nodeTemplate[util.NPUIndex6] = VTemplate{
		ChipKind: util.Ascend310P,
		AICore:   util.CoreNum10,
		AICPU:    util.NPUIndex7,
	}
	return nodeTemplate
}

// getTotalChipNum used after aicorePerChip set
func getTotalChipNum(device *vnpu.NPUDevices) (int, error) {
	totalChipNum := device.TotalRes.Aicore / device.AiCorePerChip
	if device.TotalRes.Aicore%device.AiCorePerChip != 0 {
		return 0, errors.New("getTotalChipNum error: total resource cannot be divided by coreNumPerChip")
	}
	if totalChipNum == 0 {
		return 0, errors.New("getTotalChipNum error: total chip number zero")
	}
	return totalChipNum, nil
}

func getVChipCoreNum(device *vnpu.NPUDevices) (int, error) {
	serverTypeSplit := strings.Split(device.ServerType, "-")
	if len(serverTypeSplit) < util.NPUIndex2 {
		return 0, fmt.Errorf("getVChipCoreNum serverType %s format error", device.ServerType)
	}
	coreNum, err := strconv.Atoi(serverTypeSplit[1])
	if err != nil {
		return 0, fmt.Errorf("getVChipCoreNum serverType %s split error", device.ServerType)
	}
	return coreNum, nil
}

func getVChipTotalRes(device *vnpu.NPUDevices) vnpu.VResource {
	AiCore := device.TotalRes.Aicore / device.TotalChipNum
	AiCpu := device.TotalRes.Aicpu / device.TotalChipNum
	return vnpu.VResource{
		Aicore: AiCore,
		Aicpu:  AiCpu,
		DVPP:   plugin.AscendDVPPEnabledOff,
	}
}

func checkDyVNodeResourceInitialized(device *vnpu.NPUDevices) bool {
	return device.Capability[util.AscendNPUCore] > 0
}

// setChipPropertiesFromNPUNode returns chipKind, chipLabel, accType
func setChipPropertiesFromNPUNode(device *vnpu.NPUDevices) error {
	chipKind, err := GetChipKindFromNpuNode(device) // 1. set ChipKind(Ascend910/Ascend310/Ascend310P)
	if err != nil {
		return fmt.Errorf("setNodeVNPUInfo node %s: %v", device.NodeInf.Name, err)
	}
	device.NPUDevice.ChipKind = chipKind

	chipLabel, ok := device.Label[util.ServerType] // 2. set ServerType(like Ascend310P-10-dual/Ascend910-30)
	if !ok {
		return fmt.Errorf("setNodeVNPUInfo node %s no node label <%s>", device.NodeInf.Name, util.ServerType)
	}
	device.NPUDevice.ServerType = chipLabel

	chipType, ok := device.Label[vnpu.ChipTypeKey]
	if !ok {
		return fmt.Errorf("setNodeVNPUInfo node %s no node label <%s>", device.NodeInf.Name, vnpu.ChipTypeKey)
	}
	device.NPUDevice.ChipType = chipType

	nodeFreeChips, ok := device.Annotation[util.HwPreName+device.NPUDevice.ChipKind] // 3. set free chip num from device-info
	if !ok {
		return errors.New("getFreeChipNum failed")
	}

	nodeFreeChipsSplit := strings.Split(nodeFreeChips, ",")
	device.NPUDevice.FreeChipNum = len(nodeFreeChipsSplit)

	return nil
}

// GetChipKindFromNpuNode input huawei-Ascend910 return Ascend910/Ascend310p/Ascend310
func GetChipKindFromNpuNode(device *vnpu.NPUDevices) (string, error) {
	tempVal, ok := device.Label[util.Accelerator]
	if !ok {
		return "", fmt.Errorf("getChipKindFromNpuNode label %s absent", util.Accelerator)
	}
	chipKind := strings.Split(tempVal, "-")
	if len(chipKind) < util.NPUIndex2 {
		return "", fmt.Errorf("getChipKindFromNpuNode label %s value %s %s", util.Accelerator,
			chipKind, plugin.FormatIncorrectError)
	}
	return chipKind[1], nil
}

// updateNPUNodeDeviceInfos return true if device info was updated, else return false
func updateNPUNodeDeviceInfos(device *vnpu.NPUDevices, data k8s.NodeDeviceInfoWithID) {
	if device.DevInfoUpdateTime >= data.UpdateTime {
		klog.V(util.LogDebugLev).Infof("device info is not update, skip refresh cache")
		return
	}
	device.SuperPodID = data.SuperPodID

	updateNPUNodeDeviceInfosWithVolcanoCache(device, data, data.UpdateTime)

	device.DevInfoUpdateTime = data.UpdateTime
	klog.V(util.LogDebugLev).Infof("update device info for node<%s> annotations: %v", device.NodeInf.Name, device.Annotation)
}

func updateNPUNodeDeviceInfosWithVolcanoCache(device *vnpu.NPUDevices, data k8s.NodeDeviceInfoWithID, updateTime int64) {
	for k, v := range data.DeviceList {
		// if k does not represent huawei.com/Ascend910/310/310P continue
		if len(strings.Split(k, "-")) > 1 {
			device.Annotation[k] = v
			continue
		}
		// if time interval over 10s continue
		if updateTime-device.DevInfoUpdateTime > vnpu.DeviceInfoForceUpdateInterval {
			device.Annotation[k] = v
			continue
		}
		device.Annotation[k] = getRealHealthyDeviceList(device, k, device.Annotation[k], v)
	}
}

func getRealHealthyDeviceList(device *vnpu.NPUDevices, deviceKey, oldList, newList string) string {
	// if cache card list is empty or device info is empty. update by device info
	if len(oldList) == 0 || len(newList) == 0 {
		return newList
	}
	newDeviceList := strings.Split(newList, ",")
	oldDeviceList := strings.Split(oldList, ",")

	// if cache is not equal k8s or device info is equal k8s. update by device info
	if int(device.Idle[v1.ResourceName(deviceKey)]/util.NPUHexKilo) != len(oldDeviceList) ||
		int(device.Idle[v1.ResourceName(deviceKey)]/util.NPUHexKilo) == len(newDeviceList) {
		return newList
	}

	klog.V(util.LogDebugLev).Infof("DEBUG: node %s, totalIdle from Capability: %d", device.NodeInf.Name, int(device.Idle[v1.ResourceName(deviceKey)]/util.NPUHexKilo))

	oldDevices := make(map[string]struct{})
	for _, device := range oldDeviceList {
		oldDevices[device] = struct{}{}
	}
	var deviceListCache []string
	for _, newDevice := range newDeviceList {
		if _, ok := oldDevices[newDevice]; !ok {
			continue
		}
		deviceListCache = append(deviceListCache, newDevice)
	}
	klog.V(util.LogWarningLev).Infof("update device info for node<%s> annotations: %#v", device.NodeInf.Name, deviceListCache)
	return strings.Join(deviceListCache, ",")
}

// syncAnnotation 4 parts, 1 v1.node annotations, 2 last session device infos, 3 switch info, 4 noded info
func syncAnnotation(device *vnpu.NPUDevices, npuNode *api.NodeInfo, nodeInfoOfNodeD k8s.NodeDNodeInfo) {
	existAnno := make(map[string]string)
	// 1. sync v1.node annotations
	for k, v := range npuNode.Node.Annotations {
		existAnno[k] = v
	}
	// 2. last session device infos
	for annoKey, annoValue := range device.Annotation {
		if strings.Contains(annoKey, util.HwPreName) {
			existAnno[annoKey] = annoValue
			continue
		}
	}
	// 3. noded info. adding noded reported info into NPUNode.Annotation including node healthy status
	// when there are no faults on the node, node info cm does not exist
	if nodeInfoOfNodeD.NodeStatus != "" {
		existAnno[vnpu.NodedNodeHealtyStatuskey] = nodeInfoOfNodeD.NodeStatus
	} else {
		existAnno[vnpu.NodedNodeHealtyStatuskey] = util.NodeHealthyByNodeD
	}
	device.Annotation = existAnno
}

// getNPUNodeCapacity get npu node Capacity by diff volcano version
func getNPUNodeCapacity(npuNode *api.NodeInfo) map[v1.ResourceName]float64 {
	klog.V(util.LogDebugLev).Infof("Enter getNPUNodeCapacity function")

	valueOfP := reflect.ValueOf(*npuNode)
	if valueOfP.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < valueOfP.NumField(); i++ {
		if valueOfP.Type().Field(i).Name != vnpu.OldCapacity && valueOfP.Type().Field(i).Name != vnpu.NewCapacity {
			continue
		}
		if capacity, ok := valueOfP.Field(i).Interface().(*api.Resource); ok {
			return capacity.ScalarResources
		}
		klog.V(util.LogErrorLev).Info("get capacity failed by not meet the resource type")
		return nil
	}
	return nil
}

// getNPUNodeAddress get npu node address
func getNPUNodeAddress(npuNode *api.NodeInfo) string {
	for _, addr := range npuNode.Node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			return addr.Address
		}
	}
	return ""
}
