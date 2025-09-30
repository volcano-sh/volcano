/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.
*/

/*
Package plugin is using for HuaWei Ascend pin affinity schedule.
*/
package plugin

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

// getResourceFromTemplate nodeType like Ascend310P, templateString like "vir04_3c_ndvpp"
func getResourceFromTemplate(nodeType string, templateString string,
	taskTemplate map[string]map[string]util.VResource) *util.VResource {
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

// isPodWholeCardFromAscendReal judge if card is whole card Ascend310P-0/Ascend310P-1c-400-3_0
func isPodWholeCardFromAscendReal(realCardName string) bool {
	temp := strings.Split(realCardName, "-")
	if len(temp) == util.NPUIndex2 {
		return true
	}
	return false
}

// GetPhysicCardNameFromVChip get cardName from whole Ascend310P-0/Ascend310P-1c-400-3_0
func GetPhysicCardNameFromVChip(realCardName string) string {
	if isPodWholeCardFromAscendReal(realCardName) {
		return realCardName
	}
	temp := strings.Split(realCardName, "-")
	if len(temp) < util.NPUIndex4 {
		return ""
	}
	cardType := temp[0]               // like Ascend310P
	cardIDStr := temp[util.NPUIndex3] // like 3_0
	cardIDSplit := strings.Split(cardIDStr, "_")
	if len(cardIDSplit) < util.NPUIndex2 {
		return ""
	}
	cardID := cardIDSplit[0]
	klog.V(util.LogDebugLev).Infof("GetPhysicCardNameFromVChip %s", fmt.Sprintf("%s-%s", cardType, cardID))
	return fmt.Sprintf("%s-%s", cardType, cardID)
}

// getWholeCardIDFromAscendReal get card physics id from Ascend910-0
func getWholeCardIDFromAscendReal(cardNameStr string) (int, error) {
	idStr := strings.Split(cardNameStr, "-")
	if len(idStr) < util.NPUIndex2 {
		return util.ErrorInt, fmt.Errorf("getCardIDFromCardNameStr %s %s", cardNameStr, FormatIncorrectError)
	}
	id, err := strconv.Atoi(idStr[util.NPUIndex1])
	if err != nil {
		return util.ErrorInt, fmt.Errorf("getCardIDFromCardNameStr %s %v", cardNameStr, err)
	}
	return id, nil
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
