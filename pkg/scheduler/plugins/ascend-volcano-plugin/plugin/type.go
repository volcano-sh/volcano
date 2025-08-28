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
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

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
	Ascend310P = "Ascend310P"
	// Ascend910 910 template name
	Ascend910           = "Ascend910"
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
	ascend910VirtualDevNameReg = regexp.MustCompile("Ascend910-([2-6]|8|10|12|16)c")
	ascend310VirtualDevNameReg = regexp.MustCompile("Ascend310P-(1|2|4)c")
)

// SchedulerJob the plugin define job info
type SchedulerJob struct {
	util.SchedulerJobAttr
	UnscheduledReason
	policyHandler SchedulerPluginNeed
	JobReadyTag   *bool
	SuperPods     map[string][]SuperNode
	Owner         OwnerInfo
}

// OwnerInfo the owner info of job
type OwnerInfo struct {
	v1.OwnerReference
	Annotations map[string]string
	Replicas    *int32
}

// UnscheduledReason the message of pod pending
type UnscheduledReason struct {
	Reason PendingReason
	*sync.Mutex
}

// SuperNode node with SuperPodID
type SuperNode struct {
	Name       string
	SuperPodID int32
}

// VolcanoFrame passed in by the volcano frame.
type VolcanoFrame struct {
	UID             types.UID
	KubeClient      kubernetes.Interface
	informerFactory informers.SharedInformerFactory
	VJobTemplate    map[string]map[string]util.VResource
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

// ScheduleCache the plugin defined caches saving cm data
type ScheduleCache struct {
	// special, name, value
	Names, Namespaces map[string]string
	Data              map[string]map[string]string
}

// ScheduleEnv for job scheduler context.
type ScheduleEnv struct {
	JobScheduleInfoRecorder
	ClusterCache
	FrameAttr   VolcanoFrame
	OutputCache ScheduleCache
}

// ClusterCache cluster env cache
// some cluster date needed when job scheduling, if add other cluster data, should add in this struct
type ClusterCache struct {
	Jobs         map[api.JobID]SchedulerJob
	Nodes        map[string]NPUNode
	Tors         *TorList
	SuperPodInfo *SuperPodInfo
}

// JobScheduleInfoRecorder some info need recorded in job scheduling
type JobScheduleInfoRecorder struct {
	// PodScheduleFlag key is job uid, value is job schedule type, true is pod scheduling, false is job scheduling
	PodScheduleFlag map[api.JobID]bool
	// ResetCMSetFlag flag to record rescheduling job has been set reset cm
	ResetCMSetFlag map[api.JobID]struct{}
	// PendingMessage record different job pending reason and unschedulable nodes
	PendingMessage map[api.JobID]PendingReason
	// ServerListRecordFlag flag to record tor affinity job has been record server list to logs
	ServerListRecordFlag map[api.JobID]struct{}
}

// PendingReason pod pending reason  type. key is pending reason, value is node name
type PendingReason map[string]sets.String

// SuperPodInfo cache super pod info for pod rescheduling
type SuperPodInfo struct {
	SuperPodReschdInfo        map[api.JobID]map[string][]SuperNode // cache super pod re-schd info
	SuperPodFaultTaskNodes    map[api.JobID][]string               // cache fault task nodes info
	SuperPodMapFaultTaskNodes map[api.JobID]map[string]string      // cache task and nodes for stage2
}

// ScheduleHandler information for the current plugin
type ScheduleHandler struct {
	NPUPlugins    sets.String
	PolicyBuilder PolicyBuilder
	FaultHandle   FaultHandler
	ScheduleEnv
}

// TaskResetInfo record task reset device information
type TaskResetInfo struct {
	RankList            []*TaskDevInfo
	UpdateTime          int64
	RetryTime           int
	FaultFlushing       bool
	GracefulExit        int
	RestartFaultProcess bool
}

// TaskDevInfo is the device info of a task
type TaskDevInfo struct {
	RankId int
	DevFaultInfo
}

// DevFaultInfo is the fault info of device
type DevFaultInfo struct {
	LogicId       int32
	Status        string
	Policy        string
	InitialPolicy string
	ErrorCode     []int64
	ErrorCodeHex  string
}

// NewJobScheduleInfoRecorder new default JobScheduleInfoRecorder
func NewJobScheduleInfoRecorder() JobScheduleInfoRecorder {
	return JobScheduleInfoRecorder{
		PodScheduleFlag:      make(map[api.JobID]bool),
		ResetCMSetFlag:       make(map[api.JobID]struct{}),
		PendingMessage:       make(map[api.JobID]PendingReason),
		ServerListRecordFlag: make(map[api.JobID]struct{}),
	}
}

// NewVolcanoFrame new empty volcano frame
func NewVolcanoFrame() VolcanoFrame {
	return VolcanoFrame{
		ConfigParameters: ConfigParameters{
			StaticParameters: StaticParameters{
				OnceInit:       &sync.Once{},
				IsFirstSession: util.PtrInit(true),
			},
		},
	}
}

// NewSuperPodInfo new empty super pod info
func NewSuperPodInfo() *SuperPodInfo {
	return &SuperPodInfo{
		SuperPodReschdInfo:        map[api.JobID]map[string][]SuperNode{},
		SuperPodFaultTaskNodes:    map[api.JobID][]string{},
		SuperPodMapFaultTaskNodes: map[api.JobID]map[string]string{},
	}
}

// NewClusterCache new empty  cluster cache
func NewClusterCache() ClusterCache {
	return ClusterCache{
		Jobs:         map[api.JobID]SchedulerJob{},
		Nodes:        map[string]NPUNode{},
		SuperPodInfo: NewSuperPodInfo(),
	}
}

func newUnscheduledReason() UnscheduledReason {
	return UnscheduledReason{
		Reason: make(PendingReason),
		Mutex:  &sync.Mutex{},
	}
}
