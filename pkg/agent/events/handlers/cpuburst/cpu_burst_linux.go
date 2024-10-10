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

package cpuburst

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/file"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewCPUBurst)
}

type CPUBurstHandle struct {
	*base.BaseHandle
	cgroupMgr   cgroup.CgroupManager
	podInformer v1.PodInformer
}

func NewCPUBurst(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &CPUBurstHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.CPUBurstFeature),
			Config: config,
		},
		cgroupMgr:   cgroupMgr,
		podInformer: config.InformerFactory.K8SInformerFactory.Core().V1().Pods(),
	}
}

func (c *CPUBurstHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}
	pod := podEvent.Pod
	latestPod, err := c.podInformer.Lister().Pods(pod.Namespace).Get(pod.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to get pod from lister")
	} else {
		pod = latestPod
	}
	str, exists := pod.Annotations[EnabledKey]
	if !exists {
		return nil
	}
	enable, err := strconv.ParseBool(str)
	if err != nil || !enable {
		return nil
	}

	cgroupPath, err := c.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupCpuSubsystem, podEvent.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podEvent.UID, err)
	}

	quotaBurstTime := getCPUBurstTime(pod)
	podBurstTime := int64(0)
	err = filepath.WalkDir(cgroupPath, walkFunc(cgroupPath, quotaBurstTime, &podBurstTime))
	if err != nil {
		return fmt.Errorf("failed to set container cpu quota burst time, err: %v", err)
	}

	// last set pod cgroup cpu quota burst.
	podQuotaTotalFile := filepath.Join(cgroupPath, cgroup.CPUQuotaTotalFile)
	value, err := file.ReadIntFromFile(podQuotaTotalFile)
	if err != nil {
		return fmt.Errorf("failed to get pod cpu total quota time, err: %v,path: %s", err, podQuotaTotalFile)
	}
	if value == fixedQuotaValue {
		return nil
	}
	podQuotaBurstFile := filepath.Join(cgroupPath, cgroup.CPUQuotaBurstFile)
	err = utils.UpdateFile(podQuotaBurstFile, []byte(strconv.FormatInt(podBurstTime, 10)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.ErrorS(nil, "CPU Burst is not supported", "cgroupFile", podQuotaBurstFile)
			return nil
		}
		return err
	}

	klog.InfoS("Successfully set pod cpu quota burst time", "path", podQuotaBurstFile, "quotaBurst", podBurstTime, "pod", klog.KObj(pod))
	return nil
}

func walkFunc(cgroupPath string, quotaBurstTime int64, podBurstTime *int64) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// We will set pod cgroup later.
		if path == cgroupPath {
			return nil
		}
		if d == nil || !d.IsDir() {
			return nil
		}
		quotaTotalFile := filepath.Join(path, cgroup.CPUQuotaTotalFile)
		quotaTotal, err := file.ReadIntFromFile(quotaTotalFile)
		if err != nil {
			return fmt.Errorf("failed to get container cpu total quota time, err: %v, path: %s", err, quotaTotalFile)
		}
		if quotaTotal == fixedQuotaValue {
			return nil
		}

		actualBurst := quotaBurstTime
		if quotaBurstTime > quotaTotal {
			klog.ErrorS(nil, "The quota burst time is greater than quota total, use quota total as burst time", "quotaBurst", quotaBurstTime, "quoTotal", quotaTotal)
			actualBurst = quotaTotal
		}
		if quotaBurstTime == 0 {
			actualBurst = quotaTotal
		}
		*podBurstTime += actualBurst
		quotaBurstFile := filepath.Join(path, cgroup.CPUQuotaBurstFile)
		err = utils.UpdateFile(quotaBurstFile, []byte(strconv.FormatInt(actualBurst, 10)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.ErrorS(nil, "CPU Burst is not supported", "cgroupFile", quotaBurstFile)
				return nil
			}
			return err
		}

		klog.InfoS("Successfully set container cpu burst time", "path", quotaBurstFile, "quotaTotal", quotaTotal, "quotaBurst", actualBurst)
		return nil
	}
}

func getCPUBurstTime(pod *corev1.Pod) int64 {
	var quotaBurstTime int64
	str, exists := pod.Annotations[QuotaTimeKey]
	if !exists {
		return quotaBurstTime
	}
	value, err := strconv.ParseInt(str, 10, 64)
	if err != nil || value <= 0 {
		klog.ErrorS(err, "Invalid quota burst time, use default containers' quota time", "value", str)
		return quotaBurstTime
	}
	quotaBurstTime = int64(value)
	return quotaBurstTime
}
