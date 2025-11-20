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
Package util is using for the total variable.
*/
package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api"
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
func (vResource VResource) BeGreater(resource VResource) bool {
	return vResource.Aicore >= resource.Aicore && vResource.Aicpu >= resource.Aicpu
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

// SafePrint safe print error
func SafePrint(args ...interface{}) string {
	msg := fmt.Sprint(args...)
	trimMsg := strings.Replace(msg, "\r", " ", -1)
	trimMsg = strings.Replace(trimMsg, "\n", " ", -1)
	return trimMsg
}

// ChangeNodesToNodeMaps change nodes slice into node maps
func ChangeNodesToNodeMaps(nodes []*api.NodeInfo) map[string]*api.NodeInfo {
	if len(nodes) == 0 {
		return nil
	}
	tmpNodes := make(map[string]*api.NodeInfo, len(nodes))
	for _, node := range nodes {
		tmpNodes[node.Name] = node
	}
	return tmpNodes
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

// GetNodeDevListFromAnno get node device list from annotation
func GetNodeDevListFromAnno(nodeInfo *api.NodeInfo) ([]string, error) {
	baseDevInfo, ok := nodeInfo.Node.Annotations[BaseDeviceInfoKey]
	if !ok {
		klog.V(LogErrorLev).Infof("node annotation[%s] does not exist", BaseDeviceInfoKey)
		return nil, fmt.Errorf("node annotation[%s] does not exist", BaseDeviceInfoKey)
	}
	devIpMap := make(map[string]NpuBaseInfo)
	if err := json.Unmarshal([]byte(baseDevInfo), &devIpMap); err != nil {
		klog.V(LogErrorLev).Infof("unmarshal node device list failed, error: %v", err)
		return nil, errors.New("unmarshal node device list failed")
	}
	var nodeDevList = make([]string, 0)
	for devName := range devIpMap {
		splitDev := strings.Split(devName, "-")
		// remove vnpu card
		if len(splitDev) > DevSplitNum {
			continue
		}
		nodeDevList = append(nodeDevList, devName)
	}
	return nodeDevList, nil
}

// GetActivePodUsedDevFromNode get active pod used device from node
func GetActivePodUsedDevFromNode(nodeInfo *api.NodeInfo, devType string) []string {
	var usedDev = make([]string, 0)
	for _, pod := range nodeInfo.Pods() {
		if pod.Status.Phase == v1.PodFailed || pod.Status.Phase == v1.PodSucceeded {
			continue
		}
		if err := CheckPodNameOrSpace(PodName, pod.GetName(), PodNameMaxLength); err != nil {
			klog.V(LogErrorLev).Infof("pod name is illegal, error: %v", err)
			continue
		}
		if err := CheckPodNameOrSpace(Namespace, pod.GetNamespace(), PodNameSpaceMaxLength); err != nil {
			klog.V(LogErrorLev).Infof("pod namespace is illegal, error: %v", err)
			continue
		}
		tmpDev, ok := pod.Annotations[fmt.Sprintf("%s%s", HwPreName, devType)]
		if !ok || len(tmpDev) == 0 || len(tmpDev) > PodAnnotationMaxLength {
			continue
		}
		tmpDevList := strings.Split(tmpDev, ",")
		if len(tmpDevList) == 0 || len(tmpDevList) > MaxDevicesNum {
			klog.V(LogErrorLev).Info("invalid device list length from annotation")
			continue
		}
		usedDev = append(usedDev, tmpDevList...)
	}
	klog.V(LogDebugLev).Infof("nodeName: %s, usedDev: %v", nodeInfo.Name, usedDev)
	return usedDev
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

// CheckPodNameOrSpace check pod name or pod namespace
func CheckPodNameOrSpace(checkItem, podParam string, maxLength int) error {
	if len(podParam) > maxLength {
		return fmt.Errorf("length %d is bigger than %d", len(podParam), maxLength)
	}

	pattern, ok := podRegexp[checkItem]
	if !ok {
		return errors.New("invalid check item")
	}
	if match := pattern.MatchString(podParam); !match {
		return errors.New("does not meet regex")
	}
	return nil
}

// IsNPUTask to judge the task either is NPU task or not.
func IsNPUTask(nT *api.TaskInfo) bool {
	if nT == nil || nT.Resreq == nil {
		return false
	}
	for k := range nT.Resreq.ScalarResources {
		// must contain "huawei.com/"
		if strings.Contains(string(k), HwPreName) {
			return true
		}
	}
	return false
}

// IsStrategyInSubHealthyStrategs to judge the subHealthyStrategy is in subHealthyStrategs or not.
func IsStrategyInSubHealthyStrategs(subHealthyStrategy string) bool {
	return CheckStrInSlice(subHealthyStrategy, subHealthyStrategs)
}
