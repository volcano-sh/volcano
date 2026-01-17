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

package blkioqos

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
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
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewBlkioQoSHandle)
}

type BlkioQoSHandle struct {
	*base.BaseHandle
	cgroupMgr cgroup.CgroupManager
}

func NewBlkioQoSHandle(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &BlkioQoSHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.BlkioQoSFeature),
			Config: config,
		},
		cgroupMgr: cgroupMgr,
	}
}

func (h *BlkioQoSHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}

	// Get blkio weight from pod annotation or ConfigMap default
	blkioWeight := h.getBlkioWeight(podEvent.Pod)
	if blkioWeight == 0 {
		klog.V(4).InfoS("No blkio weight specified for pod, skipping blkio QoS", "pod", klog.KObj(podEvent.Pod))
		return nil
	}

	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupBlkioSubsystem, podEvent.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path(%s), error: %v", podEvent.UID, err)
	}

	cgroupVersion := h.cgroupMgr.GetCgroupVersion()
	var weightFile string
	var weightValue string

	switch cgroupVersion {
	case cgroup.CgroupV1:
		weightFile = path.Join(cgroupPath, cgroup.BlkioWeightFileV1)
		if blkioWeight < 10 {
			blkioWeight = 10
		} else if blkioWeight > 1000 {
			blkioWeight = 1000
		}
		weightValue = strconv.FormatInt(blkioWeight, 10)
	case cgroup.CgroupV2:
		weightFile = path.Join(cgroupPath, cgroup.BlkioWeightFileV2)
		v2Weight := blkioWeight * 10
		if v2Weight < 1 {
			v2Weight = 1
		} else if v2Weight > 10000 {
			v2Weight = 10000
		}
		weightValue = strconv.FormatInt(v2Weight, 10)
	default:
		return fmt.Errorf("unsupported cgroup version: %s", cgroupVersion)
	}

	err = utils.UpdatePodCgroup(weightFile, []byte(weightValue))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup file not exist", "cgroupFile", weightFile)
			return nil
		}
		return err
	}
	klog.InfoS("Successfully set blkio weight to cgroup file", "weight", weightValue, "cgroupFile", weightFile, "pod", klog.KObj(podEvent.Pod))

	return nil
}

// getBlkioWeight returns the blkio weight from pod annotation
// Returns 0 if no weight is specified (which means blkio QoS should be skipped)
func (h *BlkioQoSHandle) getBlkioWeight(pod *corev1.Pod) int64 {
	if pod == nil || pod.Annotations == nil {
		return 0
	}

	weightStr, found := pod.Annotations[apis.BlkioWeightAnnotationKey]
	if !found || weightStr == "" {
		return 0
	}

	weight, err := strconv.ParseInt(weightStr, 10, 64)
	if err != nil {
		klog.Warningf("Invalid blkio-weight annotation value '%s' for pod %s/%s: %v", weightStr, pod.Namespace, pod.Name, err)
		return 0
	}

	if weight <= 0 {
		klog.Warningf("Invalid blkio-weight annotation value '%s' for pod %s/%s: must be positive", weightStr, pod.Namespace, pod.Name)
		return 0
	}

	return weight
}