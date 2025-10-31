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

// Package plugin is using for HuaWei Ascend pin affinity schedule.
package plugin

import (
	"regexp"

	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
)

const (
	// PluginName the HuaWei NPU 's plugin name.
	PluginName = "NPU"

	objectNilError = "object or argument is nil"
	podRankIndex   = "hccl/rankIndex"

	// FormatIncorrectError format incorrect error
	FormatIncorrectError = "format incorrect"

	// AscendVNPULevel vnpu level
	AscendVNPULevel = "vnpu-level"
	// AscendVNPULevelLow low
	AscendVNPULevelLow = "low"
	// AscendVNPULevelHigh high
	AscendVNPULevelHigh = "high"
	// AscendVNPUDVPP dvpp enable
	AscendVNPUDVPP = "vnpu-dvpp"
	// AscendDVPPEnabledOff off
	AscendDVPPEnabledOff = "no"
	// AscendDVPPEnabledNull null
	AscendDVPPEnabledNull = "null"
	// AscendDVPPEnabledOn on
	AscendDVPPEnabledOn = "yes"
	// AscendNDVPPValue value
	AscendNDVPPValue = "ndvpp"
	// AscendDVPPValue value
	AscendDVPPValue = "dvpp"
	// VNPUTempVir01 vir01
	VNPUTempVir01 = "vir01"
	// VNPUTempVir02 vir02
	VNPUTempVir02 = "vir02"
	// VNPUTempVir02C1 vir02_1c
	VNPUTempVir02C1 = "vir02_1c"
	// VNPUTempVir04  vir04
	VNPUTempVir04 = "vir04"
	// VNPUTempVir04C3 vir04_3c
	VNPUTempVir04C3 = "vir04_3c"
	// VNPUTempVir04C3NDVPP vir04_3c_ndvpp
	VNPUTempVir04C3NDVPP = "vir04_3c_ndvpp"
	// VNPUTempVir04C4cDVPP vir04_4c_dvpp
	VNPUTempVir04C4cDVPP = "vir04_4c_dvpp"
	// VNPUTempVir08  vir08 only 910
	VNPUTempVir08 = "vir08"
	// VNPUTempVir16  vir16 only 910
	VNPUTempVir16       = "vir16"
	cardHealthySuffix   = ""
	unhealthyCardSuffix = "-Unhealthy"
	// ResetInfoCMNamePrefix for reset configmap name prefix
	ResetInfoCMNamePrefix = "reset-config-"
	// ResetInfoCMDataKey for reset configmap data key
	ResetInfoCMDataKey = "reset.json"
	// ResetInfoTypeKey for reset configmap type key
	ResetInfoTypeKey = "restartType"
	// PodRescheduleRestartType for hot reset restart type
	PodRescheduleRestartType = "podReschedule"
	oldCapacity              = "Capability"
	newCapacity              = "Capacity"
	noneResourceErr          = "npu resource is not enable"
)

var (
	ascend910VirtualDevNameReg = regexp.MustCompile(util.Ascend910 + "-([2-6]|8|10|12|16)c")
	ascend310VirtualDevNameReg = regexp.MustCompile(util.Ascend310P + "-(1|2|4)c")
)
