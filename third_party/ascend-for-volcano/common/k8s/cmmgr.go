/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package k8s is using for the k8s operation.
*/
package k8s

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
)

func init() {
	cmManager = NewClusterInfoWitchCm()
}

var (
	cmManager               ClusterInfoWitchCm
	stopCh                  = make(chan struct{})
	lastInformerRestartTime int64
	needStartInformer       = true
	informerStartTimes      int
)

// InitCmInformer init cm informer, support cluster info manager and device plugin
func InitCmInformer(k8sClient kubernetes.Interface, useClusterD bool) {
	if k8sClient == nil {
		klog.V(util.LogErrorLev).Info("kube client is nil")
		return
	}
	if !needStartInformer {
		klog.V(util.LogDebugLev).Info("node need to start informer will return")
		return
	}
	if time.Now().Unix()-lastInformerRestartTime < minRestartInterval && informerStartTimes != util.NPUIndex1 {
		klog.V(util.LogDebugLev).Infof("time after last informer restart is <%v> less than 10 min",
			time.Now().Unix()-lastInformerRestartTime)
		return
	}
	if stopCh != nil {
		close(stopCh)
	}
	stopCh = make(chan struct{})
	klog.V(util.LogWarningLev).Infof("start informer at <%v>, the <%v> times start informer ",
		time.Now().Unix(), informerStartTimes+1)
	defer func() {
		lastInformerRestartTime = time.Now().Unix()
		needStartInformer = false
		informerStartTimes++
	}()
	if useClusterD {
		cmManager.initClusterCmInformer(k8sClient, stopCh)
		return
	}
	cmManager.initDeviceAndNodeDCmInformer(k8sClient, stopCh)
}

func getDataFromCM[T any](cmData *v1.ConfigMap, key string) (T, error) {
	var result T
	data, ok := cmData.Data[key]
	if !ok {
		return result, fmt.Errorf("configmap<%s> has no %s", cmData.Name, key)
	}
	// if there is no fault, the cm content may be empty
	if len(data) == 0 {
		return result, nil
	}
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		klog.V(util.LogInfoLev).Infof("failed to unmarshal data from configmap: %v", util.SafePrint(err))
		return result, err
	}
	return result, nil
}

func (cmMgr *ClusterInfoWitchCm) initClusterCmInformer(k8sClient kubernetes.Interface, stopCh <-chan struct{}) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(k8sClient, 0,
		informers.WithNamespace(util.MindXDlNameSpace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = util.CmConsumer + "=" + util.CmConsumerValue
		}))
	cmInformer := informerFactory.Core().V1().ConfigMaps().Informer()
	cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cmMgr.updateConfigMapCluster(obj, util.AddOperator)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			cmMgr.updateConfigMapCluster(newObj, util.UpdateOperator)
		},
		DeleteFunc: func(obj interface{}) {
			cmMgr.updateConfigMapCluster(obj, util.DeleteOperator)
		},
	})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)
}

func (cmMgr *ClusterInfoWitchCm) initDeviceAndNodeDCmInformer(k8sClient kubernetes.Interface, stopCh <-chan struct{}) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(k8sClient, 0,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = util.NormalCmConsumer + "=" + util.CmConsumerValue
		}))
	cmInformer := informerFactory.Core().V1().ConfigMaps().Informer()
	cmInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: InformerConfigmapFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				cmMgr.updateConfigMap(obj, util.AddOperator)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				cmMgr.updateConfigMap(newObj, util.UpdateOperator)
			},
			DeleteFunc: func(obj interface{}) {
				cmMgr.updateConfigMap(obj, util.DeleteOperator)
			},
		},
	})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)
}

// updateConfigMap update deviceInfo in cache
func (cmMgr *ClusterInfoWitchCm) updateConfigMap(obj interface{}, operator string) {
	if cmMgr == nil {
		klog.V(util.LogDebugLev).Infof("updateConfigMap failed: %s.", util.ArgumentError)
		return
	}
	klog.V(util.LogDebugLev).Info("Update DeviceInfo to cache")

	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		klog.V(util.LogErrorLev).Infof("Cannot convert to ConfigMap:%#v", obj)
		return
	}
	if CheckConfigMapIsDeviceInfo(cm) {
		if operator == util.AddOperator || operator == util.UpdateOperator {
			cmMgr.createOrUpdateDeviceInfo(cm)
			cmMgr.createOrUpdateSwitchInfo(cm)
		} else if operator == util.DeleteOperator {
			klog.V(util.LogDebugLev).Info("Del DeviceInfo from cache")
			nodeName := strings.TrimPrefix(cm.Name, util.DevInfoPreName)
			cmMgr.deviceInfos.Lock()
			delete(cmMgr.deviceInfos.Devices, nodeName)
			cmMgr.deviceInfos.Unlock()

			cmMgr.switchInfosFromCm.Lock()
			delete(cmMgr.switchInfosFromCm.Switches, nodeName)
			cmMgr.switchInfosFromCm.Unlock()
		}
		return
	}
	if CheckConfigMapIsNodeInfo(cm) {
		if operator == util.AddOperator || operator == util.UpdateOperator {
			cmMgr.createOrUpdateNodeInfo(cm)
		} else if operator == util.DeleteOperator {
			klog.V(util.LogDebugLev).Info("Del NodeInfo from cache")
			nodeName := strings.TrimPrefix(cm.Name, util.NodeDCmInfoNamePrefix)
			cmMgr.nodeInfosFromCm.Lock()
			delete(cmMgr.nodeInfosFromCm.Nodes, nodeName)
			cmMgr.nodeInfosFromCm.Unlock()
		}
	}
}

func (cmMgr *ClusterInfoWitchCm) updateConfigMapCluster(obj interface{}, operator string) {
	if cmMgr == nil {
		klog.V(util.LogDebugLev).Infof("updateConfigMapCluster failed: %s.", util.ArgumentError)
		return
	}
	klog.V(util.LogDebugLev).Info("update cluster configMap to cache")

	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		klog.V(util.LogErrorLev).Infof("cannot convert to ConfigMap: %#v", obj)
		return
	}
	if cm == nil {
		klog.V(util.LogErrorLev).Info("cm is nil")
		return
	}
	cmMgr.dealClusterDeviceInfo(cm, operator)
	cmMgr.dealClusterNodeInfo(cm, operator)
	cmMgr.dealClusterSwitchInfo(cm, operator)
}

func (cmMgr *ClusterInfoWitchCm) dealClusterDeviceInfo(cm *v1.ConfigMap, operator string) {
	if !strings.HasPrefix(cm.Name, util.ClusterDeviceInfo) {
		return
	}
	deviceInfoMap, err := getDataFromCM[map[string]NodeDeviceInfoWithID](cm, cm.Name)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("get device info failed :%v", err)
		return
	}
	cmMgr.deviceInfos.Lock()
	for deviceCmName, deviceInfo := range deviceInfoMap {
		nodeName := strings.TrimPrefix(deviceCmName, util.DevInfoPreName)
		if operator == util.AddOperator || operator == util.UpdateOperator {
			cmMgr.deviceInfos.Devices[nodeName] = NodeDeviceInfoWithID{
				NodeDeviceInfo:  deviceInfo.NodeDeviceInfo,
				SuperPodID:      deviceInfo.SuperPodID,
				CacheUpdateTime: time.Now().Unix(),
			}
		} else if operator == util.DeleteOperator {
			delete(cmMgr.deviceInfos.Devices, nodeName)
		}
	}
	cmMgr.deviceInfos.Unlock()
}

func (cmMgr *ClusterInfoWitchCm) dealClusterNodeInfo(cm *v1.ConfigMap, operator string) {
	if cm.Name != util.ClusterNodeInfo {
		return
	}
	nodeInfoMap, err := getDataFromCM[map[string]NodeDNodeInfo](cm, cm.Name)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("get node info failed :%v", err)
		return
	}
	cmMgr.nodeInfosFromCm.Lock()
	defer cmMgr.nodeInfosFromCm.Unlock()
	cmMgr.nodeInfosFromCm.Nodes = map[string]NodeDNodeInfo{}
	if operator == util.DeleteOperator {
		return
	}
	for nodeCmName, nodeInfo := range nodeInfoMap {
		nodeName := strings.TrimPrefix(nodeCmName, util.NodeDCmInfoNamePrefix)
		cmMgr.nodeInfosFromCm.Nodes[nodeName] = nodeInfo
	}
	return
}

func (cmMgr *ClusterInfoWitchCm) dealClusterSwitchInfo(cm *v1.ConfigMap, operator string) {
	if !strings.HasPrefix(cm.Name, util.ClusterSwitchInfo) {
		return
	}
	switchInfoMap, err := getDataFromCM[map[string]SwitchFaultInfo](cm, cm.Name)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("get switch info failed :%v", err)
		return
	}
	cmMgr.switchInfosFromCm.Lock()
	for switchCmName, switchInfo := range switchInfoMap {
		nodeName := strings.TrimPrefix(switchCmName, util.SwitchCmInfoNamePrefix)
		if operator == util.AddOperator || operator == util.UpdateOperator {
			cmMgr.switchInfosFromCm.Switches[nodeName] = switchInfo
		} else if operator == util.DeleteOperator {
			delete(cmMgr.switchInfosFromCm.Switches, nodeName)
		}
	}
	cmMgr.switchInfosFromCm.Unlock()
}

func (cmMgr *ClusterInfoWitchCm) createOrUpdateDeviceInfo(cm *v1.ConfigMap) {
	devInfo, err := getDataFromCM[*NodeDeviceInfoWithDevPlugin](cm, util.DevInfoCMKey)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get device info failed:%s", err)
		return
	}

	nodeName := strings.TrimPrefix(cm.Name, util.DevInfoPreName)
	cmMgr.deviceInfos.Lock()
	cmMgr.deviceInfos.Devices[nodeName] = NodeDeviceInfoWithID{
		NodeDeviceInfo:  devInfo.DeviceInfo,
		SuperPodID:      devInfo.SuperPodID,
		CacheUpdateTime: time.Now().Unix(),
	}
	cmMgr.deviceInfos.Unlock()
}

func (cmMgr *ClusterInfoWitchCm) createOrUpdateSwitchInfo(cm *v1.ConfigMap) {
	if cm == nil {
		return
	}
	switchInfo := SwitchFaultInfo{}
	data, ok := cm.Data[util.SwitchInfoCmKey]
	if !ok {
		return
	}
	unmarshalErr := json.Unmarshal([]byte(data), &switchInfo)
	if unmarshalErr != nil {
		klog.V(util.LogInfoLev).Infof("unmarshal switchInfo info failed, err: %s.", util.SafePrint(unmarshalErr))
		return
	}
	nodeName := strings.TrimPrefix(cm.Name, util.DevInfoPreName)
	cmMgr.switchInfosFromCm.Lock()
	cmMgr.switchInfosFromCm.Switches[nodeName] = switchInfo
	cmMgr.switchInfosFromCm.Unlock()
}

func (cmMgr *ClusterInfoWitchCm) createOrUpdateNodeInfo(cm *v1.ConfigMap) {
	nodeInfo, err := getDataFromCM[NodeInfoWithNodeD](cm, util.NodeInfoCMKey)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("get node info from configmap %s/%s failed, err: %s",
			cm.Namespace, cm.Name, err)
		return
	}
	nodeName := strings.TrimPrefix(cm.Name, util.NodeDCmInfoNamePrefix)
	cmMgr.nodeInfosFromCm.Lock()
	cmMgr.nodeInfosFromCm.Nodes[nodeName] = nodeInfo.NodeInfo
	cmMgr.nodeInfosFromCm.Unlock()
}

// GetDeviceInfosAndSetInformerStart get device Infos and check Informer health state
func GetDeviceInfosAndSetInformerStart(nodeList []*api.NodeInfo, useClusterD,
	selfMaintainAvailCard bool) map[string]NodeDeviceInfoWithID {
	deviceInfos := make(map[string]NodeDeviceInfoWithID)
	cmManager.deviceInfos.Lock()
	for _, nodeInfo := range nodeList {
		tmpDeviceInfo := initNodeDeviceInfoByCmMgr(nodeInfo, cmManager.deviceInfos.Devices[nodeInfo.Name], selfMaintainAvailCard)
		setNeedRestartInformer(tmpDeviceInfo.CacheUpdateTime, useClusterD)
		deviceInfos[nodeInfo.Name] = tmpDeviceInfo
	}
	cmManager.deviceInfos.Unlock()
	return deviceInfos
}

// GetDeviceInfoAndSetInformerStart get device Info and check Informer health state
func GetDeviceInfoAndSetInformerStart(nodeInfo *api.NodeInfo, useClusterD,
	selfMaintainAvailCard bool) NodeDeviceInfoWithID {
	cmManager.deviceInfos.Lock()
	tmpDeviceInfo := initNodeDeviceInfoByCmMgr(nodeInfo, cmManager.deviceInfos.Devices[nodeInfo.Name], selfMaintainAvailCard)
	setNeedRestartInformer(tmpDeviceInfo.CacheUpdateTime, useClusterD)
	cmManager.deviceInfos.Unlock()
	return tmpDeviceInfo
}

// GetNodeDInfos is to get a copy of nodeInfo which is get from configmap of nodeD
func GetNodeDInfos(nodeList []*api.NodeInfo) map[string]NodeDNodeInfo {
	nodeInfos := make(map[string]NodeDNodeInfo, util.MapInitNum)
	cmManager.nodeInfosFromCm.Lock()
	for _, nodeInfo := range nodeList {
		nodeInfos[nodeInfo.Name] = cmManager.nodeInfosFromCm.Nodes[nodeInfo.Name]
	}
	cmManager.nodeInfosFromCm.Unlock()
	return nodeInfos
}

// GetNodeDInfo is to get a copy of nodeInfo which is get from configmap of nodeD
func GetNodeDInfo(nodeInfo *api.NodeInfo) NodeDNodeInfo {
	cmManager.nodeInfosFromCm.Lock()
	nodeDInfo := cmManager.nodeInfosFromCm.Nodes[nodeInfo.Name]
	cmManager.nodeInfosFromCm.Unlock()
	return nodeDInfo
}

// GetSwitchInfos is to get a copy of switchInfo which is get from configmap of switch info
func GetSwitchInfos(nodeList []*api.NodeInfo) map[string]SwitchFaultInfo {
	switchInfos := make(map[string]SwitchFaultInfo, util.MapInitNum)
	cmManager.switchInfosFromCm.Lock()
	for _, nodeInfo := range nodeList {
		switchInfos[nodeInfo.Name] = cmManager.switchInfosFromCm.Switches[nodeInfo.Name]
	}
	cmManager.switchInfosFromCm.Unlock()
	return switchInfos
}

func initNodeDeviceInfoByCmMgr(nodeInfo *api.NodeInfo, deviceInfo NodeDeviceInfoWithID,
	selfMaintainAvailCard bool) NodeDeviceInfoWithID {
	tmpDeviceInfo := NodeDeviceInfoWithID{
		NodeDeviceInfo: NodeDeviceInfo{
			DeviceList: make(map[string]string),
			UpdateTime: deviceInfo.UpdateTime,
		},
		SuperPodID:      deviceInfo.SuperPodID,
		CacheUpdateTime: deviceInfo.CacheUpdateTime,
	}
	availableDevKey, _ := util.GetAvailableDevInfo(deviceInfo.DeviceList)
	for devListKey, devListValue := range deviceInfo.DeviceList {
		if devListKey == availableDevKey && selfMaintainAvailCard {
			devType := util.GetDeviceType(deviceInfo.DeviceList)
			nodeDevList, err := util.GetNodeDevListFromAnno(nodeInfo)
			if err != nil {
				klog.V(util.LogErrorLev).Infof("get node device list from annotation failed, error: %v", err)
				return tmpDeviceInfo
			}
			_, unHealthyDevList := util.GetUnhealthyDevInfo(deviceInfo.DeviceList)
			_, recoveringDevList := util.GetRecoveringDevInfo(deviceInfo.DeviceList)
			podUsedDevList := util.GetActivePodUsedDevFromNode(nodeInfo, devType)
			availDev := sets.NewString(nodeDevList...).
				Delete(podUsedDevList...).Delete(unHealthyDevList...).Delete(recoveringDevList...).List()
			sort.Strings(availDev)
			tmpDeviceInfo.DeviceList[devListKey] = strings.Join(availDev, ",")
			klog.V(util.LogDebugLev).Infof("node[%s] device list: %v, available list: %v, pod used list: %v, "+
				"unhealthy list: %v. The above does not include vnpu card, may not be accurate in the vnpu scence",
				nodeInfo.Name, nodeDevList, availDev, podUsedDevList, unHealthyDevList)
			// device info cm has not been updated properly, which may cause the self maintain card to be
			// unable to update timely. This will affect rescheduling
			tmpDeviceInfo.UpdateTime = time.Now().Unix()
		} else {
			tmpDeviceInfo.DeviceList[devListKey] = devListValue
		}
	}
	return tmpDeviceInfo
}

func setNeedRestartInformer(updateTime int64, useClusterD bool) {
	if needStartInformer {
		return
	}
	if !useClusterD {
		if time.Now().Unix()-updateTime > deviceInfoRefreshTime && updateTime != 0 {
			needStartInformer = true
		}
	}
	if time.Now().Unix()-updateTime > clusterInfoRefreshTime && updateTime != 0 {
		needStartInformer = true
	}
}
