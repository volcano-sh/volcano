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

package memoryqos

import (
	"errors"
	"fmt"
	"os"
	"path"

	"k8s.io/klog/v2"
	calutils "volcano.sh/volcano/pkg/agent/utils/calculate"

	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewMemoryQoSHandle)
}

type MemoryQoSHandle struct {
	*base.BaseHandle
	cgroupMgr cgroup.CgroupManager
}

func NewMemoryQoSHandle(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &MemoryQoSHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.MemoryQoSFeature),
			Config: config,
		},
		cgroupMgr: cgroupMgr,
	}
}

func (h *MemoryQoSHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}

	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupMemorySubsystem, podEvent.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podEvent.UID, err)
	}

	cgroupVersion := h.cgroupMgr.GetCgroupVersion()
	switch cgroupVersion {
	case cgroup.CgroupV1:
		qosLevelFile := path.Join(cgroupPath, cgroup.MemoryQoSLevelFile)
		qosLevel := []byte(fmt.Sprintf("%d", extension.NormalizeQosLevel(podEvent.QoSLevel)))

		err = utils.UpdatePodCgroup(qosLevelFile, qosLevel)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup file not existed", "cgroupFile", qosLevelFile)
				return nil
			}
			return err
		}

		klog.InfoS("Successfully set memory qos level to cgroup file", "qosLevel", qosLevel, "cgroupFile", qosLevelFile)
		return nil
	case cgroup.CgroupV2:
		err = h.setMemoryQoSV2(cgroupPath, podEvent.QoSLevel)
		if err != nil {
			return err
		}
		klog.InfoS("Successfully set memory qos level to cgroup file", "qosLevel", podEvent.QoSLevel)
		return nil
	default:
		return fmt.Errorf("invalid cgroup version: %s", cgroupVersion)
	}
}

func (h *MemoryQoSHandle) setMemoryQoSV2(cgroupPath string, qosLevel int64) error {
	// Set memory.high (soft limit)
	memoryHigh := calutils.CalculateMemoryHighFromQoSLevel(qosLevel)
	memoryHighFile := path.Join(cgroupPath, cgroup.MemoryHighFileV2)
	var memoryHighByte []byte
	if memoryHigh == 0 {
		memoryHighByte = []byte("max") // cgroup v2 uses "max" for no limit
	} else {
		memoryHighByte = []byte(fmt.Sprintf("%d", memoryHigh))
	}

	err := utils.UpdatePodCgroup(memoryHighFile, memoryHighByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup memory high file not exist", "cgroupFile", memoryHighFile)
		} else {
			return err
		}
	}

	// Set memory.low (minimum guarantee)
	memoryLow := calutils.CalculateMemoryLowFromQoSLevel(qosLevel)
	memoryLowFile := path.Join(cgroupPath, cgroup.MemoryLowFileV2)
	memoryLowByte := []byte(fmt.Sprintf("%d", memoryLow))

	err = utils.UpdatePodCgroup(memoryLowFile, memoryLowByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup memory low file not exist", "cgroupFile", memoryLowFile)
		} else {
			return err
		}
	}

	// Set memory.min (minimum reservation)
	memoryMin := calutils.CalculateMemoryMinFromQoSLevel(qosLevel)
	memoryMinFile := path.Join(cgroupPath, cgroup.MemoryMinFileV2)
	memoryMinByte := []byte(fmt.Sprintf("%d", memoryMin))

	err = utils.UpdatePodCgroup(memoryMinFile, memoryMinByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup memory min file not exist", "cgroupFile", memoryMinFile)
		} else {
			return err
		}
	}

	klog.InfoS("Successfully set memory QoS for cgroup v2", "qosLevel", qosLevel, "memoryHigh", string(memoryHighByte), "memoryLow", memoryLow, "memoryMin", memoryMin)
	return nil
}
