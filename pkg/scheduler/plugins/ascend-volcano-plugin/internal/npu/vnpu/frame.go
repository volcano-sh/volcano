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
Package vnpu is using for HuaWei Ascend pin vnpu allocation.
*/
package vnpu

import (
	"errors"
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// GetTaskResource get vTask used resource.
func (tp *VirtualNPU) GetTaskResource(task *api.TaskInfo, node plugin.NPUNode) (util.VResource, error) {
	if tp == nil || task == nil {
		klog.V(util.LogDebugLev).Infof("GetTaskResource failed:%s", util.ArgumentError)
		return util.VResource{}, errors.New(util.ArgumentError)
	}
	klog.V(util.LogDebugLev).Infof("enter task<%s> GetTaskResource", task.Name)
	coreNum, err := getAiCoreNumFromTask(task)
	if err != nil {
		return util.VResource{}, fmt.Errorf("task %s AscendNPUCore read failed", task.Name)
	}
	klog.V(util.LogDebugLev).Infof("get coreNum %d", coreNum)

	tempCore := node.TotalRes.Aicore
	if tempCore == 0 {
		return util.VResource{}, fmt.Errorf("%s not inital for Aicore is 0", node.Name)
	}
	if node.IsResourceWholeCard(coreNum) {
		res := util.VResource{
			Aicore: coreNum,
			Aicpu:  coreNum * node.TotalRes.Aicpu / tempCore,
			DVPP:   plugin.AscendDVPPEnabledNull,
		}
		return res, nil
	}

	dvpp, err := tp.getVTaskDVPP(task)
	if err != nil {
		return util.VResource{}, err
	}

	cpuLevel := tp.getVTaskLevel(task)

	virTemplate := getResTemplateFromTaskSettingAndChipType(coreNum, dvpp, node.ChipType)
	if node.ChipKind == plugin.Ascend310P {
		virTemplate = getResTemplateFromTaskSetting(coreNum, cpuLevel, dvpp)
	}
	if virTemplate == "" {
		return util.VResource{}, fmt.Errorf("task<%s> get virTemplate is null", task.Name)
	}

	klog.V(util.LogDebugLev).Infof("vnpu template string for cur task:<%s>", virTemplate)
	taskReqRes := tp.VT.Data[virTemplate]
	return taskReqRes, nil
}

func (tp *VirtualNPU) getVTaskDVPP(task *api.TaskInfo) (string, error) {
	dvpp, ok := task.Pod.Labels[plugin.AscendVNPUDVPP]
	if !ok {
		klog.V(util.LogWarningLev).Infof("%s not set VNPU dvpp, use default null.", task.Name)
		return plugin.AscendDVPPEnabledNull, nil
	}
	switch dvpp {
	case plugin.AscendDVPPEnabledOff, plugin.AscendDVPPEnabledNull, plugin.AscendDVPPEnabledOn:
		break
	default:
		klog.V(util.LogWarningLev).Infof("%s set wrong dvpp %s.", task.Name, dvpp)
		return "", fmt.Errorf("err dvpp value:%s", dvpp)
	}
	return dvpp, nil
}

func (tp *VirtualNPU) getVTaskLevel(task *api.TaskInfo) string {
	cpuLevel, ok := task.Pod.Labels[plugin.AscendVNPULevel]
	if !ok {
		klog.V(util.LogWarningLev).Infof("%s not set VNPU level, use default low.", task.Name)
		return plugin.AscendVNPULevelLow
	}
	switch cpuLevel {
	case plugin.AscendVNPULevelLow, plugin.AscendVNPULevelHigh:
		break
	default:
		klog.V(util.LogWarningLev).Infof("%s set wrong VNPU level %s, use default low.", task.Name, cpuLevel)
		cpuLevel = plugin.AscendVNPULevelLow
	}
	return cpuLevel
}

func getAiCoreNumFromTask(task *api.TaskInfo) (int, error) {
	for _, container := range task.Pod.Spec.Containers {
		coreNum, ok := container.Resources.Requests[util.AscendNPUCore]
		if !ok {
			continue
		}
		if coreNum.Value() == 0 {
			continue
		}
		return int(coreNum.Value()), nil
	}
	return 0, fmt.Errorf("getAiCoreNumFromTask get resource requests failed")
}

// getResTemplateFromTaskSetting get like vir04_3c_ndvpp
func getResTemplateFromTaskSetting(coreNum int, cpuLevel, dvpp string) string {
	var virTemplate string
	switch coreNum {
	case util.NPUIndex1:
		virTemplate = plugin.VNPUTempVir01
	case util.NPUIndex2:
		virTemplate = plugin.VNPUTempVir02
		if cpuLevel == plugin.AscendVNPULevelLow {
			virTemplate = virTemplate + "_1c"
		}
	case util.NPUIndex4:
		virTemplate = getVirTemplate(dvpp, cpuLevel)
	default:
		klog.V(util.LogErrorLev).Infof(coreNumErr, coreNum)
		return ""
	}
	return virTemplate
}

// getResTemplateFromTaskSetting get like vir04_3c_ndvpp
func getResTemplateFromTaskSettingAndChipType(coreNum int, dvpp, chipType string) string {
	switch chipType {
	case plugin.ChipTypeB1, plugin.ChipTypeB2C, plugin.ChipTypeB2:
		return getResTemplateFromTaskSettingForB1AndB2C(coreNum)
	case plugin.ChipTypeB3:
		return getResTemplateFromTaskSettingForB3(coreNum)
	case plugin.ChipTypeB4:
		return getResTemplateFromTaskSettingForB4(coreNum, dvpp)
	default:
		klog.V(util.LogErrorLev).Infof(coreNumErr, coreNum)
		return ""
	}
}

// getResTemplateFromTaskSettingForB1AndB2C get like vir04_3c_ndvpp
func getResTemplateFromTaskSettingForB1AndB2C(coreNum int) string {
	var virTemplate string
	switch coreNum {
	case util.NPUIndex3:
		virTemplate = plugin.VNPUTempVir03
	case util.NPUIndex6:
		virTemplate = plugin.VNPUTempVir06
	case util.NPUIndex12:
		virTemplate = plugin.VNPUTempVir12
	default:
		klog.V(util.LogErrorLev).Infof(coreNumErr, coreNum)
		return ""
	}
	return virTemplate
}

// getResTemplateFromTaskSetting get like vir04_3c_ndvpp
func getResTemplateFromTaskSettingForB3(coreNum int) string {
	var virTemplate string
	switch coreNum {
	case util.NPUIndex5:
		virTemplate = plugin.VNPUTempVir05
	case util.NPUIndex10:
		virTemplate = plugin.VNPUTempVir10
	default:
		klog.V(util.LogErrorLev).Infof(coreNumErr, coreNum)
		return ""
	}
	return virTemplate
}

// getResTemplateFromTaskSetting get like vir04_3c_ndvpp
func getResTemplateFromTaskSettingForB4(coreNum int, dvpp string) string {
	var virTemplate string
	switch coreNum {
	case util.NPUIndex5:
		virTemplate = plugin.VNPUB4TempVir05
	case util.NPUIndex10:
		return getB4TempFromTaskLabel(dvpp)
	default:
		klog.V(util.LogErrorLev).Infof(coreNumErr, coreNum)
		return ""
	}
	return virTemplate
}

func getB4TempFromTaskLabel(dvpp string) string {
	var virTemplate string
	switch dvpp {
	case plugin.AscendDVPPEnabledOn:
		virTemplate = plugin.VNPUB4TempVir10C4M
	case plugin.AscendDVPPEnabledOff:
		virTemplate = plugin.VNPUB4TempVir10C3NM
	default:
		virTemplate = plugin.VNPUB4TempVir10
	}
	return virTemplate
}

func getVirTemplate(dvpp string, cpuLevel string) string {
	switch dvpp {
	case plugin.AscendDVPPEnabledOn:
		return plugin.VNPUTempVir04C4cDVPP
	case plugin.AscendDVPPEnabledOff:
		return plugin.VNPUTempVir04C3NDVPP
	default:
		virTemplate := plugin.VNPUTempVir04
		if cpuLevel == plugin.AscendVNPULevelLow {
			virTemplate = virTemplate + "_3c"
		}
		return virTemplate
	}
}
