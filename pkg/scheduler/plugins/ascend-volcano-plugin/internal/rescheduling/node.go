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
	"fmt"
	"strings"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// createFaultCardHandlers initialise FaultCard struct == getInoperableNPUCards
func (fNode *FaultNode) createFaultCardHandlers(node *plugin.NPUNode) []FaultCard {
	klog.V(util.LogInfoLev).Infof("create new fault card handlers for node %s", node.Name)
	faultCards := make([]FaultCard, 0)
	allCards, err := fNode.getAllNPUCardsFromDeviceInfo(node)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("get all fault card info for node %s, err %v", node.Name, err.Error())
		return faultCards
	}
	for _, card := range allCards {
		faultCard := FaultCard{
			IsFaultCard: false,
			NPUName:     card,
			FaultType:   CardHealthy,
		}

		if faultCard.isCardUnhealthy(fNode.UnhealthyNPU) {
			klog.V(util.LogDebugLev).Infof("card %s is unhealthy", faultCard.NPUName)
			faultCard.setIsFaultCard(true)
			faultCard.setFaultType(CardUnhealthy)
			faultCards = append(faultCards, faultCard)
			continue
		}
		if faultCard.isCardNetworkUnhealthy(fNode.NetworkUnhealthyNPU) {
			klog.V(util.LogDebugLev).Infof("card %s is network unhealthy", faultCard.NPUName)
			faultCard.setIsFaultCard(true)
			faultCard.setFaultType(CardNetworkUnhealthy)
			faultCards = append(faultCards, faultCard)
			continue
		}
		faultCards = append(faultCards, faultCard)
	}

	return faultCards
}

// getNodeNPUsByKey get the npu list from node.DeviceInfo
func (fNode *FaultNode) getNodeNPUsByKey(node *plugin.NPUNode, deviceKey string) ([]string, error) {
	npuStr, ok := node.Annotation[deviceKey]
	if !ok {
		return nil, fmt.Errorf("%s device info don't have key %s", node.Name, deviceKey)
	}
	if len(npuStr) == 0 {
		return nil, nil
	}
	npus := strings.Split(npuStr, ",")

	return npus, nil
}

// getAllNPUCardsFromDeviceInfo get un-allocated healthy card from device info
func (fNode *FaultNode) getAllNPUCardsFromDeviceInfo(node *plugin.NPUNode) ([]string, error) {
	var allCard []string
	healthyCard, err := fNode.getNodeNPUsByKey(node, fNode.NPUName) // ["Ascend910-0", ...]
	allCard = append(allCard, healthyCard...)
	allCard = append(allCard, fNode.UnhealthyNPU...)
	allCard = append(allCard, fNode.NetworkUnhealthyNPU...)
	allCard = util.RemoveSliceDuplicateElement(allCard)
	if err != nil {
		return allCard, err
	}
	return allCard, nil
}

// getUnhealthyCardsFromDeviceInfo get unhealthyCard from device info
func (fNode *FaultNode) getUnhealthyCardsFromDeviceInfo(node *plugin.NPUNode) ([]string, error) {
	unhealthyCardName := fmt.Sprintf("%s-%s", fNode.NPUName, CardUnhealthy) // ["Ascend910-1"]
	return fNode.getNodeNPUsByKey(node, unhealthyCardName)
}

// getNetworkUnhealthyCardsFromDeviceInfo get networkUnhealthyCard from device info
func (fNode *FaultNode) getNetworkUnhealthyCardsFromDeviceInfo(node *plugin.NPUNode) ([]string, error) {
	networkUnhealthyCardName := fmt.Sprintf("%s-%s", fNode.NPUName, CardNetworkUnhealthy) // ["Ascend910-1"]
	return fNode.getNodeNPUsByKey(node, networkUnhealthyCardName)
}

func (fCard *FaultCard) isCardUnhealthy(unHealthyList []string) bool {
	return util.IsSliceContain(fCard.NPUName, unHealthyList)
}

func (fCard *FaultCard) isCardNetworkUnhealthy(networkUnhealthyList []string) bool {
	return util.IsSliceContain(fCard.NPUName, networkUnhealthyList)
}

func (fNode *FaultNode) updateFaultNodesFromDeviceInfo(node *plugin.NPUNode) {
	klog.V(util.LogInfoLev).Infof("update information from device info for node %s", node.Name)
	tmpUnhealthyNPUs, err := fNode.getUnhealthyCardsFromDeviceInfo(node)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("getUnhealthyCardsFromDeviceInfo: %s", util.SafePrint(err))
	}
	fNode.setUnhealthyNPUList(tmpUnhealthyNPUs)
	klog.V(util.LogInfoLev).Infof("Unhealthy cards from device info: %v", tmpUnhealthyNPUs)

	tmpNetworkUnhealthyNPUs, err := fNode.getNetworkUnhealthyCardsFromDeviceInfo(node)
	if err != nil {
		klog.V(util.LogInfoLev).Infof("getNetworkUnhealthyCardsFromDeviceInfo: %s", util.SafePrint(err))
	}
	fNode.setNetworkUnhealthyNPUList(tmpNetworkUnhealthyNPUs)
	klog.V(util.LogInfoLev).Infof("Network unhealthy cards from device info: %v", tmpUnhealthyNPUs)

	DeviceFaultReason, err := GetNodeDeviceFaultFromDeviceInfo(node)
	if err != nil {
		klog.V(util.LogDebugLev).Infof("GetNodeDeviceFaultFromDeviceInfo: %s", util.SafePrint(err))
	}
	fNode.setFaultDeviceList(DeviceFaultReason)
	fNode.setNodeHasCardSubHealthFault()
}

// GetNodeDeviceFaultFromDeviceInfo get device fault from device info
func GetNodeDeviceFaultFromDeviceInfo(node *plugin.NPUNode) ([]FaultDeviceList, error) {
	deviceFaultList, ok := node.Annotation[DeviceFaultCmKey]
	if !ok {
		return nil, fmt.Errorf("getNodeDeviceFaultFromDeviceInfo failed")
	}
	var deviceFault []FaultDeviceList
	if unmarshalErr := json.Unmarshal([]byte(deviceFaultList), &deviceFault); unmarshalErr != nil {
		klog.V(util.LogWarningLev).Infof("convertToDeviceFaultListFromCM Unmarshal: %s.", util.SafePrint(unmarshalErr))
		return nil, unmarshalErr
	}
	return deviceFault, nil
}

// updateFaultNodesAttr update Information from device Info
func (fNode *FaultNode) updateFaultNodesAttr(node *plugin.NPUNode) {
	klog.V(util.LogInfoLev).Infof("Update node %s attributes", node.Name)
	// 1. create fault Card Object
	tmpFaultCards := fNode.createFaultCardHandlers(node)
	fNode.setFaultCards(tmpFaultCards)

	fNode.setNodeHealthStateValue(NodeHealthy)
	fNode.setIsFaultNodeValue(false)

	// 2. judge if node is unhealthy by NodeD
	fNode.setNodeHealthyByNodeD(node)
	// 3. judge if node is unhealthy by switch info
	fNode.setNodeHealthyBySwitch(node)

	if fNode.NodeHealthState == NodeUnhealthy {
		return
	}

	fNode.setHasSwitchSubHealthFault(node.Annotation[util.SwitchNodeHealtyStatuskey] == util.NodeSubHealthy)
	// 4. set node health state by card unhealthy
	fNode.setNodeHealthyByCardHealth(node)
}

func (fNode *FaultNode) setNodeHealthyByNodeD(node *plugin.NPUNode) {
	if !fNode.isNodeDEnabled(node) {
		klog.V(util.LogDebugLev).Infof("node %s nodeD not enabled", node.Name)
		fNode.setNodeDValue(false)
		return
	}
	fNode.setNodeDValue(true)
	// 2. to judge if noded has reported node unhealthy
	healthyStatus, ok := node.Annotation[util.NodedNodeHealtyStatuskey]
	if !ok {
		// 2 reason:
		// if haven't got the healthy status reported by noded, will not set node status to unhealthy;
		// when there are no faults on the node, node info cm does not exist.
		klog.V(util.LogInfoLev).Infof("failed to obtain node[%s] healthy status from noded configmap", node.Name)
		return
	}
	if healthyStatus == util.NodeUnHealthyByNodeD {
		fNode.setIsFaultNodeValue(true)
		fNode.setNodeHealthStateValue(NodeUnhealthy)
		klog.V(util.LogInfoLev).Infof("Node[%s] has received unhealthy status from noded", node.Name)
	}
}

func (fNode *FaultNode) setNodeHealthyBySwitch(node *plugin.NPUNode) {
	// 1. to judge if switch has reported node unhealthy
	healthyStatus, ok := node.Annotation[util.SwitchNodeHealtyStatuskey]
	if !ok || healthyStatus != util.NodeUnHealthyByNodeD {
		// if haven't got the healthy status reported by switch, will not set node status to unhealthy
		return
	}
	if !fNode.IsFaultNode {
		klog.V(util.LogInfoLev).Infof("Node[%s] has received unhealthy status from switch", node.Name)
	}
	fNode.setIsFaultNodeValue(true)
	fNode.setNodeHealthStateValue(NodeUnhealthy)
}

func (fNode *FaultNode) setNodeHealthyByCardHealth(node *plugin.NPUNode) {
	for _, card := range fNode.FaultCards {
		if !card.IsFaultCard {
			continue
		}
		fNode.setIsFaultNodeValue(true)
		switch card.FaultType {
		case CardUnhealthy:
			fNode.setNodeHealthStateValue(NodeCardUnhealthy)
			klog.V(util.LogInfoLev).Infof("Node %s health state set to %s", node.Name, NodeCardUnhealthy)
		case CardNetworkUnhealthy:
			fNode.setNodeHealthStateValue(NodeCardNetworkUnhealthy)
			klog.V(util.LogInfoLev).Infof("Node %s health state set to %s", node.Name, NodeCardNetworkUnhealthy)
		default:
			klog.V(util.LogInfoLev).Infof("card health state %s illegal", card.FaultType)
		}
	}
}

func (fNode *FaultNode) isNodeDEnabled(node *plugin.NPUNode) bool {
	value, ok := node.Label[nodeDEnableKey]
	if !ok {
		return false
	}

	switch value {
	case nodeDEnableOnValue:
		return true
	case nodeDEnableOffValue:
		return false
	default:
		klog.V(util.LogErrorLev).Infof("isEnableFaultNode not support %s.", value)
		return false
	}
}

// getNodeNPUsByKey get the npu list from node.DeviceInfo
func (fNode *FaultNode) getL1LinkDownCards() []string {
	var cards []string
	for _, fault := range fNode.FaultDeviceList {
		if fault.FaultCode == linkDownFaultCode && fault.FaultHandling == NotHandleFault {
			cards = append(cards, fault.NPUName)
		}
	}
	return cards
}

func (fNode *FaultNode) setNodeDValue(value bool) {
	fNode.NodeDEnable = value
}

func (fNode *FaultNode) setIsFaultNodeValue(value bool) {
	fNode.IsFaultNode = value
}

func (fNode *FaultNode) setHasSwitchSubHealthFault(isSubHealthy bool) {
	fNode.HasSwitchSubHealthFault = isSubHealthy
}

func (fNode *FaultNode) setNodeHealthStateValue(nodeHealthState string) {
	fNode.NodeHealthState = nodeHealthState
}

func (fNode *FaultNode) setUnhealthyNPUList(value []string) {
	fNode.UnhealthyNPU = value
}

func (fNode *FaultNode) setNetworkUnhealthyNPUList(value []string) {
	fNode.NetworkUnhealthyNPU = value
}

func (fNode *FaultNode) setFaultCards(value []FaultCard) {
	fNode.FaultCards = value
}

func (fCard *FaultCard) setFaultType(value string) {
	fCard.FaultType = value
}

func (fCard *FaultCard) setIsFaultCard(value bool) {
	fCard.IsFaultCard = value
}

func (fNode *FaultNode) setFaultDeviceList(value []FaultDeviceList) {
	fNode.FaultDeviceList = value
}

func (fNode *FaultNode) setNodeHasCardSubHealthFault() {
	for _, faultCode := range fNode.FaultDeviceList {
		if faultCode.FaultHandling == SubHealthFault {
			fNode.HasCardSubHealthFault = true
			return
		}
	}
}

func newFaultNodeDefault(nodeName string, updateTime int64) *FaultNode {
	faultNode := &FaultNode{
		NodeName:            nodeName,
		UpdateTime:          updateTime,
		UnhealthyNPU:        nil,
		NetworkUnhealthyNPU: nil,
		IsFaultNode:         false,
		NodeDEnable:         false,
		NodeHealthState:     NodeHealthy,
		FaultCards:          nil,
		FaultDeviceList:     []FaultDeviceList{},
	}
	return faultNode
}
