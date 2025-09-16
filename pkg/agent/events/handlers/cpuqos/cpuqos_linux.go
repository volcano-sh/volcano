/*
Copyright 2024 The Volcano Authors.

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

package cpuqos

import (
	"errors"
	"fmt"
	"os"
	"path"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils"
	calutils "volcano.sh/volcano/pkg/agent/utils/calculate"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewCPUQoSHandle)
}

type CPUQoSHandle struct {
	*base.BaseHandle
	cgroupMgr cgroup.CgroupManager
}

func NewCPUQoSHandle(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &CPUQoSHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.CPUQoSFeature),
			Config: config,
		},
		cgroupMgr: cgroupMgr,
	}
}

func (h *CPUQoSHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}

	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupCpuSubsystem, podEvent.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podEvent.UID, err)
	}

	cgroupVersion := h.cgroupMgr.GetCgroupVersion()
	switch cgroupVersion {
	case cgroup.CgroupV1:
		qosLevelFile := path.Join(cgroupPath, cgroup.CPUQoSLevelFile)
		qosLevelByte := []byte(fmt.Sprintf("%d", podEvent.QoSLevel))

		err = utils.UpdatePodCgroup(qosLevelFile, qosLevelByte)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup file not exist", "cgroupFile", qosLevelFile)
				return nil
			}
			return err
		}
		klog.InfoS("Successfully set cpu qos level to cgroup file", "qosLevel", podEvent.QoSLevel, "cgroupFile", qosLevelFile)

		return nil
	case cgroup.CgroupV2:
		err = h.setCPUWeightAndQuota(cgroupPath, podEvent.QoSLevel)
		if err != nil {
			return err
		}
		klog.InfoS("Successfully set cpu qos level to cgroup file", "qosLevel", podEvent.QoSLevel)
		return nil

	default:
		return fmt.Errorf("invalid cgroup version: %s", cgroupVersion)
	}
}

func (h *CPUQoSHandle) setCPUWeightAndQuota(cgroupPath string, qosLevel int64) error {
	if qosLevel == -1 {
		cpuIdleFile := path.Join(cgroupPath, cgroup.CPUIdleFileV2)
		cpuIdleByte := []byte(fmt.Sprint("1"))

		err := utils.UpdatePodCgroup(cpuIdleFile, cpuIdleByte)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup cpu idle file not exist", "cgroupFile", cpuIdleFile)
			} else {
				return err
			}
		}
	} else {
		cpuWeight := calutils.CalculateCPUWeightFromQoSLevel(qosLevel)

		cpuWeightFile := path.Join(cgroupPath, cgroup.CPUWeightFileV2)
		cpuWeightByte := []byte(fmt.Sprintf("%d", cpuWeight))

		err := utils.UpdatePodCgroup(cpuWeightFile, cpuWeightByte)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup cpu weight file not exist", "cgroupFile", cpuWeightFile)
			} else {
				return err
			}
		}

		if cpuQuota := calutils.CalculateCPUQuotaFromQoSLevel(qosLevel); cpuQuota > 0 {
			cpuMaxFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFileV2)
			cpuMaxByte := []byte(fmt.Sprintf("%d", cpuQuota))

			err = utils.UpdatePodCgroup(cpuMaxFile, cpuMaxByte)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					klog.InfoS("Cgroup cpu quota file not exist", "cgroupFile", cpuMaxFile)
				} else {
					return err
				}
			}
		}
	}

	return nil
}
