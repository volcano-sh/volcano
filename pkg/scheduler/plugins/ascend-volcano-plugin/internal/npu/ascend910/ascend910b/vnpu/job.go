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
Package vnpu is using for Ascend vnpu affinity schedule.
*/
package vnpu

import (
	"errors"
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/vnpu"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// InitVNPU init map of vnpu tmp
func (tp *virtual910NPU) InitVNPU() {
	if tp == nil {
		klog.V(util.LogDebugLev).Infof("InitVNPU failed:%s", util.ArgumentError)
		return
	}
	tp.vHandle = &vnpu.VirtualNPU{
		DynamicVNPU: vnpu.DynamicVNPU{
			DowngradeCache: make(map[string][]string, util.MapInitNum),
		},
	}
}

func (tp *virtual910NPU) checkDyVJobReq() error {
	if !tp.IsVJob() {
		return fmt.Errorf("%s not VirtualNPU job", tp.Name)
	}
	if tp.vHandle.StaticByConf {
		return fmt.Errorf("volcano configuration %s true, only support static vnpu", util.SegmentEnable)
	}
	for _, vT := range tp.Tasks {
		if tp.checkDyVJobReqByTemp(vT) {
			continue
		}
		return fmt.Errorf("%s req err %d", vT.Name, vT.ReqNPUNum)
	}
	return nil
}

func (tp *virtual910NPU) checkDyVJobReqByTemp(vT util.NPUTask) bool {
	switch tp.vHandle.VT.Temp {
	case plugin.ChipTypeB1:
		return checkB1DyJobRequire(vT)
	case plugin.ChipTypeB2C, plugin.ChipTypeB2:
		return checkB2AndB2CDyJobRequire(vT)
	case plugin.ChipTypeB3:
		return checkB3DyJobRequire(vT)
	case plugin.ChipTypeB4:
		return checkB4DyJobRequire(vT)
	default:
		return true
	}
}

func checkB1DyJobRequire(vT util.NPUTask) bool {
	return vT.ReqNPUNum == util.CoreNum6 || vT.ReqNPUNum == util.CoreNum12 || vT.ReqNPUNum%util.CoreNum25 == 0
}

func checkB2AndB2CDyJobRequire(vT util.NPUTask) bool {
	return vT.ReqNPUNum == util.CoreNum6 || vT.ReqNPUNum == util.CoreNum12 || vT.ReqNPUNum%util.CoreNum24 == 0
}

func checkB3DyJobRequire(vT util.NPUTask) bool {
	return vT.ReqNPUNum == util.CoreNum5 || vT.ReqNPUNum == util.CoreNum10 || vT.ReqNPUNum%util.CoreNum20 == 0
}

func checkB4DyJobRequire(vT util.NPUTask) bool {
	return vT.ReqNPUNum == util.CoreNum5 || vT.ReqNPUNum == util.CoreNum10 || vT.ReqNPUNum%util.CoreNum20 == 0
}

func (tp *virtual910NPU) validDyVNPUTaskDVPPLabel(vT util.NPUTask) error {
	if !vT.IsVNPUTask() {
		return errors.New("not vNPU task")
	}

	dvppValue := vnpu.GetVNPUTaskDVPP(vT)
	cpuLevel := vnpu.GetVNPUTaskCpuLevel(vT)
	if !(tp.vHandle.VT.Temp == plugin.ChipTypeB4 && vT.ReqNPUNum == util.NPUIndex10) &&
		cpuLevel != plugin.AscendVNPULevelLow {
		return fmt.Errorf("%s req %d ai-core and npu is %s, but cpu level is:%s", vT.Name, vT.ReqNPUNum,
			tp.vHandle.VT.Temp, cpuLevel)
	}
	if !(tp.vHandle.VT.Temp == plugin.ChipTypeB4 && vT.ReqNPUNum == util.NPUIndex10) &&
		dvppValue != plugin.AscendDVPPEnabledNull {
		return fmt.Errorf("%s req %d ai-core and npu is %s, but dvpp label is:%s", vT.Name, vT.ReqNPUNum,
			tp.vHandle.VT.Temp, dvppValue)
	}
	return nil
}

func (tp *virtual910NPU) validDyVNPUJobLabel() error {
	if !tp.IsVJob() {
		return fmt.Errorf("%s not VirtualNPU job", tp.Name)
	}
	for _, vT := range tp.Tasks {
		if tErr := tp.validDyVNPUTaskDVPPLabel(vT); tErr != nil {
			return tErr
		}
	}
	return nil
}

// ValidDyVNPUJob valid dynamic cut job
func (tp *virtual910NPU) ValidDyVNPUJob() *api.ValidateResult {
	if tp.Status == util.PodGroupRunning {
		klog.V(util.LogDebugLev).Infof("%s's pg is running", tp.ComJob.Name)
		return nil
	}
	if reqErr := tp.checkDyVJobReq(); reqErr != nil {
		return &api.ValidateResult{Pass: false, Reason: reqErr.Error(), Message: reqErr.Error()}
	}
	if labelErr := tp.validDyVNPUJobLabel(); labelErr != nil {
		return &api.ValidateResult{Pass: false, Reason: labelErr.Error(), Message: labelErr.Error()}
	}
	return nil
}
