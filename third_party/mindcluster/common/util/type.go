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

import "regexp"

const (
	// LogErrorLev for log error.
	LogErrorLev = 1
	// LogWarningLev for log warning.
	LogWarningLev = 2
	// LogInfoLev for log information.
	LogInfoLev = 3
	// LogDebugLev for log debug.
	LogDebugLev = 4
	// ErrorInt return -1 when get error for int
	ErrorInt = -1
	// NPUIndex2 the 2 index.
	NPUIndex2 = 2
	// NPUIndex3 the 3 index.
	NPUIndex3 = 3
	// NPUIndex8 the 8 index.
	NPUIndex8 = 8
	// NPUIndex16 the 16 index.
	NPUIndex16 = 16
	// NPUIndex7 the 7 index.
	NPUIndex7 = 7
	// NPUIndex4 the 4 index.
	NPUIndex4 = 4
	// NPUIndex5 the 5 index.
	NPUIndex5 = 5
	// NPUIndex6 the 6 index.
	NPUIndex6 = 6
	// NPUIndex1 the 1 index.
	NPUIndex1 = 1
	// NPUIndex0 the 0 index.
	NPUIndex0 = 0
	// NPUIndex9 the 9 index.
	NPUIndex9 = 9
	// NPUIndex10 the 10 index.
	NPUIndex10 = 10
	// NPUIndex11 the 11 index.
	NPUIndex11 = 11
	// NPUIndex12 the 12 index.
	NPUIndex12 = 12
	// NPUIndex13 the 13 index.
	NPUIndex13 = 13
	// NPUIndex14 the 14 index.
	NPUIndex14 = 14
	// NPUIndex15 the 15 index.
	NPUIndex15 = 15
	// CoreNum32 32 core 910
	CoreNum32 = 32
	// CoreNum3 3 core 910
	CoreNum3 = 3
	// CoreNum5 5 core 910
	CoreNum5 = 5
	// CoreNum10 10 core 910
	CoreNum10 = 10
	// CoreNum6 6 core 910
	CoreNum6 = 6
	// CoreNum12 12 core 910
	CoreNum12 = 12
	// CoreNum30 30 core 910
	CoreNum30 = 30
	// CoreNum20 20 core 910
	CoreNum20 = 20
	// CoreNum25 25 core 910
	CoreNum25 = 25
	// CoreNum24 24 core 910
	CoreNum24 = 24
	// CpuNum14 14 cpu 910
	CpuNum14 = 14
	// CpuNum6 6 cpu 910
	CpuNum6 = 6
	// MapInitNum for map init length.
	MapInitNum = 3
	// Base10 for const 10.
	Base10 = 10
	// BitSize64 for const 64
	BitSize64 = 64
	// MaxSliceNum max slice number
	MaxSliceNum = 128
	// NPUHexKilo for const 1000,volcano frame used.
	NPUHexKilo = 1000
	// Accelerator for custom tag.
	Accelerator = "accelerator"
	// CMInitParamKey init param key in scheduler configmap
	CMInitParamKey = "init-params"
	// AcceleratorType for selector.
	AcceleratorType = "accelerator-type"
	// ModuleA3x16AcceleratorType for module mode.
	ModuleA3x16AcceleratorType = "module-a3-16"
	// ModuleAcceleratorType for module mode.
	ModuleAcceleratorType = "module"
	// ServerType server type value takes Ascend310P-10-dual/Ascend910-32...
	ServerType = "servertype"
	// ServerTypeDual dual card
	ServerTypeDual = "dual"

	// SegmentEnable for VNPU segment enable flag. Default is "false".
	SegmentEnable = "presetVirtualDevice"

	// UseClusterInfoManager for use cluster info manager, default is true
	UseClusterInfoManager = "useClusterInfoManager"
	// ForceEnqueue for force enqueue when npu num in cluster is meet job required , default is true
	ForceEnqueue = "forceEnqueue"
	// SelfMaintainAvailCard for volcano self maintain available card, default is true
	SelfMaintainAvailCard = "self-maintain-available-card"

	// SubHealthyStrategyLabel sub-healthy handle strategy. default is grace exit
	SubHealthyStrategyLabel = "subHealthyStrategy"
	// SubHealthyIgnore ignore sub-healthy
	SubHealthyIgnore = "ignore"
	// SubHealthyGraceExit don't use sub-healthy node and grace exit
	SubHealthyGraceExit = "graceExit"
	// SubHealthyForceExit don't use sub-healthy node and force exit
	SubHealthyForceExit = "forceExit"
	// SubHealthyHotSwitch strategy name of hot switch
	SubHealthyHotSwitch = "hotSwitch"
	// DevInfoNameSpace device-plugin install Namespace
	DevInfoNameSpace = "kube-system"
	// MindXDlNameSpace mindx dl Namespace
	MindXDlNameSpace = "mindx-dl"
	// DevInfoPreName like "mindx-dl-deviceinfo-ubuntu"
	DevInfoPreName = "mindx-dl-deviceinfo-"
	// NodeDCmInfoNamePrefix is for noded to report node healthy state
	NodeDCmInfoNamePrefix = "mindx-dl-nodeinfo-"
	// SwitchCmInfoNamePrefix is the prefix for switch fault configmap
	SwitchCmInfoNamePrefix = "mindx-dl-switchinfo-"
	// NodeHealthyStatusKey  is the key of node healthy status from configmap data of noded and clusterD
	NodeHealthyStatusKey = "NodeHealthyStatus"
	// NodeSubHealthy means there is some fault on the node which is reported by nodeD, but will not immediately
	// make node unhealthy, this status will prevent new task schduled on this node and reschedule will not consider
	// this node
	NodeSubHealthy = "SubHealthy"
	// NodeUnHealthy is the node unhealthy status reported by nodeD configmap, switch info CM and clusterD,
	// in this case pod will be rescheduling
	NodeUnHealthy = "UnHealthy"
	// NodeHealthyByNodeD is the node healthy status reported by nodeD configmap
	NodeHealthyByNodeD = "Healthy"
	// NodeDEnableKey indicates if the label has been set
	NodeDEnableKey = "nodeDEnable"
	// NodeDEnableOnValue the value of NodeDEnableKey, which means nodeD has been enabled
	NodeDEnableOnValue = "on"

	// PreSeparateFaultCode  PreSeparate fault Code
	PreSeparateFaultCode = "PreSeparate"

	// SwitchNodeHealtyStatuskey same with noded there will be healthy subhealthy unhealthy status report by switch info
	SwitchNodeHealtyStatuskey = "NodeStatus"

	// DevInfoCMKey mindx-dl-deviceinfo configmap key
	DevInfoCMKey = "DeviceInfoCfg"
	// NodeInfoCMKey node info configmap key
	NodeInfoCMKey = "NodeInfo"
	// SwitchInfoCmKey is the key of switch info configmap
	SwitchInfoCmKey = "SwitchInfoCfg"
	// RePropertyCacheName rescheduling keyword in init env.cache
	RePropertyCacheName = "re-scheduling"
	// CmCheckCode Check code key
	CmCheckCode = "checkCode"
	// JobRecovery keywords for retain
	JobRecovery = "job-recovery"

	// DeleteOperator informer delete operator
	DeleteOperator = "delete"
	// AddOperator informer add operator
	AddOperator = "add"
	// UpdateOperator informer update operator
	UpdateOperator = "update"

	// CmConsumer who uses these configmap
	CmConsumer = "mx-consumer-volcano"
	// NormalCmConsumer normal who uses these configmap
	NormalCmConsumer = "mx-consumer-cim"
	// CmConsumerValue the value only for true
	CmConsumerValue = "true"
	// ClusterDeviceInfo the name of cluster device info configmap
	ClusterDeviceInfo = "cluster-info-device-"
	// ClusterNodeInfo the name of cluster node info configmap
	ClusterNodeInfo = "cluster-info-node-cm"
	// ClusterSwitchInfo the name of cluster switch info configmap
	ClusterSwitchInfo = "cluster-info-switch-"

	// PodPredicateTime set pod PodPredicateTime for using by device-plugin.
	PodPredicateTime = "predicate-time"
	// NodeNotMeetTopologyWarning node not satisfy the schedulable topology warning.
	NodeNotMeetTopologyWarning = "the npus on this node don't satisfy the schedulable topology"
	// ArgumentError argument nil error.
	ArgumentError = "invalid argument"
	// RankIdNotExistError rank id does not exist
	RankIdNotExistError = "rank id does not exist"
	// JobKindKey for define the Job kind:ascend-310P, ascend-910
	JobKindKey = "ring-controller.atlas"
	// DistributedJobKey flag for distributed job
	DistributedJobKey = "distributed-job"
	// DistributedJobValue indicate distributed job
	DistributedJobValue = "true"
	// StandaloneJobValue indicate standalone job
	StandaloneJobValue = "false"

	// SuperPodAnnoKey annotation key of super pod
	SuperPodAnnoKey = "sp-block"
	// DistributedInferKey distributed infer
	DistributedInferKey = "distributed"
	// DistributedInferLabel true or false
	DistributedInferLabel = "true"
	// OperatorNameLabelKey pod label key for acjob operator name
	OperatorNameLabelKey = "training.kubeflow.org/operator-name"
)

const (
	// AffScore0 value 0 for scored.
	AffScore0 = iota
	// AffScore1 value 1 for scored.
	AffScore1
	// AffScore2 value 2 for scored.
	AffScore2
	// AffScore3 value 3 for scored.
	AffScore3
	// AffScore4 value 4 for scored.
	AffScore4
	// AffScore5 value 5 for scored.
	AffScore5
	// AffScore6 value 6 for scored.
	AffScore6
	// AffScore7 value 7 for scored.
	AffScore7
	// AffScore8 value 8 for scored.
	AffScore8
	// AffScore15 value 15 for scored.
	AffScore15
)

const (
	// JobNotEnqueue job enqueue failed
	JobNotEnqueue = -1
	// JobEnqueue job enqueue success
	JobEnqueue = 1
	// JobEnqueueSkip skip the judgement of ascend-volcano-plugin in the job enqueue phase
	JobEnqueueSkip = 0
	// PodGroupInqueue the pg Inqueue status
	PodGroupInqueue = "Inqueue"
	// PodGroupPending the pg Pending status
	PodGroupPending = "Pending"
	// PodGroupRunning the pg Running status
	PodGroupRunning = "Running"
	// PodGroupUnknown the pg Unknown status
	PodGroupUnknown = "Unknown"
	// PodGroupUnschedulableType the pg Unschedulable Condition
	PodGroupUnschedulableType = "Unschedulable"
	// EnableFunc enable the function
	EnableFunc = "on"
	// SinglePodTag the tag of single pod rescheduling
	SinglePodTag = "pod-rescheduling"
	// ProcessRecoverEnable the tag of process rescheduling
	ProcessRecoverEnable = "process-recover-enable"
	// BaseDeviceInfoKey base device info key
	BaseDeviceInfoKey = "baseDeviceInfos"
	// TorAffinityKey the key of tor affinity
	TorAffinityKey = "tor-affinity"
	// LargeModelTag the value of large model
	LargeModelTag = "large-model-schema"
	// NormalSchema the value of normal tor affinity
	NormalSchema         = "normal-schema"
	fillJobMaxNPUTaskNum = 4
	// NullTag the value means not use tor affinity
	NullTag = "null"
	// DevSplitNum device split number
	DevSplitNum = 2
)

const (
	// Rank0 default time of pod deleted
	Rank0 = "0"
)

const (
	// SeparateFaultStrategy Separate task
	SeparateFaultStrategy = "Separate"
	// SubHealthFaultStrategy SubHealth task
	SubHealthFaultStrategy = "SubHealth"
	// RelationFault fault type of relation fault
	RelationFault = "RelationFaultSeparate"
)

const (
	// Permit indicates permits job to be pipelined
	Permit = 1
	// Abstain indicates abstains in voting job to be pipelined
	Abstain = 0
	// Reject indicates  rejects job to be pipelined
	Reject = -1
)

const (
	// Namespace check item podName namespace
	Namespace = "namespace"
	// PodName check item podName
	PodName = "podName"
	// PodNameMaxLength pod name max length
	PodNameMaxLength = 253
	// PodNameSpaceMaxLength pod namespace max length
	PodNameSpaceMaxLength = 63
	// PodAnnotationMaxLength pod annotation max data length 1MB
	PodAnnotationMaxLength = 1024 * 1024
	// MaxDevicesNum max device num
	MaxDevicesNum = 100
)

const (
	// AppTypeLabelKey job type label key
	AppTypeLabelKey = "app"
	// ControllerAppType controller app type
	ControllerAppType = "mindie-ms-controller"
	// CoordinatorAppType coordinator app type
	CoordinatorAppType = "mindie-ms-coordinator"
)

// HotSwitch
const (
	// NeedVolcanoOpeKey for volcano
	NeedVolcanoOpeKey = "needVolcanoOpe"
	// OpeTypeDelete operation type delete
	OpeTypeDelete = "delete"
)

var (
	podRegexp = map[string]*regexp.Regexp{
		Namespace: regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`),
		PodName:   regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`),
	}

	// subHealthyStrategs subHealthy Strategs
	subHealthyStrategs = []string{SubHealthyIgnore, SubHealthyGraceExit, SubHealthyForceExit, SubHealthyHotSwitch}
)

// VTemplate for vNode resource
type VTemplate struct {
	// ChipKind Ascend910/Ascend310P
	ChipKind   string
	AICore     int
	AICPU      int
	DVPPEnable string
}

// VResource resource dimensions
type VResource struct {
	Aicore int
	Aicpu  int
	DVPP   string
}

// Instance is for annotation
type Instance struct { // Instance
	PodName    string   `json:"pod_name"`  // pod Name
	ServerID   string   `json:"server_id"` // serverdId
	SuperPodId int32    `json:"super_pod_id"`
	Devices    []Device `json:"devices"` // dev
}

// Device id for Instcance
type Device struct { // Device
	DeviceID      string `json:"device_id"` // device id
	DeviceIP      string `json:"device_ip"` // device ip
	SuperDeviceID string `json:"super_device_id,omitempty"`
}

// NpuBaseInfo npu base info
type NpuBaseInfo struct {
	IP            string
	SuperDeviceID uint32
}
