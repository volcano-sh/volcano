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
Package plugin is using for HuaWei Ascend pin affinity schedule frame.
*/

package plugin

import "volcano.sh/volcano/third_party/ascend-for-volcano/common/util"

const (
	// TorNodeCMName the Name of tor info configmap
	TorNodeCMName = "basic-tor-node-cm"
	// TorShareCMName the Name of tor share info configmap
	TorShareCMName = "tor-share-cm"
	// TorInfoCMKey the key of tor info in configmap
	TorInfoCMKey = "tor_info"
	// TorLevelCMKey the key of tor level in configmap
	TorLevelCMKey = "tor_level"
	// SingleLayer the single layer switch value of tor level in configmap
	SingleLayer = "single_layer"
	// TorAffinityKey the key of tor affinity
	TorAffinityKey = "tor-affinity"
	// GlobalTorInfoKey the key of tor share info in configmap
	GlobalTorInfoKey = "global-tor-info"
	// NormalSchema the value of normal tor affinity
	NormalSchema       = "normal-schema"
	keyOfSharedTorNum  = "shared-tor-num"
	shareTorNum1       = 1
	shareTorNum2       = 2
	keyOfNSLBVersion   = "nslb-version"
	defaultNSLBVersion = "1.0"
	// NSLB2Version nslb 2.0 version
	NSLB2Version = "2.0"
	isHealthy    = "isHealthy"
	isSharedTor  = "isSharedTor"
	vcTaskIndex  = "VC_TASK_INDEX"
	cmNameSpace  = "volcano-system"

	defaultSuperPodSize = 48
	defaultReserveNodes = 2
	// PodRankIndexKey rank index key
	PodRankIndexKey = "hccl/rankIndex"
	// ReplicaSetType replicaset type
	ReplicaSetType = "ReplicaSet"
	// PodGroupScheduleKey podgroup schedule the enable key
	PodGroupScheduleKey = "podgroup-sched-enable"
	// PodGroupScheduleValue podgroup schedule the enable value
	PodGroupScheduleValue = "true"
)

const (
	taskOrderHighPriority = -1
	taskOrderLowPriority  = 1
	taskOrderSamePriority = 0
)

const (
	freeTor = 0
)

const (
	// the define of tor is healthy
	healthyTor   = 0
	unhealthyTor = 1
)

const (
	scoreWeight                   = 100
	defaultSchedulingTaskNum      = -1
	deviceInfoForceUpdateInterval = 10
)

const (
	chipTypeKey = "node.kubernetes.io/npu.chip.name"
	// ChipTypeB1 chip type 910B1
	ChipTypeB1 = util.ChipKind + "B1"
	// ChipTypeB2C chip type 910B2C
	ChipTypeB2C = util.ChipKind + "B2C"
	// ChipTypeB2 chip type 910 B2
	ChipTypeB2 = util.ChipKind + "B2"
	// ChipTypeB3 chip type 910B3
	ChipTypeB3 = util.ChipKind + "B3"
	// ChipTypeB4 chip type 910B4
	ChipTypeB4 = util.ChipKind + "B4"
)

// the temp of 910B1/910B2C
const (
	// VNPUTempVir06 vir06_1c_16g
	VNPUTempVir06 = "vir06_1c_16g"
	// VNPUTempVir03 vir03_1c_8g
	VNPUTempVir03 = "vir03_1c_8g"
	// VNPUTempVir12 vir12_3c_32g
	VNPUTempVir12 = "vir12_3c_32g"
)

// the temp of 910B3
const (
	// VNPUTempVir05 vir05_1c_16g
	VNPUTempVir05 = "vir05_1c_16g"
	// VNPUTempVir10 vir10_3c_32g
	VNPUTempVir10 = "vir10_3c_32g"
)

// the temp of 910B4
const (
	// VNPUB4TempVir05 vir05_1c_8g
	VNPUB4TempVir05 = "vir05_1c_8g"
	// VNPUB4TempVir10C3NM vir10_3c_16g_nm
	VNPUB4TempVir10C3NM = "vir10_3c_16g_nm"
	// VNPUB4TempVir10C4M vir10_4c_16g_m
	VNPUB4TempVir10C4M = "vir10_4c_16g_m"
	// VNPUB4TempVir10 vir10_3c_16g
	VNPUB4TempVir10 = "vir10_3c_16g"
)

const (
	// GraceExitValue grace exit value
	GraceExitValue = 1
	// DefaultExitValue default exit value
	DefaultExitValue = 0
)

const (
	// DefaultGraceOverTime time interval for grace delete
	DefaultGraceOverTime = 900
	// GraceOverTimeKey for GraceOverTime config by user
	GraceOverTimeKey  = "grace-over-time"
	minGraceOverTime  = 2
	maxGraceOverTime  = 3600
	reserveNodesKey   = "reserve-nodes"
	sizeOfSuperPodKey = "super-pod-size"
)
