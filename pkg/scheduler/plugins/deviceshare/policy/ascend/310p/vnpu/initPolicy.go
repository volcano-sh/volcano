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
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/deviceshare/config"
	"volcano.sh/volcano/pkg/scheduler/plugins/deviceshare/policy/ascend/common/k8s"
)

func InitVNPUDevice(device *vnpu.NPUDevices, ssn *framework.Session, nodeInfo *api.NodeInfo) error {
	if ssn == nil {
		klog.V(vnpu.LogDebugLev).Infof("InitVNPUDevice failed: %s.", vnpu.ArgumentError)
		return errors.New(vnpu.ArgumentError)
	}

	klog.V(vnpu.LogDebugLev).Infof("enter %s InitVNPUDevice.", "DeviceShare")
	defer klog.V(vnpu.LogDebugLev).Infof("leave %s InitNPUSession.", "DeviceShare")

	// device初始化方法，把信息都转换为基本数据结构（不包括api包）存储到device struct中
	initVolcanoFrameFromSsn(device, ssn)

	initCmInformer(device)

	initNodeFromSsn(device, nodeInfo)

	// 调用设备的内部初始化方法
	return nil
}

// initCmInformer init cm informer, support cluster info manager and device plugin
func initCmInformer(device *vnpu.NPUDevices) {
	if device.FrameAttr.KubeClient == nil {
		klog.V(vnpu.LogErrorLev).Info("kube client in session is nil")
		return
	}
	k8s.InitCmInformer(device.FrameAttr.KubeClient, device.FrameAttr.UseClusterD)
}

func initVolcanoFrameFromSsn(device *vnpu.NPUDevices, ssn *framework.Session) {
	if ssn == nil {
		klog.V(vnpu.LogErrorLev).Infof("InitVolcanoFrameFromSsn failed: %s.", vnpu.ArgumentError)
		return
	}

	configs := getConfigurationByKey(initConfsFromSsn(ssn.Configurations))
	device.FrameAttr.UID = ssn.UID
	device.FrameAttr.KubeClient = ssn.KubeClient()
	device.FrameAttr.InformerFactory = ssn.InformerFactory()
	device.FrameAttr.VJobTemplate = map[string]map[string]vnpu.VResource{
		vnpu.Ascend310P: {
			vnpu.VNPUTempVir01:        {Aicore: 1, Aicpu: 1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir02:        {Aicore: vnpu.NPUIndex2, Aicpu: vnpu.NPUIndex2, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir02C1:      {Aicore: vnpu.NPUIndex2, Aicpu: 1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir04:        {Aicore: vnpu.NPUIndex4, Aicpu: vnpu.NPUIndex4, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir04C3:      {Aicore: vnpu.NPUIndex4, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir04C3NDVPP: {Aicore: vnpu.NPUIndex4, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledOff},
			vnpu.VNPUTempVir04C4cDVPP: {Aicore: vnpu.NPUIndex4, Aicpu: vnpu.NPUIndex4, DVPP: vnpu.AscendDVPPEnabledOn},
		},
		vnpu.Ascend910: {
			vnpu.VNPUTempVir02: {Aicore: vnpu.NPUIndex2, Aicpu: 1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir04: {Aicore: vnpu.NPUIndex4, Aicpu: 1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir08: {Aicore: vnpu.NPUIndex8, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir16: {Aicore: vnpu.NPUIndex16, Aicpu: vnpu.NPUIndex7, DVPP: vnpu.AscendDVPPEnabledNull},
		},
		vnpu.ChipTypeB1: {
			vnpu.VNPUTempVir06: {Aicore: vnpu.NPUIndex6, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir03: {Aicore: vnpu.NPUIndex3, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir12: {Aicore: vnpu.NPUIndex12, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
		},
		vnpu.ChipTypeB2C: {
			vnpu.VNPUTempVir06: {Aicore: vnpu.NPUIndex6, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir03: {Aicore: vnpu.NPUIndex3, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir12: {Aicore: vnpu.NPUIndex12, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
		},
		vnpu.ChipTypeB2: {
			vnpu.VNPUTempVir06: {Aicore: vnpu.NPUIndex6, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir03: {Aicore: vnpu.NPUIndex3, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir12: {Aicore: vnpu.NPUIndex12, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
		},
		vnpu.ChipTypeB3: {
			vnpu.VNPUTempVir05: {Aicore: vnpu.NPUIndex5, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUTempVir10: {Aicore: vnpu.NPUIndex10, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
		},
		vnpu.ChipTypeB4: {
			vnpu.VNPUB4TempVir05:     {Aicore: vnpu.NPUIndex5, Aicpu: vnpu.NPUIndex1, DVPP: vnpu.AscendDVPPEnabledNull},
			vnpu.VNPUB4TempVir10C3NM: {Aicore: vnpu.NPUIndex10, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledOff},
			vnpu.VNPUB4TempVir10C4M:  {Aicore: vnpu.NPUIndex10, Aicpu: vnpu.NPUIndex4, DVPP: vnpu.AscendDVPPEnabledOn},
			vnpu.VNPUB4TempVir10:     {Aicore: vnpu.NPUIndex10, Aicpu: vnpu.NPUIndex3, DVPP: vnpu.AscendDVPPEnabledNull},
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
		klog.V(vnpu.LogWarningLev).Infof("init static parameters, UseClusterD"+
			" is <%v>", device.FrameAttr.UseClusterD)
	})
}

// getUseClusterDConfig check use cluster info manager by config, default true
func getUseClusterDConfig(conf map[string]string) bool {
	useClusterInfoManager, ok := conf[vnpu.UseClusterInfoManager]
	if !ok {
		klog.V(vnpu.LogDebugLev).Info("CheckUseCIMByConfig doesn't exist useClusterInfoManager.")
		return true
	}
	return useClusterInfoManager == "true"
}

// getSelfMaintainAvailCard check volcano self maintain available card by config, default true
func getSelfMaintainAvailCard(conf map[string]string) bool {
	selfMaintainAvailCard, ok := conf[vnpu.SelfMaintainAvailCard]
	if !ok {
		klog.V(vnpu.LogDebugLev).Info("CheckUseCIMByConfig doesn't exist self-maintain-available-card.")
		return true
	}
	return selfMaintainAvailCard == "true"
}

// initDynamicParameters
func initDynamicParameters(device *vnpu.NPUDevices, configs map[string]string) {
	if device == nil || configs == nil {
		klog.V(vnpu.LogInfoLev).Infof("InitCache failed: %s.", vnpu.ArgumentError)
		return
	}
	device.FrameAttr.PresetVirtualDevice = getPresetVirtualDeviceConfig(configs)
}

// getPresetVirtualDeviceConfig get VNPU segmentEnable by init plugin parameters, return true if static
func getPresetVirtualDeviceConfig(conf map[string]string) bool {
	// get segmentEnable by user configuration
	segmentEnable, ok := conf[vnpu.SegmentEnable]
	if !ok {
		klog.V(vnpu.LogDebugLev).Info("checkVNPUSegmentEnable doesn't exist presetVirtualDevice.")
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
			klog.V(vnpu.LogInfoLev).Infof("Marshal configuration failed: %s.", err)
			continue
		}
		if err = yaml.Unmarshal(out, newCfg); err != nil {
			klog.V(vnpu.LogInfoLev).Infof("Unmarshal configuration failed: %s.", err)
			continue
		}
		newConfs[idx] = *newCfg
	}
	return newConfs
}

// getConfigurationByKey called by GetConfigFromSchedulerConfigMap
func getConfigurationByKey(configurations []config.Configuration) map[string]string {
	for _, cf := range configurations {
		if cf.Name == vnpu.CMInitParamKey {
			return cf.Arguments
		}
	}
	return map[string]string{}
}

func initNodeFromSsn(device *vnpu.NPUDevices, nodeInfo *api.NodeInfo) {
	// 1.obtain device infos, and if node not in session, its device info should not keep in cache
	deviceInfos := k8s.GetDeviceInfoAndSetInformerStart(nodeInfo, device.FrameAttr.UseClusterD,
		device.FrameAttr.SelfMaintainAvailCard)
	// 2. obtain node infos of noded configmap
	nodeInfosOfNodeD := k8s.GetNodeDInfo(nodeInfo)
	// 3. obtain switch infos of switch configmap
	//switchInfos := k8s.GetSwitchInfos(nodeInfo)

	if err := initNPUNodeByNodeInf(device, nodeInfo, deviceInfos, nodeInfosOfNodeD, device.FrameAttr.VJobTemplate); err != nil &&
		!strings.Contains(err.Error(), vnpu.NoneResourceErr) {
		klog.V(vnpu.LogErrorLev).Infof("InitNodeFromSsn %s %s, not put in nodes.", nodeInfo.Name, err)
	}
}

// initNPUNodeByNodeInf init NPU node from node info and cm.
func initNPUNodeByNodeInf(device *vnpu.NPUDevices, npuNode *api.NodeInfo, deviceInfo k8s.NodeDeviceInfoWithID,
	nodeInfoOfNodeD k8s.NodeDNodeInfo, vJobTemplate map[string]map[string]vnpu.VResource) error {
	if device == nil || npuNode == nil {
		klog.V(vnpu.LogInfoLev).Infof("InitNPUNodeByNodeInf failed: %s.", vnpu.ArgumentError)
		return errors.New(vnpu.ArgumentError)
	}
	capability := getNPUNodeCapacity(npuNode)
	if !vnpu.IsMapHasNPUResource(capability, vnpu.HwPreName) {
		return fmt.Errorf("node %s npu resource is not enable", npuNode.Name)
	}
	if deviceInfo.DeviceList == nil {
		return fmt.Errorf("node %s device info or clusterd info is not enable", npuNode.Name)
	}
	device.NodeInf.Name = npuNode.Name
	device.Capability = capability
	device.BaseDeviceInfo = npuNode.Node.Annotations[vnpu.BaseDeviceInfoKey]
	device.NodeInf.Allocate = npuNode.Allocatable.ScalarResources
	device.Idle = npuNode.Idle.ScalarResources
	device.Label = npuNode.Node.Labels
	device.Address = getNPUNodeAddress(npuNode)
	//device.Tasks = npuNode.Tasks
	syncAnnotation(device, npuNode, nodeInfoOfNodeD)
	updateNPUNodeDeviceInfos(device, deviceInfo)

	if setVNPUErr := setNodeVNPUInfo(device, npuNode, vJobTemplate); setVNPUErr != nil {
		klog.V(vnpu.LogDebugLev).Infof("setNodeVNPUInfo %s %s", npuNode.Name, setVNPUErr)
	}
	klog.V(vnpu.LogDebugLev).Infof("initNPUNodeByNodeInf <%s> success %#v", npuNode.Name, device.NodeInf)
	return nil
}

func setNodeVNPUInfo(device *vnpu.NPUDevices, ni *api.NodeInfo, jobTemplate map[string]map[string]vnpu.VResource) error {
	device.NPUDevice = vnpu.NPUDevice{
		Chips:            make(map[int]*vnpu.VChip, vnpu.MapInitNum),
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
	klog.V(vnpu.LogDebugLev).Infof("setNodeVNPUInfo %s initialisation success:<%#v>", device.NodeInf.Name, device.NPUDevice)
	return nil
}

func initVChips(device *vnpu.NPUDevices, ni *api.NodeInfo, taskTemplate map[string]map[string]vnpu.VResource) error {
	chipTotalRes := getVChipTotalRes(device)
	if err := createNodeNewVChips(device, chipTotalRes); err != nil {
		klog.V(vnpu.LogDebugLev).Infof("vNode %s %s.", device.NodeInf.Name, vnpu.SafePrint(err))
	} // 3. create new VChip by freeCardID whole card

	if err := setUnhealthyChipIds(device); err != nil {
		klog.V(vnpu.LogDebugLev).Infof("vNode %s %s.", device.NodeInf.Name, err)
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
	coreNameStr, ok := pod.Annotations[vnpu.AscendNPUCore]
	if !ok {
		klog.V(vnpu.LogDebugLev).Infof("addNPUResource pod %s %s no value", pod.Name, vnpu.AscendNPUCore)
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
	if err != nil || len(physicsID) != vnpu.NPUIndex1 {
		klog.V(vnpu.LogErrorLev).Infof("addNPUResourceVNPUCard get pod<%s> card physics id failed", pod.Name)
		return
	}
	_, isCardunhealthy := device.UnhealthyChipIds[physicsID[0]]
	if isCardunhealthy {
		klog.V(vnpu.LogErrorLev).Infof("addNPUResourceVNPUCard get pod<%s> card is unhealthy", pod.Name)
		return
	}
	// 2. add chip to node
	curVChip, ok := device.Chips[physicsID[0]]
	if !ok {
		curVChip = NewVChip(device, physicsID[0], chipTotalRes)
		device.Chips[physicsID[0]] = curVChip
	}
	curVChip.Unstable = curVChip.IsPodResUnstable(pod) || curVChip.Unstable
	curVChip.AddRealCardID(pod.Annotations[vnpu.AscendNPUPodRealUse])
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
	if device.ChipKind == vnpu.Ascend310P {
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
	vChip.TotalRes.DVPP = vnpu.AscendDVPPEnabledOff
	vChip.UsedRes.DVPP = vnpu.AscendDVPPEnabledOff
	vChip.FreeRes.DVPP = vnpu.AscendDVPPEnabledOn

	if strings.HasPrefix(device.ServerType, vnpu.ServerTypeDual) {
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
		if strings.Contains(string(k), vnpu.HwPreName) {
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
	ChipsStr, ok := device.Annotation[vnpu.HwPreName+device.NPUDevice.ChipKind+cardHealthTypeSuffix]
	if !ok {
		klog.V(vnpu.LogDebugLev).Infof("%s get healthy card failed", device.NodeInf.Name)
		return nil, fmt.Errorf("no key: %s", vnpu.HwPreName+device.NPUDevice.ChipKind)
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
			klog.V(vnpu.LogDebugLev).Infof("%s %s covert to int %s", chip, strID, util.SafePrint(aErr))
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
	totalCore, ok := device.Capability[vnpu.AscendNPUCore]
	if !ok {
		return fmt.Errorf("getTotalResFromNpuNode no resource <%s>", vnpu.AscendNPUCore)
	}
	device.NPUDevice.TotalRes.Aicore = int(totalCore / vnpu.NPUHexKilo)

	numCorePerChip, err := getVChipCoreNum(device)
	if err != nil || numCorePerChip == 0 {
		return fmt.Errorf("getTotalChipNum error: %v or numCorePerChip zero number: %d",
			vnpu.SafePrint(err), numCorePerChip)
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
	if cpuPerChip == vnpu.ErrorInt {
		return errors.New("getTotalResFromNpuNode get aicpu from template failed")
	}
	device.NPUDevice.TotalRes.Aicpu = cpuPerChip * totalChipNum
	device.NPUDevice.TotalRes.DVPP = vnpu.AscendDVPPEnabledOff

	return nil
}

func getCpuNumPerChip(device *vnpu.NPUDevices, templates []VTemplate) int {
	cpuPerChip := vnpu.ErrorInt
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
	nodeTemplate := make([]VTemplate, vnpu.NPUIndex7)
	if len(nodeTemplate) < vnpu.NPUIndex7 {
		return nodeTemplate
	}
	nodeTemplate[0] = VTemplate{
		ChipKind: vnpu.Ascend310P,
		AICore:   vnpu.NPUIndex8,
		AICPU:    vnpu.NPUIndex7,
	}
	nodeTemplate[vnpu.NPUIndex1] = VTemplate{
		ChipKind: vnpu.ChipTypeB1,
		AICore:   vnpu.CoreNum25,
		AICPU:    vnpu.CpuNum6,
	}
	nodeTemplate[vnpu.NPUIndex2] = VTemplate{
		ChipKind: vnpu.ChipTypeB2C,
		AICore:   vnpu.CoreNum24,
		AICPU:    vnpu.CpuNum6,
	}
	nodeTemplate[vnpu.NPUIndex3] = VTemplate{
		ChipKind: vnpu.ChipTypeB3,
		AICore:   vnpu.CoreNum20,
		AICPU:    vnpu.CpuNum6,
	}
	nodeTemplate[vnpu.NPUIndex4] = VTemplate{
		ChipKind: vnpu.ChipTypeB4,
		AICore:   vnpu.CoreNum20,
		AICPU:    vnpu.CpuNum6,
	}
	nodeTemplate[vnpu.NPUIndex5] = VTemplate{
		ChipKind: vnpu.ChipTypeB2,
		AICore:   vnpu.CoreNum24,
		AICPU:    vnpu.CpuNum6,
	}
	nodeTemplate[vnpu.NPUIndex6] = VTemplate{
		ChipKind: vnpu.Ascend310P,
		AICore:   vnpu.CoreNum10,
		AICPU:    vnpu.NPUIndex7,
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
	if len(serverTypeSplit) < vnpu.NPUIndex2 {
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
		DVPP:   vnpu.AscendDVPPEnabledOff,
	}
}

func checkDyVNodeResourceInitialized(device *vnpu.NPUDevices) bool {
	return device.Capability[vnpu.AscendNPUCore] > 0
}

// setChipPropertiesFromNPUNode returns chipKind, chipLabel, accType
func setChipPropertiesFromNPUNode(device *vnpu.NPUDevices) error {
	chipKind, err := GetChipKindFromNpuNode(device) // 1. set ChipKind(Ascend910/Ascend310/Ascend310P)
	if err != nil {
		return fmt.Errorf("setNodeVNPUInfo node %s: %v", device.NodeInf.Name, err)
	}
	device.NPUDevice.ChipKind = chipKind

	chipLabel, ok := device.Label[vnpu.ServerType] // 2. set ServerType(like Ascend310P-10-dual/Ascend910-30)
	if !ok {
		return fmt.Errorf("setNodeVNPUInfo node %s no node label <%s>", device.NodeInf.Name, vnpu.ServerType)
	}
	device.NPUDevice.ServerType = chipLabel

	chipType, ok := device.Label[vnpu.ChipTypeKey]
	if !ok {
		return fmt.Errorf("setNodeVNPUInfo node %s no node label <%s>", device.NodeInf.Name, vnpu.ChipTypeKey)
	}
	device.NPUDevice.ChipType = chipType

	nodeFreeChips, ok := device.Annotation[vnpu.HwPreName+device.NPUDevice.ChipKind] // 3. set free chip num from device-info
	if !ok {
		return errors.New("getFreeChipNum failed")
	}

	nodeFreeChipsSplit := strings.Split(nodeFreeChips, ",")
	device.NPUDevice.FreeChipNum = len(nodeFreeChipsSplit)

	return nil
}

// GetChipKindFromNpuNode input huawei-Ascend910 return Ascend910/Ascend310p/Ascend310
func GetChipKindFromNpuNode(device *vnpu.NPUDevices) (string, error) {
	tempVal, ok := device.Label[vnpu.Accelerator]
	if !ok {
		return "", fmt.Errorf("getChipKindFromNpuNode label %s absent", vnpu.Accelerator)
	}
	chipKind := strings.Split(tempVal, "-")
	if len(chipKind) < vnpu.NPUIndex2 {
		return "", fmt.Errorf("getChipKindFromNpuNode label %s value %s %s", vnpu.Accelerator,
			chipKind, vnpu.FormatIncorrectError)
	}
	return chipKind[1], nil
}

// updateNPUNodeDeviceInfos return true if device info was updated, else return false
func updateNPUNodeDeviceInfos(device *vnpu.NPUDevices, data k8s.NodeDeviceInfoWithID) {
	if device.DevInfoUpdateTime >= data.UpdateTime {
		klog.V(vnpu.LogDebugLev).Infof("device info is not update, skip refresh cache")
		return
	}
	device.SuperPodID = data.SuperPodID

	updateNPUNodeDeviceInfosWithVolcanoCache(device, data, data.UpdateTime)

	device.DevInfoUpdateTime = data.UpdateTime
	klog.V(vnpu.LogDebugLev).Infof("update device info for node<%s> annotations: %v", device.NodeInf.Name, device.Annotation)
	return
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
	if int(device.Idle[v1.ResourceName(deviceKey)]/vnpu.NPUHexKilo) != len(oldDeviceList) ||
		int(device.Idle[v1.ResourceName(deviceKey)]/vnpu.NPUHexKilo) == len(newDeviceList) {
		return newList
	}
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
	klog.V(vnpu.LogWarningLev).Infof("update device info for node<%s> annotations: %#v", device.NodeInf.Name, deviceListCache)
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
		if strings.Contains(annoKey, vnpu.HwPreName) {
			existAnno[annoKey] = annoValue
			continue
		}
	}
	// 3. noded info. adding noded reported info into NPUNode.Annotation including node healthy status
	// when there are no faults on the node, node info cm does not exist
	if nodeInfoOfNodeD.NodeStatus != "" {
		existAnno[vnpu.NodedNodeHealtyStatuskey] = nodeInfoOfNodeD.NodeStatus
	} else {
		existAnno[vnpu.NodedNodeHealtyStatuskey] = vnpu.NodeHealthyByNodeD
	}
	device.Annotation = existAnno
}

// getNPUNodeCapacity get npu node Capacity by diff volcano version
func getNPUNodeCapacity(npuNode *api.NodeInfo) map[v1.ResourceName]float64 {
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
		klog.V(vnpu.LogErrorLev).Info("get capacity failed by not meet the resource type")
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
