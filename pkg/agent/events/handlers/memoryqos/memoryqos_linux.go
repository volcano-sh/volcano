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
}
