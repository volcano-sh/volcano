package vnpu

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

func checkVNPUResourcesInPod(pod *v1.Pod) bool {
	if pod.Labels["ring-controller.atlas"] != "ascend-310P" {
		return false
	}
	for _, container := range pod.Spec.Containers {
		_, ok := container.Resources.Limits["huawei.com/npu-core"]
		if ok {
			return true
		}
	}
	return false
}

// getWholeCardIDFromAscendReal get card physics id from Ascend910-0
func getWholeCardIDFromAscendReal(cardNameStr string) (int, error) {
	idStr := strings.Split(cardNameStr, "-")
	if len(idStr) < NPUIndex2 {
		return ErrorInt, fmt.Errorf("getCardIDFromCardNameStr %s %s", cardNameStr, FormatIncorrectError)
	}
	id, err := strconv.Atoi(idStr[NPUIndex1])
	if err != nil {
		return ErrorInt, fmt.Errorf("getCardIDFromCardNameStr %s %v", cardNameStr, err)
	}
	return id, nil
}

// getResTemplateFromTaskSetting get like vir04_3c_ndvpp
func getResTemplateFromTaskSetting(coreNum int, cpuLevel, dvpp string) string {
	var virTemplate string
	switch coreNum {
	case NPUIndex1:
		virTemplate = VNPUTempVir01
	case NPUIndex2:
		virTemplate = VNPUTempVir02
		if cpuLevel == AscendVNPULevelLow {
			virTemplate = virTemplate + "_1c"
		}
	case NPUIndex4:
		virTemplate = getVirTemplate(dvpp, cpuLevel)
	default:
		klog.V(LogErrorLev).Infof("wrong number %d", coreNum)
		return ""
	}
	return virTemplate
}

func getVirTemplate(dvpp string, cpuLevel string) string {
	switch dvpp {
	case AscendDVPPEnabledOn:
		return VNPUTempVir04C4cDVPP
	case AscendDVPPEnabledOff:
		return VNPUTempVir04C3NDVPP
	default:
		virTemplate := VNPUTempVir04
		if cpuLevel == AscendVNPULevelLow {
			virTemplate = virTemplate + "_3c"
		}
		return virTemplate
	}
}

// Add add resource
func (vResource *VResource) Add(resource VResource) {
	vResource.Aicore += resource.Aicore
	vResource.Aicpu += resource.Aicpu
}

// Sub sub resource
func (vResource *VResource) Sub(resource VResource) {
	vResource.Aicore -= resource.Aicore
	vResource.Aicpu -= resource.Aicpu
}

// BeGreater judge resource greater or equal to
func (vResource *VResource) BeGreater(resource VResource) bool {
	return vResource.Aicore >= resource.Aicore && vResource.Aicpu >= resource.Aicpu
}

// UpdateDVPP update dvpp according to pod resource
func (vChip *VChip) UpdateDVPP(podResDVPP string) {
	if vChip == nil {
		klog.V(LogDebugLev).Infof("UpdateDVPP failed: %s", ArgumentError)
		return
	}
	if podResDVPP == AscendDVPPEnabledOn {
		vChip.UsedRes.DVPP = AscendDVPPEnabledOn
		vChip.FreeRes.DVPP = AscendDVPPEnabledOff
	}
	if podResDVPP == AscendDVPPEnabledOff && vChip.UsedRes.DVPP == AscendDVPPEnabledOff {
		vChip.UsedRes.DVPP = AscendDVPPEnabledOff
		vChip.FreeRes.DVPP = AscendDVPPEnabledOn
	}
	if podResDVPP == AscendDVPPEnabledNull && vChip.UsedRes.DVPP != AscendDVPPEnabledOn {
		vChip.UsedRes.DVPP = AscendDVPPEnabledNull
		vChip.FreeRes.DVPP = AscendDVPPEnabledNull
	}
}

// ResetDVPP update dvpp according to pod resource
func (vChip *VChip) ResetDVPP(podResDVPP string) {
	if vChip == nil {
		klog.V(LogDebugLev).Infof("UpdateDVPP failed: %s", ArgumentError)
		return
	}
	if podResDVPP == AscendDVPPEnabledOn {
		vChip.UsedRes.DVPP = AscendDVPPEnabledOff
		vChip.FreeRes.DVPP = AscendDVPPEnabledOn
	}
}

// isChipMeetResReq check chip resource can be allocated as the task requires
func (vChip *VChip) isChipMeetResReq(vRes VResource) bool {
	if vChip == nil {
		klog.V(LogDebugLev).Infof("isChipMeetResReq failed: %s", ArgumentError)
		return false
	}
	if !vChip.isChipResourceEnough(vRes) {
		klog.V(LogDebugLev).Infof("vChip %s resource <%#v> not enough", vChip.Name, vChip.FreeRes)
		return false
	}
	if !vChip.isChipVGroupValid(vRes) {
		klog.V(LogDebugLev).Infof("vChip %s vGroup not enough", vChip.Name)
		return false
	}
	if !vChip.isChipDVPPValid(vRes) {
		klog.V(LogDebugLev).Infof("vChip %s DVPP not enough", vChip.Name)
		return false
	}
	return true
}

// Len for order.
func (vChips vChipsList) Len() int {
	return len(vChips)
}

// Less for order.
func (vChips vChipsList) Less(i, j int) bool {
	if i > vChips.Len() || j > vChips.Len() {
		return false
	}
	return !vChips[i].FreeRes.BeGreater(vChips[j].FreeRes)
}

// Swap for order.
func (vChips vChipsList) Swap(i, j int) {
	if i > vChips.Len() || j > vChips.Len() {
		return
	}
	vChips[i], vChips[j] = vChips[j], vChips[i]
}

// isChipResourceEnough check if core is enough for task
func (vChip *VChip) isChipResourceEnough(vRes VResource) bool {
	return vChip.FreeRes.BeGreater(vRes)
}

// isChipVGroupValid check if vGroup is valid
func (vChip *VChip) isChipVGroupValid(vRes VResource) bool {
	if vChip.Kind != Ascend310P {
		klog.V(LogDebugLev).Infof("not %s task, no need to check vGroup", Ascend310P)
		return true
	}

	if !vChip.SegmentFlag {
		klog.V(LogDebugLev).Info("whole card, no need to check vGroup")
		return true
	}

	vGroups := vChip.getVGroups()

	if len(vGroups) == NPUIndex3 && vRes.Aicore >= NPUIndex4 {
		klog.V(LogDebugLev).Infof("%d vGroups, only support 1,2 core", len(vGroups))
		return false
	}

	if len(vGroups) == NPUIndex4 && vRes.Aicore >= NPUIndex2 {
		klog.V(LogDebugLev).Infof("%d vGroups, only support 1 core", len(vGroups))
		return false
	}

	return true
}

func (vChip *VChip) isChipDVPPValid(vRes VResource) bool {
	// 1. if task dvpp on, the node's free resource must support dvpp
	if vRes.DVPP == AscendDVPPEnabledOn && vChip.FreeRes.DVPP != AscendDVPPEnabledOn {
		return false
	}
	// 2. if task dvpp null, the node's free resource cannot be off
	if vRes.DVPP == AscendDVPPEnabledNull && vChip.FreeRes.DVPP == AscendDVPPEnabledOff {
		return false
	}
	// 3. if task dvpp no, the node's free resource can be any
	return true
}

// realChip like:Ascend310P-2c.1cpu-105-0_3.
func (vChip *VChip) getVGroups() []int {
	vGroups := make([]int, 0)
	for _, realChip := range vChip.ID {
		realChipSplit := strings.Split(realChip, "_")
		if len(realChipSplit) < NPUIndex2 {
			continue
		}
		vGroupStr := realChipSplit[len(realChipSplit)-1]
		vGroup, err := strconv.Atoi(vGroupStr)
		if err != nil {
			continue
		}
		var existFlag bool
		for _, v := range vGroups {
			if vGroup == v {
				existFlag = true
				break
			}
		}
		if !existFlag {
			vGroups = append(vGroups, vGroup)
		}
	}

	return vGroups
}

func (vChip *VChip) SetIsDual(value bool) {
	vChip.IsDual = value
}

// IsPodResUnstable return true if chip stable
func (vChip *VChip) IsPodResUnstable(pod *v1.Pod) bool {
	realStr, ok := pod.Annotations[AscendNPUPodRealUse]
	return !ok || realStr == ""
}

func (vChip *VChip) AddRealCardID(id string) {
	if id == "" {
		return
	}
	vChip.ID = append(vChip.ID, id)
}

func (vChip *VChip) AddPodToPodMap(pod *v1.Pod) {
	vChip.PodMap[string(pod.UID)] = pod
}

func (vChip *VChip) SetSegmentFlag(value bool) {
	vChip.SegmentFlag = value
}

// PtrInit return base type ptr
func PtrInit[T any](v T) *T { return &v }

// SafePrint safe print error
func SafePrint(args ...interface{}) string {
	msg := fmt.Sprint(args...)
	trimMsg := strings.Replace(msg, "\r", " ", -1)
	trimMsg = strings.Replace(trimMsg, "\n", " ", -1)
	return trimMsg
}
