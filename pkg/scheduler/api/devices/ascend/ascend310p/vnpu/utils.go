package vnpu

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// ChangeTopToIntArray Change npu card ids from string to int array.
func ChangeTopToIntArray(topStr string, npuCardPreName string) []int {
	topInt := make([]int, 0)
	var cardStr string
	var topStrArray []string

	if topStr == "" {
		return []int{}
	}

	topStrArray = strings.Split(topStr, ",")
	for _, cardStr = range topStrArray {
		// cannot use strings 's Trim
		v := strings.TrimPrefix(cardStr, npuCardPreName)
		cardInt, err := strconv.Atoi(v)
		if err != nil {
			klog.V(LogErrorLev).Infof("ChangeTopToIntArray conv failed %v.", err)
			return nil
		}

		topInt = append(topInt, cardInt)
	}
	klog.V(LogDebugLev).Infof("ChangeTopToIntArray %v.", topInt)
	return topInt
}

// ChangeIntArrToStr Covert []int to string. Like [0,1] -> "Ascend910-0,Ascend910-1".
func ChangeIntArrToStr(top []int, npuCardPreName string) string {
	var tmp int
	var str string

	i := 0
	for i, tmp = range top {
		str += npuCardPreName + strconv.Itoa(tmp)
		if i+1 < len(top) {
			str += ","
		}
	}

	return str
}

// Max return the bigger one
func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// Min return the smaller one
func Min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// RemoveSliceDuplicateElement remove duplicate element in slice
func RemoveSliceDuplicateElement(languages []string) []string {
	result := make([]string, 0, len(languages))
	temp := map[string]struct{}{}
	for _, item := range languages {
		if _, ok := temp[item]; !ok {
			temp[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

// RemoveCommonElement remove common element from s1
func RemoveCommonElement(s1, s2 []int) []int {
	res := make([]int, 0)
	for _, e1 := range s1 {
		existFlag := false
		for _, e2 := range s2 {
			if e1 == e2 {
				existFlag = true
				break
			}
		}
		if !existFlag {
			res = append(res, e1)
		}
	}
	return res
}

// IsMapHasNPUResource Determines whether a target string exists in the map.
func IsMapHasNPUResource(resMap map[v1.ResourceName]float64, npuName string) bool {
	for k := range resMap {
		// must contain "huawei.com"
		if strings.Contains(string(k), npuName) {
			return true
		}
	}
	return false
}

// SafePrint safe print error
func SafePrint(args ...interface{}) string {
	msg := fmt.Sprint(args...)
	trimMsg := strings.Replace(msg, "\r", " ", -1)
	trimMsg = strings.Replace(trimMsg, "\n", " ", -1)
	return trimMsg
}

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

// IsSliceContain judges whether keyword in targetSlice
func IsSliceContain(keyword interface{}, targetSlice interface{}) bool {
	if targetSlice == nil {
		klog.V(LogErrorLev).Infof("IsSliceContain :%s", ArgumentError)
		return false
	}
	kind := reflect.TypeOf(targetSlice).Kind()
	if kind != reflect.Slice && kind != reflect.Array {
		klog.V(LogErrorLev).Infof(
			"the input %#v of type %T isn't a slice or array", targetSlice, targetSlice)
		return false
	}

	v := reflect.ValueOf(targetSlice)
	m := make(map[interface{}]struct{}, v.Len())
	for j := 0; j < v.Len(); j++ {
		m[v.Index(j).Interface()] = struct{}{}
	}

	_, ok := m[keyword]
	return ok
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

// ConvertErrSliceToError convert []error to one error.
func ConvertErrSliceToError(reErrors []error) error {
	var reE error

	for _, value := range reErrors {
		if reE == nil {
			reE = value
			continue
		}
		reE = fmt.Errorf("%s %s", reE, value)
	}

	return reE
}

// GetNpuNameFromJobRequire get npuName,if job require name is npu-core return huawei.com/Ascend310P
func GetNpuNameFromJobRequire(npuName string) string {
	if npuName == AscendNPUCore {
		return NPU310PCardName
	}
	return npuName
}

// CheckStrInSlice return whether str in string slice
func CheckStrInSlice(str string, slice []string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

// IsNodeReady returns the node ready status
func IsNodeReady(node *v1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == v1.NodeReady {
			return cond.Status == v1.ConditionTrue
		}
	}
	return false
}

// MakeDataHash check code for configmap
func MakeDataHash(data interface{}) string {
	var dataBuffer []byte
	if dataBuffer = marshalData(data); len(dataBuffer) == 0 {
		return ""
	}
	h := sha256.New()
	if _, err := h.Write(dataBuffer); err != nil {
		klog.V(LogErrorLev).Info("hash data error")
		return ""
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func marshalData(data interface{}) []byte {
	dataBuffer, err := json.Marshal(data)
	if err != nil {
		klog.V(LogErrorLev).Infof("marshal data err: %s", SafePrint(err))
		return nil
	}
	return dataBuffer
}

// SortByNumericValue sort string
func SortByNumericValue(s []string) {
	sort.Slice(s, func(i, j int) bool {
		num1, err1 := strconv.Atoi(s[i])
		if err1 != nil {
			klog.V(LogErrorLev).Infof("Atoi data err: %s", SafePrint(err1))
			return false
		}
		num2, err2 := strconv.Atoi(s[j])
		if err2 != nil {
			klog.V(LogErrorLev).Infof("Atoi data err: %s", SafePrint(err2))
			return true
		}
		return num1 >= num2
	})
}

// PtrInit return base type ptr
func PtrInit[T any](v T) *T { return &v }

// GetDeviceType get device type from dev list
func GetDeviceType(devList map[string]string) string {
	for key := range devList {
		if strings.Contains(key, Ascend910) {
			return Ascend910
		}
		if strings.Contains(key, Ascend310P) {
			return Ascend310P
		}
		if strings.Contains(key, Ascend310) {
			return Ascend310
		}
	}
	klog.V(LogDebugLev).Info("cannot decide device type from dev list")
	return Ascend910
}

// GetUnhealthyDevInfo get unhealthy device info from device list
func GetUnhealthyDevInfo(devList map[string]string) (string, []string) {
	unHealthyKey := HwPreName + GetDeviceType(devList) + "-Unhealthy"
	klog.V(LogDebugLev).Infof("unhealthy device key: %s", unHealthyKey)
	unHealthyDevStr := devList[unHealthyKey]
	if len(unHealthyDevStr) == 0 {
		return unHealthyKey, []string{}
	}
	unHealthyDevList := strings.Split(unHealthyDevStr, ",")
	klog.V(LogDebugLev).Infof("unhealthy device list: %v", unHealthyDevList)
	return unHealthyKey, unHealthyDevList
}

// GetAvailableDevInfo get available device info from device list
func GetAvailableDevInfo(devList map[string]string) (string, []string) {
	availDevKey := HwPreName + GetDeviceType(devList)
	klog.V(LogDebugLev).Infof("available device key: %s", availDevKey)
	availDevStr := devList[availDevKey]
	if len(availDevStr) == 0 {
		return availDevKey, []string{}
	}
	availDevList := strings.Split(availDevStr, ",")
	klog.V(LogDebugLev).Infof("available device list: %v", availDevList)
	return availDevKey, availDevList
}

// GetRecoveringDevInfo get recovering device info from device list
func GetRecoveringDevInfo(devList map[string]string) (string, []string) {
	recoveringKey := HwPreName + GetDeviceType(devList) + "-Recovering"
	klog.V(LogDebugLev).Infof("recovering device key: %s", recoveringKey)
	recoveringDevStr := devList[recoveringKey]
	if len(recoveringDevStr) == 0 {
		return recoveringKey, []string{}
	}
	recoveringDevList := strings.Split(recoveringDevStr, ",")
	klog.V(LogDebugLev).Infof("recovering device list: %v", recoveringDevList)
	return recoveringKey, recoveringDevList
}
