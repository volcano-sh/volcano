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
Package rescheduling is using for HuaWei Ascend pin affinity schedule utilities.
*/
package rescheduling

import (
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

var reSchedulerCache *DealReSchedulerCache

const (
	// RePropertyName name specifying re-scheduler cm
	RePropertyName = "re-scheduling"
	// ReschedulingReasonKey is used to record the reason of rescheduling
	ReschedulingReasonKey = "rescheduling-reason"
	// CmName Name of ReSchedulerConfigmap
	CmName = "vcjob-fault-npu-cm"
	// CmNameSpace Namespace of ReSchedulerConfigmap
	CmNameSpace = "volcano-system"
	// RescheduleReasonCmName Name of RescheduleReasonConfigmap
	RescheduleReasonCmName = "job-reschedule-reason"
	// RescheduleReasonCmNamespace Namespace of RescheduleReasonConfigmap
	RescheduleReasonCmNamespace = "mindx-dl"

	// JobRescheduleLabelKey key word of re-scheduling configuration
	JobRescheduleLabelKey = "fault-scheduling"
	// JobGraceRescheduleLabelValue Grace delete reschedule job, possible value of re-scheduling configuration
	JobGraceRescheduleLabelValue = "grace"
	// JobForceRescheduleLabelValue Force delete reschedule job, possible value of re-scheduling configuration
	JobForceRescheduleLabelValue = "force"
	// JobOffRescheduleLabelValue not delete reschedule job, possible value of re-scheduling configuration
	JobOffRescheduleLabelValue = "off"
	// GraceOverTimeKey for GraceOverTime config by user
	GraceOverTimeKey = "grace-over-time"
	// ElasticSchedulingKey for distinguishing whether a job is enabled with elastic scheduling
	ElasticSchedulingKey = "elastic-scheduling"
	// JobOnElasticScheduling job enabled with elastic scheduling
	JobOnElasticScheduling = "on"
	// JobOffElasticScheduling job not enabled with elastic scheduling
	JobOffElasticScheduling = "off"

	nodeDEnableKey      = "nodeDEnable"
	nodeDEnableOnValue  = "on"
	nodeDEnableOffValue = "off"

	podRankIndex = "hccl/rankIndex"

	// CmFaultNodeKind key in configmap which saves the FaultNode cache
	CmFaultNodeKind = "fault-node"
	// CmFaultJob910bx2Kind key in configmap which saves the 910bx2 FaultJob cache
	CmFaultJob910bx2Kind = "fault-job-910bx2"
	// CmFaultJob910x8Kind key in configmap which saves the 910x8 FaultJob cache
	CmFaultJob910x8Kind = "fault-job-910x8"
	// CmJobRemainRetryTimes key in configmap which saves remain retry times of job
	CmJobRemainRetryTimes = "remain-retry-times"
	// MaxRescheduleRecordsNum the upper limit of the cm kept reschedule records, oldest record will be deleted
	// if record more than MaxRescheduleRecordsNum records
	MaxRescheduleRecordsNum = 10
	// MaxKbOfRescheduleRecords the upper limit words of the cm kept reschedule records
	MaxKbOfRescheduleRecords = 950 * 1024
	// CmJobRescheduleReasonsKey keeping recent MaxRescheduleRecordsNum records of rescheduling
	CmJobRescheduleReasonsKey = "recent-reschedule-records"
	// CmNodeRankTimeMapKind record map jobUID rankIndex node and times of occurrence
	CmNodeRankTimeMapKind = "node-rankIndex-Occurrence"
	// CmCheckCode Check code key
	CmCheckCode = "checkCode"

	// CmFaultJob key in configmap which saves the FaultJob cache
	CmFaultJob = "fault-job"

	// DefaultGraceOverTime time interval for grace delete
	DefaultGraceOverTime = 900
	minGraceOverTime     = 2
	maxGraceOverTime     = 3600
	maxIntervalTime      = 300
	maxRankIndex         = 1000

	// PublicFaultType represents a PublicFault fault type
	PublicFaultType = "PublicFault"
	// CardHealthy represents a healthy card
	CardHealthy = "Healthy"
	// CardUnhealthy represents an unhealthy card
	CardUnhealthy = "Unhealthy"
	// CardNetworkUnhealthy represents a network unhealthy card
	CardNetworkUnhealthy = "NetworkUnhealthy"
	// NodeHealthy represents node is available for scheduling
	NodeHealthy = "Healthy"
	// NodeUnhealthy represents node is unhealthy
	NodeUnhealthy = "NodeUnhealthy"
	// NodeCardUnhealthy represents node is unhealthy because of the card is unhealthy
	NodeCardUnhealthy = "CardUnhealthy"
	// NodeCardNetworkUnhealthy represents node is unhealthy because of card is network unhealthy
	NodeCardNetworkUnhealthy = "CardNetworkUnhealthy"
	// NoFaultJobsErr none fault jobs
	NoFaultJobsErr = "none fault jobs to be restarted in cache"
	// JobRecovery Name of cm for recovery
	JobRecovery = "job-recovery"
	// DeviceFaultCmKey the key of DeviceFault info
	DeviceFaultCmKey = "huawei.com/Ascend910-Fault"
	// PodFailed the state of failed pod
	PodFailed = "pod-failed"
	// PodHealthy the state of healthy pod
	PodHealthy = "pod-healthy"

	// FaultRetryTimesKey key of fault-retry-times label
	FaultRetryTimesKey = "fault-retry-times"
)

const (
	// PreSeparateNPU fault type waiting user check
	PreSeparateNPU = "PreSeparateNPU"
	// NotHandleFault fault type not handle
	NotHandleFault = "NotHandleFault"
	// NodeFaultCode fault type nodeUnhealthy
	NodeFaultCode = "heartbeatTimeOut"
	// SubHealthFault subHealth code
	SubHealthFault = "SubHealthFault"
)

const (
	getNoneJobsErr = "get none jobs"
	pendingTimes   = 12
	spPendingTimes = 6
	// SuperPodAnnoKey annotation key of super pod
	SuperPodAnnoKey          = "sp-block"
	singleThreadDeletePodNum = 200
)

const (
	taskFaultKey         = "fault-type"
	softwareKey          = "software"
	linkDownFaultCode    = "81078603"
	linkDownFaultTimeout = 20
)

// ReScheduler object for re-scheduling
type ReScheduler struct {
	*DealReSchedulerCache
	GraceDeleteTime int64
	Jobs            map[api.JobID]plugin.SchedulerJob
	Nodes           map[string]plugin.NPUNode
	isFirstSession  *bool
}

// FaultNodeInfoToCm fault node info to cm
type FaultNodeInfoToCm struct {
	FaultDeviceList     []FaultDeviceList
	NodeName            string
	UnhealthyNPU        []string
	NetworkUnhealthyNPU []string
	NodeDEnable         bool
	NodeHealthState     string
	UpdateTime          int64
}

// DealReSchedulerCache object with method for re-scheduler cache
type DealReSchedulerCache struct {
	FaultNodes                 map[string]*FaultNode
	FaultJobs                  map[api.JobID]*FaultJob
	JobRemainRetryTimes        map[api.JobID]*RemainRetryTimes
	JobRecentRescheduleRecords map[api.JobID]*RescheduleReason
}

// RescheduleReason shows the reason of this job rescheduling
type RescheduleReason struct {
	// JobID the job id of this record
	JobID api.JobID
	// TotalRescheduleTimes to show how many times reschedule has happened since job created
	TotalRescheduleTimes int
	// RescheduleRecords keep recent MaxRescheduleRecordsNum records of rescheduling
	RescheduleRecords []RescheduleRecord
	// AdditionalInfo is used to provide additional information, such as for length concern reduce some records
	AdditionalInfo string `json:",omitempty"`
}

// RescheduleRecord will records job rescheduling records
type RescheduleRecord struct {
	// LogFileFormatTime is the formated time, to make it convenient to read and locate log
	LogFileFormatTime string
	// RescheduleTimeStamp time.now.unix() indicates when the rescheduling happened
	RescheduleTimeStamp int64
	// ReasonOfTask record the reason of this rescheduling of task
	ReasonOfTask []RescheduleTaskReason
}

// RescheduleTaskReason record the reason of this rescheduling of task
type RescheduleTaskReason struct {
	// RescheduleReason the fault type of this rescheduling
	RescheduleReason string
	// PodName the fault task caused this rescheduling
	PodName string
	// NodeName the fault node caused this rescheduling
	NodeName string
	// NodeRankIndex the rank index of the fault task
	NodeRankIndex string
}

// RemainRetryTimes remained retry times
type RemainRetryTimes struct {
	UUID  types.UID
	Times int
}

// FaultCard card object for re-scheduling
type FaultCard struct {
	IsFaultCard bool
	NPUName     string
	FaultType   string
}

// FaultNode node object for re-scheduling
type FaultNode struct {
	SuperPodID              int32
	NodeName                string
	NPUName                 string
	FaultDeviceList         []FaultDeviceList
	UpdateTime              int64
	UnhealthyNPU            []string
	NetworkUnhealthyNPU     []string
	IsFaultNode             bool
	NodeDEnable             bool
	NodeHealthState         string
	FaultCards              []FaultCard
	HasSwitchSubHealthFault bool
	HasCardSubHealthFault   bool
	LinkDownTime            int64
}

// SimpleFNodeInfo simple fault node info
type SimpleFNodeInfo struct {
	NodeName                string
	IsFaultNode             bool
	HasCardSubHealthFault   bool
	HasSwitchSubHealthFault bool
	NodeHealthState         string
}

// FaultDeviceList is the fault reason of card
type FaultDeviceList struct {
	FaultType            string `json:"fault_type"`
	NPUName              string `json:"npu_name"`
	FaultLevel           string `json:"fault_level"`
	FaultHandling        string `json:"fault_handling"`
	LargeModelFaultLevel string `json:"large_model_fault_level"`
	FaultCode            string `json:"fault_code"`
}

// FaultTask object dealing with node for rescheduling
type FaultTask struct {
	Reason             []FaultReasonList
	FaultTime          int64
	RelationFault      string
	IsFaultTask        bool
	IsFaultRetryEnable bool
	HasSubHealthFault  bool
	IsSoftwareFault    bool
	TaskUID            api.TaskID
	TaskName           string
	TaskNamespace      string
	NodeName           string
	NodeRankIndex      string
	UseCardName        []string
	PodCreateTime      int64
	faultType          string
}

// miniFaultTask struct for print fTask important infos to logs
type miniFaultTask struct {
	TaskName      string
	NodeName      string
	NodeRankIndex string
	UseCardName   []string
	Reason        []FaultReasonList
	FaultType     string
}

// miniFaultJob struct for print fJobs important infos to logs
type miniFaultJob struct {
	ReferenceName string
	FaultTasks    []miniFaultTask
}

// FaultReasonList node Fault Device List
type FaultReasonList struct {
	NodeName      string `json:"node_name"`
	TaskName      string `json:"task_name"`
	FaultRankList []string
	FaultDeviceList
}

// FaultJob job object for re-scheduling
type FaultJob struct {
	ReScheduleKey      string // values taken off/grace/force
	RescheduleTime     int64
	SubHealthyStrategy string
	IsSubHealthFault   bool
	PendingSessionNum  int
	IsFaultJob         bool
	JobName            string
	JobUID             api.JobID
	JobNamespace       string
	SuperPods          map[string][]plugin.SuperNode
	FaultTasks         []FaultTask
	UpdateTime         int64
	FaultTypes         []string
	DeleteExecutedFlag bool
	ElasticScheduling  string
	ReferenceName      string
	FaultRetryTimes    int
	faultReason        string
	UUID               types.UID
}

type deletePodInfo struct {
	isMasterFault bool
	superPod      bool
	ids           []string
	reason        string
}
