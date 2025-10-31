package vnpu

import (
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

var Ascend310pvNPUEnable bool

const (
	DeviceName = "ascend310p-vNPU"
)

type NPUDevice struct { // One VNode/VChip?
	VT VTemplate
	// Chips map chipID to VChip class
	Chips map[int]*VChip
	// ChipKind Ascend910/310/310p
	ChipKind string
	// UnhealthyChipIds the card unhealthy chip ids in this node
	UnhealthyChipIds map[int]struct{}
	// ServerType Ascend310p-10-dual cardType-cardCoreNum-duo
	ServerType string
	// TotalChipNum num of total chips, get from capacity
	TotalChipNum int
	// AiCorePerChip num of aicore on each chip
	AiCorePerChip int
	// FreeChipNum num of free chips get from device-info
	FreeChipNum int
	// TotalRes total resource on node
	TotalRes VResource
	// ValidVNode node init success
	ValidVNode bool
	// Chip type 910B1/910B2C/910B3/910B4
	ChipType string

	DowngradeCache map[string]struct{} //if the pod has been downgraded in the node

	// for Concurrent task. not same core request task only has one on a node in same time.
	// nodeName: templateName:taskUID
	ConCache map[string]map[types.UID]struct{} //types.UID对应pod.UID
}

type NodeInf struct {
	Name       string
	Capability map[v1.ResourceName]float64
	Allocate   map[v1.ResourceName]float64
	Idle       map[v1.ResourceName]float64
	//no need and cannot maintain taskInfo in NodeInf because of cyclic dependencies
	//Tasks          map[api.TaskID]*api.TaskInfo
	BaseDeviceInfo string
	// node annotation and device info + switch info + node info
	Annotation        map[string]string
	Label             map[string]string
	Address           string
	SuperPodID        int32
	DevInfoUpdateTime int64
}

// VTemplate vNPU template
type VTemplate struct {
	Data map[string]VResource
	Temp string
}

type VChip struct {
	PodMap map[string]*v1.Pod
	ID     []string
	// Name Ascend910-0
	Name string
	// Kind Ascend910/Ascend310/Ascend310P
	Kind        string
	IsDual      bool
	Unstable    bool
	CoreNum     int
	SegmentFlag bool
	TotalRes    VResource
	UsedRes     VResource
	FreeRes     VResource
}
type VResource struct {
	Aicore int
	Aicpu  int
	DVPP   string
}

type vChipsList []*VChip

// VolcanoFrame passed in by the volcano frame.
type VolcanoFrame struct {
	UID             types.UID
	KubeClient      kubernetes.Interface
	InformerFactory informers.SharedInformerFactory
	VJobTemplate    map[string]map[string]VResource
	ConfigParameters
}

// ConfigParameters some volcano scheduler parameters
type ConfigParameters struct {
	StaticParameters
	DynamicParameters
}

// StaticParameters volcano scheduler static parameters
type StaticParameters struct {
	OnceInit              *sync.Once
	UseClusterD           bool
	SelfMaintainAvailCard bool
	NslbVersion           string
	SharedTorNum          int
	IsFirstSession        *bool // scheduler first session message is unreliable
}

// DynamicParameters volcano scheduler dynamic parameters
type DynamicParameters struct {
	PresetVirtualDevice bool
	GraceDeleteTime     int64
	SuperPodSize        int
	ReservePodSize      int
}

// Below is the definition of const variables

const (
	// PluginName the HuaWei NPU 's plugin name.
	PluginName = "huaweiNPU"

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
	VNPUTempVir16 = "vir16"
	// Ascend310P 310P template name
	//Ascend310P = "Ascend310P"
	//// Ascend910 910 template name
	//Ascend910           = "Ascend910"
	CardHealthySuffix   = ""
	UnhealthyCardSuffix = "-Unhealthy"
	// ResetInfoCMNamePrefix for reset configmap name prefix
	ResetInfoCMNamePrefix = "reset-config-"
	// ResetInfoCMDataKey for reset configmap data key
	ResetInfoCMDataKey = "reset.json"
	// ResetInfoTypeKey for reset configmap type key
	ResetInfoTypeKey = "restartType"
	// PodRescheduleRestartType for hot reset restart type
	PodRescheduleRestartType = "podReschedule"
	OldCapacity              = "Capability"
	NewCapacity              = "Capacity"
	NoneResourceErr          = "npu resource is not enable"
)

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
	// HwPreName pre name
	HwPreName = "huawei.com/"
	// Accelerator for custom tag.
	Accelerator = "accelerator"
	// CMInitParamKey init param key in scheduler configmap
	CMInitParamKey = "init-params"
	// AcceleratorType for selector.
	AcceleratorType = "accelerator-type"
	// Module910bx16AcceleratorType for module mode.
	Module910bx16AcceleratorType = "module-910b-16"
	// Module910bx8AcceleratorType for module mode.
	Module910bx8AcceleratorType = "module-910b-8"
	// ModuleA3x16AcceleratorType for module mode.
	ModuleA3x16AcceleratorType = "module-a3-16"
	// ModuleAcceleratorType for module mode.
	ModuleAcceleratorType = "module"
	// ServerType server type value takes Ascend310P-10-dual/Ascend910-32...
	ServerType = "servertype"
	// ServerTypeDual dual card
	ServerTypeDual = "dual"

	// NPU910CardName for judge 910 npu resource.
	NPU910CardName = "huawei.com/Ascend910"
	// NPU910CardNamePre for getting card number.
	NPU910CardNamePre = "Ascend910-"
	// NPU310PCardName for judge 310P npu resource.
	NPU310PCardName = "huawei.com/Ascend310P"
	// NPU310CardName for judge 310 npu resource.
	NPU310CardName = "huawei.com/Ascend310"
	// NPU310CardNamePre for getting card number.
	NPU310CardNamePre = "Ascend310-"
	// NPU310PCardNamePre for getting card number.
	NPU310PCardNamePre = "Ascend310P-"
	// AscendNPUPodRealUse for NPU pod real use cards.
	AscendNPUPodRealUse = "huawei.com/AscendReal"
	// AscendNPUCore for NPU core num, like 56; Records the chip name that the scheduler assigns to the pod.
	AscendNPUCore = "huawei.com/npu-core"
	// Ascend910bName for judge Ascend910b npu resource.
	Ascend910bName = "huawei.com/Ascend910b"

	// Ascend310P device type 310P
	Ascend310P = "Ascend310P"
	// Ascend310 device type 310
	Ascend310 = "Ascend310"
	// Ascend910 device type 910
	Ascend910 = "Ascend910"

	// SegmentEnable for VNPU segment enable flag. Default is "false".
	SegmentEnable = "presetVirtualDevice"

	// UseClusterInfoManager for use cluster info manager, default is true
	UseClusterInfoManager = "useClusterInfoManager"
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
	// NodedNodeHealtyStatuskey  is the key of node healthy status from configmap data of noded
	NodedNodeHealtyStatuskey = "nodedNodeHealtyStatus"
	// NodeSubHealthy means there is some fault on the node which is reported by nodeD, but will not immediately
	// make node unhealthy, this status will prevent new task schduled on this node and reschedule will not consider
	// this node
	NodeSubHealthy = "SubHealthy"
	// NodeUnHealthyByNodeD is the node unhealthy status reported by nodeD configmap,
	// in this case pod will be rescheduling
	NodeUnHealthyByNodeD = "UnHealthy"
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

	// Pod910DeviceKey pod annotation key, for generate 910 hccl rank table
	Pod910DeviceKey = "ascend.kubectl.kubernetes.io/ascend-910-configuration"
	// PodPredicateTime set pod PodPredicateTime for using by device-plugin.
	PodPredicateTime = "predicate-time"
	// NodeNotMeetTopologyWarning node not satisfy the schedulable topology warning.
	NodeNotMeetTopologyWarning = "the npus on this node don't satisfy the schedulable topology"
	// ArgumentError argument nil error.
	ArgumentError = "invalid argument"
	// JobKindKey for define the Job kind:ascend-310P, ascend-910
	JobKindKey = "ring-controller.atlas"
	// JobKind910Value in ring-controller.atlas.
	JobKind910Value = "ascend-910"
	// JobKind310Value in ring-controller.atlas.
	JobKind310Value = "ascend-310"
	// JobKind310PValue 310p ring controller name
	JobKind310PValue = "ascend-310P"
	// JobKind910BValue 910B ring controller name
	JobKind910BValue = "ascend-910b"
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
	scoreWeight                   = 100
	defaultSchedulingTaskNum      = -1
	DeviceInfoForceUpdateInterval = 10
)

const (
	chipKind    = "910"
	ChipTypeKey = "node.kubernetes.io/npu.chip.name"
	// ChipTypeB1 chip type 910B1
	ChipTypeB1 = chipKind + "B1"
	// ChipTypeB2C chip type 910B2C
	ChipTypeB2C = chipKind + "B2C"
	// ChipTypeB2 chip type 910 B2
	ChipTypeB2 = chipKind + "B2"
	// ChipTypeB3 chip type 910B3
	ChipTypeB3 = chipKind + "B3"
	// ChipTypeB4 chip type 910B4
	ChipTypeB4 = chipKind + "B4"
)
