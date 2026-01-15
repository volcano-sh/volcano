/*
Copyright 2026 The Volcano Authors.

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

package memoryqosv2

import (
	"encoding/json"
	"path"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	configv1alpha1 "volcano.sh/apis/pkg/apis/config/v1alpha1"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	podutils "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewMemoryQoSV2Handle)
}

// MemoryQoSV2Handle handles memory QoS events for pods, specifically designed for cgroup v2 environments.
// It manages memory settings like High, Low, and Min based on pod configurations.
type MemoryQoSV2Handle struct {
	*base.BaseHandle
	cgroupMgr cgroup.CgroupManager
	podLister corelisters.PodLister
	recorder  record.EventRecorder
}

// NewMemoryQoSV2Handle initializes and returns a new MemoryQoSV2Handle.
func NewMemoryQoSV2Handle(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &MemoryQoSV2Handle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.MemoryQoSV2Feature),
			Config: config,
		},
		cgroupMgr: cgroupMgr,
		podLister: config.InformerFactory.K8SInformerFactory.Core().V1().Pods().Lister(),
		recorder:  config.GenericConfiguration.Recorder,
	}
}

// Handle processes PodEvents to apply memory QoS settings.
// It retrieves the pod, parses colocation config, and updates cgroup memory limits (High, Low, Min) for each container.
func (h *MemoryQoSV2Handle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		klog.Warningf("illegal pod event. type: %T", event)
		return nil
	}

	pod, err := h.podLister.Pods(podEvent.Pod.Namespace).Get(podEvent.Pod.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("pod does not existed, skipped handling memory qos", "pod", klog.KObj(podEvent.Pod))
			return nil
		}
		return err
	}
	if pod.UID != podEvent.UID {
		klog.InfoS("pod uid not match, skipped handling memory qos", "pod", klog.KObj(pod), "uid", pod.UID, "event uid", podEvent.UID)
		return nil
	}

	cfgStr := pod.Annotations[configv1alpha1.ColocationConfigKey]
	if cfgStr == "" {
		klog.V(5).InfoS("pod does not contains config, skipped handling memory qos", "pod", klog.KObj(pod))
		return nil
	}

	cfg, err := parseColocationConfig(cfgStr)
	if err != nil {
		h.recorder.Event(pod, v1.EventTypeWarning, "ColocationConfigParseFailed", err.Error())
		klog.ErrorS(err, "Failed to parse memory qos config", "pod", klog.KObj(pod))
		return nil
	}

	podCgroup, err := h.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupMemorySubsystem, podEvent.UID)
	if err != nil {
		klog.ErrorS(err, "Failed to get pod cgroup", "pod", klog.KObj(pod))
		return err
	}

	memorySubsystem := h.cgroupMgr.Memory()
	for _, container := range pod.Spec.Containers {
		containerID := podutils.FindContainerIDByName(pod, container.Name)
		cgroupName := h.cgroupMgr.BuildContainerCgroupName(containerID)
		containerCgroup := path.Join(podCgroup, cgroupName)

		memoryHigh, supported := memorySubsystem.High()
		if supported {
			quantity := container.Resources.Limits[corev1.ResourceMemory]
			value := func() int64 {
				if quantity.IsZero() || cfg.HighRatio >= 100 {
					return cgroup.MemoryUnlimited
				}
				return quantity.Value() * int64(cfg.HighRatio) / 100
			}()
			err = memoryHigh.Set(containerCgroup, value)
			if err != nil {
				klog.ErrorS(err, "Failed to set memory high", "pod", klog.KObj(pod), "container", container.Name)
				return err
			}
		}

		memoryLow, supported := memorySubsystem.Low()
		if supported {
			quantity := container.Resources.Requests[corev1.ResourceMemory]
			value := quantity.Value() * int64(cfg.LowRatio) / 100
			err = memoryLow.Set(containerCgroup, value)
			if err != nil {
				klog.ErrorS(err, "Failed to set memory low", "pod", klog.KObj(pod), "container", container.Name)
				return err
			}
		}

		memoryMin, supported := memorySubsystem.Min()
		if supported {
			quantity := container.Resources.Requests[corev1.ResourceMemory]
			value := quantity.Value() * int64(cfg.MinRatio) / 100
			err = memoryMin.Set(containerCgroup, value)
			if err != nil {
				klog.ErrorS(err, "Failed to set memory min", "pod", klog.KObj(pod), "container", container.Name)
				return err
			}
		}
	}

	klog.InfoS("Successfully set memory qos to cgroup file", "pod", klog.KObj(pod), "config", cfg)
	return nil
}

// parseColocationConfig decodes the colocation configuration string into a MemoryQos struct.
// If MemoryQos is missing in the config, set the default value to reset the pod's memory cgroup settings.
func parseColocationConfig(v string) (*configv1alpha1.MemoryQos, error) {
	var cfg configv1alpha1.Configuration
	if err := json.Unmarshal([]byte(v), &cfg); err != nil {
		return nil, err
	}
	if cfg.MemoryQos == nil { // TODO: adapt feature gate MemoryQoS: https://kubernetes.io/blog/2023/05/05/qos-memory-resources/
		cfg.MemoryQos = &configv1alpha1.MemoryQos{
			HighRatio: 100,
			LowRatio:  0,
			MinRatio:  0,
		}
	}

	return cfg.MemoryQos, nil
}
