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

package vnpu

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func (ns *NPUDevices) addTaskInConCache(pod *v1.Pod, taskResReq VResource, chipVTemplate string) error {
	if ns == nil {
		return fmt.Errorf("addTaskInConCache failed:%s", ArgumentError)
	}

	date := ns.ConCache
	if date == nil {
		date = make(map[string]map[types.UID]struct{})
	}

	temp, ok := date[chipVTemplate]
	if !ok {
		temp = make(map[types.UID]struct{}, MapInitNum)
	}
	_, ok = temp[pod.UID]
	if ok {
		return nil
	}
	temp[pod.UID] = struct{}{}
	date[chipVTemplate] = temp
	ns.ConCache = date
	klog.V(LogDebugLev).Infof("addTaskInConCache %s %s ConCache: %v", ns.NodeInf.Name, pod.Name, ns.ConCache)
	return nil
}

func (ns *NPUDevices) releaseTaskInConCache(pod *v1.Pod, taskResReq VResource, chipVTemplate string) error {
	if ns == nil {
		return fmt.Errorf("releaseTaskInConCache failed:%s", ArgumentError)
	}

	temp := ns.ConCache
	if temp == nil {
		return fmt.Errorf("template %s not in %s ConCache", chipVTemplate, ns.NodeInf.Name)
	}
	tIDs, ok := temp[chipVTemplate]
	if !ok {
		return fmt.Errorf("template %s not in %s ConCache", chipVTemplate, ns.NodeInf.Name)
	}
	if _, ok := tIDs[pod.UID]; !ok {
		return fmt.Errorf("tID %s not in %s %s ConCache", pod.UID, chipVTemplate, ns.NodeInf.Name)
	}
	delete(tIDs, pod.UID)
	if len(tIDs) == 0 {
		delete(temp, chipVTemplate)
		ns.ConCache = temp
		return nil
	}
	temp[chipVTemplate] = tIDs
	ns.ConCache = temp
	return nil
}

// GetTemplateByResReq get template by resource request.
func (ns *NPUDevices) GetTemplateByResReq(taskResReq VResource, vt VTemplate) (string, error) {
	if ns == nil {
		return "", fmt.Errorf("getTemplateByResReq failed:%s", ArgumentError)
	}

	name := ""
	for tName, value := range vt.Data {
		if value.Aicore != taskResReq.Aicore {
			continue
		}
		if value.Aicpu != taskResReq.Aicpu {
			continue
		}
		if value.DVPP != taskResReq.DVPP {
			continue
		}
		name = tName
	}
	if name == "" {
		return "", fmt.Errorf("%#v not get template", taskResReq)
	}
	return name, nil
}

// UpdateNodeInfoSegment vnpu add resource in Device
func (ns *NPUDevices) UpdateNodeInfoSegmentWithAdd(allocChipID string, taskResReq VResource) {
	if ns == nil {
		klog.V(LogErrorLev).Infof("UpdateNodeInfoSegmentWithAdd error : %s", ArgumentError)
		return
	}

	for chipID, chip := range ns.Chips {
		if strconv.Itoa(chipID) != allocChipID {
			continue
		}
		chip.UsedRes.Add(taskResReq)
		chip.FreeRes.Sub(taskResReq)
		if !ns.IsResourceWholeCard(taskResReq.Aicore) {
			chip.SegmentFlag = true
		}
		chip.UpdateDVPP(taskResReq.DVPP)
	}
	klog.V(LogInfoLev).Infof("dynamic vnpu UpdateNodeInfo node <%s> chip resource updated", ns.NodeInf.Name)
}

// UpdateNodeInfoSegment vnpu sub resource in Device
func (ns *NPUDevices) UpdateNodeInfoSegmentWithSub(allocChipID string, taskResReq VResource) {
	if ns == nil {
		klog.V(LogErrorLev).Infof("UpdateNodeInfoSegmentWithSub error : %s", ArgumentError)
		return
	}

	for chipID, chip := range ns.Chips {
		if strconv.Itoa(chipID) != allocChipID {
			continue
		}
		chip.UsedRes.Sub(taskResReq)
		chip.FreeRes.Add(taskResReq)
		//if !ns.IsResourceWholeCard(taskResReq.Aicore) {
		//	chip.SegmentFlag = true
		//}
		chip.ResetDVPP(taskResReq.DVPP)
	}
	klog.V(LogInfoLev).Infof("dynamic vnpu UpdateNodeInfo node <%s> chip resource updated", ns.NodeInf.Name)
}

// UpdateNodeInfoWhole vnpu update npuNode after allocation for whole card tasks
func (ns *NPUDevices) UpdateNodeInfoWholeWithAdd(allocChipIDs string) {
	if ns == nil {
		klog.V(LogErrorLev).Infof("UpdateNodeInfoWholeWithAdd error : %s", ArgumentError)
		return
	}
	if ns.TotalChipNum == 0 {
		klog.V(LogErrorLev).Infof("UpdateNodeInfoWhole node <%s> total chip number equal zero", ns.NodeInf.Name)
		return
	}

	chipRes := VResource{
		Aicore: ns.AiCorePerChip,
		Aicpu:  ns.TotalRes.Aicpu / ns.TotalChipNum,
		DVPP:   AscendDVPPEnabledNull,
	}
	allocChipIDList := strings.Split(allocChipIDs, ",")
	for _, allocChipID := range allocChipIDList {
		for chipID, chip := range ns.Chips {
			if strconv.Itoa(chipID) != allocChipID {
				continue
			}
			chip.UsedRes.Add(chipRes)
			chip.FreeRes.Sub(chipRes)
			chip.UpdateDVPP(chipRes.DVPP)
		}
	}
}

// UpdateNodeInfoWholeWithSub vnpu update npuNode after allocation for whole card tasks
func (ns *NPUDevices) UpdateNodeInfoWholeWithSub(allocChipIDs string) {
	if ns == nil {
		klog.V(LogErrorLev).Infof("UpdateNodeInfoWholeWithSub error : %s", ArgumentError)
		return
	}
	if ns.TotalChipNum == 0 {
		klog.V(LogErrorLev).Infof("UpdateNodeInfoWhole node <%s> total chip number equal zero", ns.NodeInf.Name)
		return
	}

	chipRes := VResource{
		Aicore: ns.AiCorePerChip,
		Aicpu:  ns.TotalRes.Aicpu / ns.TotalChipNum,
		DVPP:   AscendDVPPEnabledNull,
	}
	allocChipIDList := strings.Split(allocChipIDs, ",")
	for _, allocChipID := range allocChipIDList {
		for chipID, chip := range ns.Chips {
			if strconv.Itoa(chipID) != allocChipID {
				continue
			}
			chip.UsedRes.Sub(chipRes)
			chip.FreeRes.Add(chipRes)
			chip.ResetDVPP(chipRes.DVPP)
		}
	}
}

func (ns *NPUDevices) downgradeTaskAICPU(podResReq VResource) VResource {
	if ns == nil {
		klog.V(LogErrorLev).Infof("downgradeTaskAICPU error : %s", ArgumentError)
		return podResReq
	}
	if podResReq.Aicore == NPUIndex2 {
		return VResource{
			Aicore: podResReq.Aicore,
			Aicpu:  NPUIndex1,
			DVPP:   podResReq.DVPP,
		}
	}
	if podResReq.Aicore == NPUIndex4 {
		return VResource{
			Aicore: podResReq.Aicore,
			Aicpu:  NPUIndex3,
			DVPP:   podResReq.DVPP,
		}
	}
	return podResReq
}

func (ns *NPUDevices) GetPodResource(pod *v1.Pod) (VResource, error) {
	if ns == nil || pod == nil {
		return VResource{}, fmt.Errorf("GetPodResource error : %s", ArgumentError)
	}

	coreNum, err := ns.getAiCoreNumFromPod(pod)
	if err != nil {
		return VResource{}, fmt.Errorf("task %s AscendNPUCore read failed", pod.Name)
	}
	tempCore := ns.TotalRes.Aicore
	if tempCore == 0 {
		klog.V(LogInfoLev).Infof("%s not inital for Aicore is 0", ns.NodeInf.Name)
		//return VResource{}, fmt.Errorf("%s not inital for Aicore is 0", ns.NodeInf.Name)
		return VResource{}, nil
	}
	if ns.IsResourceWholeCard(coreNum) {
		res := VResource{
			Aicore: coreNum,
			Aicpu:  coreNum * ns.TotalRes.Aicpu / tempCore,
			DVPP:   "null",
		}
		return res, nil
	}

	dvpp, err := ns.GetVTaskDVPP(pod)
	if err != nil {
		return VResource{}, err
	}

	cpuLevel := ns.GetVTaskLevel(pod)

	virTemplate := getResTemplateFromTaskSetting(coreNum, cpuLevel, dvpp)

	taskReqRes := ns.VT.Data[virTemplate]
	return taskReqRes, nil
}

func (ns *NPUDevices) GetVTaskDVPP(pod *v1.Pod) (string, error) {
	if ns == nil || pod == nil {
		return "", fmt.Errorf("GetVTaskDVPP error : %s", ArgumentError)
	}

	dvpp, ok := pod.Labels[AscendVNPUDVPP]
	if !ok {
		klog.V(LogWarningLev).Infof("%s not set VNPU dvpp, use default null.", pod.Name)
		return AscendDVPPEnabledNull, nil
	}
	switch dvpp {
	case AscendDVPPEnabledOff, AscendDVPPEnabledNull, AscendDVPPEnabledOn:
		break
	default:
		klog.V(LogWarningLev).Infof("%s set wrong dvpp %s.", pod.Name, dvpp)
		return "", fmt.Errorf("err dvpp value:%s", dvpp)
	}
	return dvpp, nil
}

func (ns *NPUDevices) GetVTaskLevel(pod *v1.Pod) string {
	if ns == nil || pod == nil {
		klog.V(LogErrorLev).Infof("GetVTaskLevel error : %s", ArgumentError)
		return ""
	}

	cpuLevel, ok := pod.Labels[AscendVNPULevel]
	if !ok {
		klog.V(LogWarningLev).Infof("%s not set VNPU level, use default low.", pod.Name)
		return AscendVNPULevelLow
	}
	switch cpuLevel {
	case AscendVNPULevelLow, AscendVNPULevelHigh:
		break
	default:
		klog.V(LogWarningLev).Infof("%s set wrong VNPU level %s, use default low.", pod.Name, cpuLevel)
		cpuLevel = AscendVNPULevelLow
	}
	return cpuLevel
}

// IsResourceWholeCard judge if resource is whole card by node total resource
func (ns *NPUDevices) IsResourceWholeCard(aiCore int) bool {
	if ns == nil {
		klog.V(4).Infof("IsResourceWholeCard failed: %s", "invalid argument")
		return false
	}
	chipCoreNum, err := ns.getVChipCoreNum()
	if err != nil || chipCoreNum == 0 {
		klog.V(2).Infof("IsResourceWholeCard get chipCoreNum failed or zero number")
		return false
	}
	return aiCore%chipCoreNum == 0
}

func (ns *NPUDevices) getVChipCoreNum() (int, error) {
	if ns == nil {
		return 0, fmt.Errorf("getVChipCoreNum failed: %s", "invalid argument")
	}
	serverTypeSplit := strings.Split(ns.ServerType, "-")
	if len(serverTypeSplit) < 2 {
		return 0, fmt.Errorf("getVChipCoreNum serverType %s format error", ns.ServerType)
	}
	coreNum, err := strconv.Atoi(serverTypeSplit[1])
	if err != nil {
		return 0, fmt.Errorf("getVChipCoreNum serverType %s split error", ns.ServerType)
	}
	return coreNum, nil
}

func (ns *NPUDevices) getAiCoreNumFromPod(pod *v1.Pod) (int, error) {
	if ns == nil {
		return 0, fmt.Errorf("getAiCoreNumFromPod failed: %s", "invalid argument")
	}

	for _, container := range pod.Spec.Containers {
		coreNum, ok := container.Resources.Requests["huawei.com/npu-core"]
		if !ok {
			continue
		}
		if coreNum.Value() == 0 {
			continue
		}
		return int(coreNum.Value()), nil
	}

	return 0, nil

	// original npu scheduling logic may lead to too much inval error in pod's log file
	//return 0, fmt.Errorf("getAiCoreNumFromTask get resource requests failed")
}

// PreCheckNodePredicate PreCheck Predicate nodes.
func (ns *NPUDevices) preCheckNodePredicate(pod *v1.Pod) error {
	nodeHealthyStatusByNodeD := ns.Annotation[NodedNodeHealtyStatuskey]
	if nodeHealthyStatusByNodeD == PreSeparateFaultCode {
		klog.V(LogDebugLev).Infof("NodePredicate %s failed, cause node is %s.", ns.NodeInf.Name,
			nodeHealthyStatusByNodeD)
		return fmt.Errorf("node is %s, due to nodeD reported node status", nodeHealthyStatusByNodeD)
	}

	// vNPU job no need to check
	//if err := ns.checkNPUResourceStable(pod); err != nil {
	//	return err
	//}
	if err := ns.checkNodeNum(pod); err != nil {
		return err
	}
	return nil
}

// checkNodeNum Check whether the number of cards on the node meets the task requirements.
func (ns *NPUDevices) checkNodeNum(pod *v1.Pod) error {
	if ns == nil {
		return errors.New(objectNilError)
	}

	nodeNPUNum, ok := ns.Idle[AscendNPUCore]
	klog.V(3).Infof("DEBUG: nodeNPUNum millicore from ns.Idle[huawei.com/npu-core]: %f", nodeNPUNum)
	klog.V(3).Infof("DEBUG: nodeNPUNum from ns.Idle[huawei.com/npu-core]: %d", int(nodeNPUNum/NPUHexKilo))
	reqNPUNum, err := ns.getAiCoreNumFromPod(pod)
	if err != nil {
		return fmt.Errorf("failed to get requested NPU number from pod: %v", err)
	}
	if !ok {
		return fmt.Errorf("not have %s", AscendNPUCore)
	}
	if int(nodeNPUNum/NPUHexKilo) < reqNPUNum {
		return fmt.Errorf("node not meet task request %s:%d", AscendNPUCore, reqNPUNum)
	}
	return nil
}

// CheckNodeNPUByPod check nod npu meet task req
func (ns *NPUDevices) CheckNodeNPUByPod(pod *v1.Pod) error {
	if ns == nil || pod == nil {
		return errors.New(ArgumentError)
	}
	taskRes, err := ns.GetPodResource(pod)
	if err != nil {
		return err
	}
	return ns.CheckNodeNPUByDyPod(pod, taskRes)
}

// CheckNodeNPUByDyPod check chip on node has enough resource, fault chips are not in list, unstable excluded
func (ns *NPUDevices) CheckNodeNPUByDyPod(pod *v1.Pod, taskResReq VResource) error {
	if ns == nil || pod == nil {
		klog.V(LogDebugLev).Infof("CheckNodeNPUByDyTask failed: %s", ArgumentError)
		return errors.New(ArgumentError)
	}
	klog.V(LogDebugLev).Infof("check dynamic vNPU %s on %s", pod.Name, ns.NodeInf.Name)
	if !ns.ValidVNode {
		klog.V(LogInfoLev).Infof("dynamic vNPU node<%s> not valid vNode", ns.NodeInf.Name)
		return errors.New("checkNodeNPUByDyTask invalid VNode")
	}
	if ns.IsNodeNotMeetRes(taskResReq) {
		// if node resource not enough, reduce task aiCPU
		if ns.taskAICPUCanBeDowngrade(taskResReq) {
			klog.V(LogInfoLev).Infof("dynamic vnpu task<%s> resource not enough, downgrade cpu", pod.Name)
			ns.DowngradeCache[pod.Name] = struct{}{}
			return ns.CheckNodeNPUByDyPod(pod, ns.downgradeTaskAICPU(taskResReq))
		}
	}
	if diffErr := ns.IsNodeHasDifferentUnFinishedTask(pod, taskResReq); diffErr != nil {
		return diffErr
	}
	klog.V(LogInfoLev).Infof("dynamic vnpu task<%s> CheckNodeNPUByDyTask node<%s> ok", pod.Name, ns.NodeInf.Name)
	return nil
}

// IsNodeNotMeetRes judge the node meet resource or not.
func (ns *NPUDevices) IsNodeNotMeetRes(podResReq VResource) bool {
	return !ns.isNodeTotalResEnough(podResReq) || !ns.isNodeChipResEnough(podResReq)
}

// isNodeTotalResEnough judge node total resource enough
func (ns *NPUDevices) isNodeTotalResEnough(vRes VResource) bool {
	var nodeResFree VResource
	for _, chip := range ns.Chips {
		if chip.Unstable {
			klog.V(LogDebugLev).Infof("chip <%s> unstable, resource exempted", chip.Name)
		}
		nodeResFree.Add(chip.FreeRes)
	}
	return nodeResFree.BeGreater(vRes)
}

// isNodeChipResEnough judge if chip on node can be allocated to job
func (ns *NPUDevices) isNodeChipResEnough(vRes VResource) bool {
	if ns.IsResourceWholeCard(vRes.Aicore) {
		return ns.isNodeChipResEnoughWholeCard(vRes)
	}
	for _, vChip := range ns.Chips {
		if !vChip.isChipMeetResReq(vRes) || vChip.Unstable {
			klog.V(LogDebugLev).Infof("vChip %s does not meet resource requirements", vChip.Name)
			continue
		}
		return true
	}
	return false
}

func (ns *NPUDevices) isNodeChipResEnoughWholeCard(vRes VResource) bool {
	if ns.AiCorePerChip == 0 {
		return false
	}
	freeWholeCard := 0
	for _, vChip := range ns.Chips {
		if vChip.SegmentFlag || vChip.FreeRes.Aicore == 0 {
			continue
		}
		freeWholeCard += 1
	}
	return vRes.Aicore/ns.AiCorePerChip <= freeWholeCard
}

// IsNodeHasDifferentUnFinishedTask judge the node wither has the different template unfinished job.
func (ns *NPUDevices) IsNodeHasDifferentUnFinishedTask(pod *v1.Pod, podResReq VResource) error {
	if ns == nil || pod == nil {
		klog.V(LogDebugLev).Infof("IsNodeHasDifferentUnFinishedTask failed :%s", ArgumentError)
		return errors.New(ArgumentError)
	}
	klog.V(LogDebugLev).Infof("%s IsNodeHasDifferentUnFinishedTask cache :%v", pod.Name, ns.ConCache)
	nodeTempMap := ns.ConCache
	if len(nodeTempMap) == 0 {
		klog.V(LogDebugLev).Infof("%s IsNodeHasDifferentUnFinishedTask cache no node %s, ok.",
			pod.Name, ns.NodeInf.Name)
		return nil
	}
	template, getErr := ns.GetTemplateByResReq(podResReq, ns.VT)
	if getErr != nil {
		klog.V(LogDebugLev).Infof("IsNodeHasDifferentUnFinishedTask %s", getErr)
		return getErr
	}
	if len(nodeTempMap) == 1 {
		_, tOK := nodeTempMap[template]
		if tOK {
			klog.V(LogDebugLev).Infof("%s IsNodeHasDifferentUnFinishedTask cache no template:%s, ok.",
				pod.Name, template)
			return nil
		}
	}

	return fmt.Errorf("%s is using %s, and not rewrite", pod.Name, ns.NodeInf.Name)
}

// taskAICPUCanBeDowngrade if task label is low, aicpu can be lower
func (ns *NPUDevices) taskAICPUCanBeDowngrade(podResReq VResource) bool {
	if podResReq.Aicore == NPUIndex2 && podResReq.Aicpu == NPUIndex2 {
		return true
	}
	if podResReq.Aicore == NPUIndex4 && podResReq.Aicpu == NPUIndex4 && podResReq.DVPP != AscendDVPPEnabledOn {
		return true
	}

	return false
}

// SetNPUTopologyToPodFn write chip to pod annotation AscendNPUCore
func (ns *NPUDevices) SetNPUTopologyToPodFn(kubeClient kubernetes.Interface, pod *v1.Pod, podResReq VResource, allocChipID string, chipVTemplate VTemplate) {
	if ns == nil || pod == nil {
		klog.V(LogDebugLev).Infof("SetNPUTopologyToPodFn failed: %s", ArgumentError)
		return
	}
	tmp := strconv.FormatInt(time.Now().UnixNano(), Base10)
	pod.Annotations[PodPredicateTime] = tmp
	// 1. whole card
	if ns.IsResourceWholeCard(podResReq.Aicore) {
		pod.Annotations[AscendNPUCore] = allocChipID

		patch := AddNPUAllocationPatch(allocChipID, "", tmp)
		_, err := kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			klog.V(LogErrorLev).Infof("patch pod %s failed: %v", pod.Name, err)
		}

		klog.V(LogInfoLev).Infof("dynamic vnpu setNPUTopologyToPod %s top:%s.", pod.Name, allocChipID)
		return
	}

	for curTemplate, jobVResource := range chipVTemplate.Data {
		if podResReq != jobVResource {
			continue
		}
		pod.Annotations[AscendNPUCore] = fmt.Sprintf("%s-%s", allocChipID, curTemplate)

		patch := AddNPUAllocationPatch(allocChipID, curTemplate, tmp)
		_, err := kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			klog.V(LogErrorLev).Infof("patch pod %s failed: %v", pod.Name, err)
		}

		klog.V(LogInfoLev).Infof("dynamic vnpu setNPUTopologyToPod %s top:%s.", pod.Name,
			pod.Annotations[AscendNPUCore])
		return
	}
}

// SelectChipFromNode get chip with least resource that meets vRes requirements
func (ns *NPUDevices) SelectChipFromNode(vRes VResource) (string, error) {
	if ns == nil {
		klog.V(LogDebugLev).Infof("SelectChipFromNode failed: %s", ArgumentError)
		return "", errors.New(ArgumentError)
	}
	var vChipSlice []*VChip
	for _, Chip := range ns.Chips {
		vChipSlice = append(vChipSlice, Chip)
	}

	tempVChips := vChipsList(vChipSlice)
	sort.Sort(tempVChips)
	if len(tempVChips) == 0 {
		return "", fmt.Errorf("selectChipFromNode sorted chips len 0")
	}

	if ns.IsResourceWholeCard(vRes.Aicore) {
		return ns.selectChipFromNodeWhole(tempVChips, vRes)
	}
	return ns.selectChipFromNodeSegment(tempVChips, vRes)
}

func (ns *NPUDevices) selectChipFromNodeWhole(vChips []*VChip, vRes VResource) (string, error) {
	if ns.AiCorePerChip == 0 {
		return "", errors.New("AiCorePerChip is zero, division by zero avoided")
	}
	reqCardNum := vRes.Aicore / ns.AiCorePerChip
	allocCardNum := 0
	if reqCardNum == 0 {
		klog.V(LogDebugLev).Infof("selectChipFromNodeWhole aiCore:%d perCard:%d", vRes.Aicore,
			ns.AiCorePerChip)
		return "", errors.New("task require card number 0")
	}
	vResChip := VResource{
		Aicore: vRes.Aicore / reqCardNum,
		Aicpu:  vRes.Aicpu / reqCardNum,
		DVPP:   AscendDVPPEnabledNull,
	}
	cardNames := make([]string, 0)
	for _, chip := range vChips {
		if !chip.isChipMeetResReq(vResChip) || chip.SegmentFlag {
			klog.V(LogDebugLev).Infof("chip %s does not meet whole card resource requirements", chip.Name)
			continue
		}
		chipID, err := getWholeCardIDFromAscendReal(chip.Name)
		if err != nil {
			return "", fmt.Errorf("selectChipFromNodeWhole chip name <%s> err: %s", chip.Name,
				SafePrint(err))
		}
		cardNames = append(cardNames, strconv.Itoa(chipID))
		allocCardNum += 1
		if allocCardNum == reqCardNum {
			return strings.Join(cardNames, ","), nil
		}
	}
	return "", fmt.Errorf("selectChipFromNodeWhole free whole chip <%d> not enough for req <%d>", allocCardNum,
		reqCardNum)
}

func (ns *NPUDevices) selectChipFromNodeSegment(vChip []*VChip, vRes VResource) (string, error) {
	sort.Sort(vChipsList(vChip))
	for _, chip := range vChip {
		if !chip.isChipMeetResReq(vRes) || chip.Unstable {
			klog.V(LogDebugLev).Infof("chip %s does not meet resource requirements", chip.Name)
			continue
		}
		chipID, err := getWholeCardIDFromAscendReal(chip.Name)
		if err != nil {
			return "", fmt.Errorf("selectChipFromNodeSegment chip name <%s> err: %s", chip.Name,
				SafePrint(err))
		}
		return strconv.Itoa(chipID), nil
	}

	return "", fmt.Errorf("selectChipFromNodeSegment available chip not found for req <%d>", vRes.Aicore)
}

func escapeJSONPointer(p string) string {
	p = strings.Replace(p, "~", "~0", -1)
	p = strings.Replace(p, "/", "~1", -1)
	return p
}

func AddNPUAllocationPatch(allocChipID string, template string, timestamp string) string {
	var allocValue string
	if template != "" {
		allocValue = fmt.Sprintf("%s-%s", allocChipID, template)
	} else {
		allocValue = allocChipID
	}

	return fmt.Sprintf(`[{"op": "add", "path": "/metadata/annotations/%s", "value":"%s"},`+
		`{"op": "add", "path": "/metadata/annotations/%s", "value": "%s"}]`,
		escapeJSONPointer(PodPredicateTime), timestamp,
		escapeJSONPointer(AscendNPUCore), allocValue)
}
