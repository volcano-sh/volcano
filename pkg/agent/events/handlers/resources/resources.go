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

package resources

import (
	"fmt"
	"os"
	"path"
	"strconv"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewResources)
}

type ResourcesHandle struct {
	*base.BaseHandle
	cgroupMgr              cgroup.CgroupManager
	getNodeFunc            utilnode.ActiveNode
	memoryThrottlingFactor float64
}

func NewResources(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &ResourcesHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.ResourcesFeature),
			Config: config,
			Active: true,
		},
		cgroupMgr:              cgroupMgr,
		getNodeFunc:            config.GetNode,
		memoryThrottlingFactor: config.GenericConfiguration.MemoryThrottlingFactor,
	}
}

func (r *ResourcesHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}

	if !allowedUseExtRes(podEvent.QoSLevel) {
		return nil
	}

	node, err := r.getNodeFunc()
	if err != nil {
		klog.ErrorS(err, "Failed to get node")
		return err
	}

	resources := utilpod.CalculateExtendResources(podEvent.Pod, node, r.memoryThrottlingFactor)
	var errs []error
	// set container and pod level cgroup.
	for _, cr := range resources {
		cgroupPath, err := r.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cr.CgroupSubSystem, podEvent.UID)
		if err != nil {
			klog.ErrorS(err, "Failed to get pod cgroup", "pod", klog.KObj(podEvent.Pod), "subSystem", cr.CgroupSubSystem)
			errs = append(errs, err)
		}

		filePath := path.Join(cgroupPath, cr.ContainerID, cr.SubPath)
		err = utils.UpdateFile(filePath, []byte(strconv.FormatInt(cr.Value, 10)))
		if os.IsNotExist(err) {
			klog.InfoS("Cgroup file not existed", "filePath", filePath)
			continue
		}

		if err != nil {
			errs = append(errs, err)
			klog.ErrorS(err, "Failed to set cgroup", "path", filePath, "pod", klog.KObj(podEvent.Pod))
			continue
		}
		klog.InfoS("Successfully set cpu and memory cgroup", "path", filePath, "pod", klog.KObj(podEvent.Pod))
	}
	return utilerrors.NewAggregate(errs)
}

// allowedUseExtRes defines what qos levels can use extension resources,
// currently only qos level QosLevelLS and QosLevelBE can use.
func allowedUseExtRes(qosLevel int64) bool {
	return qosLevel <= 1
}
