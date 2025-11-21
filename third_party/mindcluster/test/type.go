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
Package test is using for HuaWei Ascend pin scheduling test.
*/
package test

import (
	"k8s.io/api/core/v1"
)

const (
	npuIndex2 = 2
	npuIndex3 = 3
	// NPUIndex4 for re-scheduler tests
	NPUIndex4 = 4
	// NPUIndex5 for re-scheduler tests
	NPUIndex5 = 5
	// NPUIndex8 for re-scheduler tests
	NPUIndex8 = 8
	// NPUHexKilo for const 1000,volcano frame used.
	NPUHexKilo     = 1000
	npuCodex200    = 160
	fakeNpuCodeNum = "160"
	podRankIndex   = "hccl/rankIndex"
	// NPU910CardName 910 card name
	NPU910CardName = "huawei.com/Ascend910"
	// AscendNPUPodRealUse for NPU pod real use cards.
	AscendNPUPodRealUse = "huawei.com/AscendReal"
	// FakeUpdateTime fake update time for test
	FakeUpdateTime = int64(11110)
	// FakeJobName fake job namespace/name
	FakeJobName = "vcjob/job"
	// FakeTaskName0 fake task name
	FakeTaskName0 = "vcjob-pod0"
	// FakeTaskName1 fake task name
	FakeTaskName1              = "vcjob-pod1"
	kubeGroupNameAnnotationKey = "scheduling.k8s.io/group-name"
	fakeNodeNum                = 16
	fakeIpPrefix               = "1.1.0."
)

const (
	overTimeKey                = "grace-over-time"
	overTimeValue              = "900"
	presetVirtualDeviceKey     = "presetVirtualDevice"
	presetVirtualDeviceValue   = "false"
	nslbVersionKey             = "nslb-version"
	nslbVersionValue           = "2.0"
	sharedTorNumKey            = "shared-tor-num"
	sharedTorNumValue          = "2"
	useClusterInfoManagerKey   = "useClusterInfoManager"
	useClusterInfoManagerValue = "true"
	superPodSizeKey            = "super-pod-size"
	superPodSizeValue          = "48"
	reserveNodesKey            = "reserve-nodes"
	reserveNodesValue          = "2"
	fakeResourceNum            = "8000"
	fakeNPUNum                 = "8"
	acceleratorKey             = "accelerator"
	acceleratorValue           = "huawei-Ascend910"
	npuCoreName                = "huawei.com/npu-core"
	fakeNpuCoreStr             = "0-vir05_1c_16g"
	fakeWholeCardStr           = "0,1,2,3"
	serverTypeKey              = "servertype"
	fake910ServerType          = "Ascend910B-20"
)

const (
	defaultNS = "default"
	taskName1 = "task1"
	taskName2 = "task2"
	taskUid1  = "task-uid1"
	taskUid2  = "task-uid2"
	podName1  = "pod1"
	podName2  = "pod2"
)

const (
	chipTypeKey    = "node.kubernetes.io/npu.chip.name"
	fakeChipType   = "B3"
	fakeChipName   = "910"
	torAffinityKey = "tor-affinity"
	normalJob      = "normal"
)

// NPUPod test NPU pod struct
type NPUPod struct {
	Namespace, Name, NodeName, GroupName string
	Phase                                v1.PodPhase
	ReqSource                            v1.ResourceList
	Labels, Selector                     map[string]string
}

// NPUNode test NPU node struct
type NPUNode struct {
	Name                         string
	Capacity, Allocatable        v1.ResourceList
	Labels, Selector, Annotation map[string]string
	Other                        map[string]interface{}
}
