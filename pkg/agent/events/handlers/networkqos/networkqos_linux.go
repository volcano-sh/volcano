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

package networkqos

import (
	"fmt"
	"os"
	"path"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/networkqos"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewNetworkQoSHandle)
}

type NetworkQoSHandle struct {
	*base.BaseHandle
	cgroupMgr     cgroup.CgroupManager
	networkqosMgr networkqos.NetworkQoSManager
	poLister      listersv1.PodLister
	recorder      record.EventRecorder
}

func NewNetworkQoSHandle(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &NetworkQoSHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.NetworkQoSFeature),
			Config: config,
		},
		cgroupMgr:     cgroupMgr,
		networkqosMgr: networkqos.GetNetworkQoSManager(config),
		poLister:      config.InformerFactory.K8SInformerFactory.Core().V1().Pods().Lister(),
		recorder:      config.GenericConfiguration.Recorder,
	}
}

func (h *NetworkQoSHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event: %v", event)
	}

	pod, err := h.poLister.Pods(podEvent.Pod.Namespace).Get(podEvent.Pod.Name)
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.V(4).InfoS("pod does not existed, skipped handling network qos", "namespace", podEvent.Pod.Namespace, "name", podEvent.Pod.Name)
			return nil
		}
		return err
	}

	_, ingressExisted := pod.Annotations["kubernetes.io/ingress-bandwidth"]
	_, egressExisted := pod.Annotations["kubernetes.io/egress-bandwidth"]
	if ingressExisted || egressExisted {
		h.recorder.Event(pod, corev1.EventTypeWarning, "NetworkQoSSkipped",
			fmt.Sprintf("Colocation Network QoS is not set, because it already has an Ingress-Bandwidth/Egress-Bandwidth"+
				" network rate limit(with annotation key kubernetes.io/ingress-bandwidth or kubernetes.io/egress-bandwidth )"))
		return nil
	}

	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupNetCLSSubsystem, podEvent.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podEvent.UID, err)
	}

	qosLevelFile := path.Join(cgroupPath, cgroup.NetCLSFileName)
	uintQoSLevel := uint32(extension.NormalizeQosLevel(podEvent.QoSLevel))
	qosLevel := []byte(strconv.FormatUint(uint64(uintQoSLevel), 10))

	err = utils.UpdatePodCgroup(qosLevelFile, qosLevel)
	if os.IsNotExist(err) {
		klog.InfoS("Cgroup file not existed", "cgroupFile", qosLevelFile)
		return nil
	}
	klog.InfoS("Successfully set network qos level to cgroup file", "qosLevel", string(qosLevel), "cgroupFile", qosLevelFile)
	return nil
}

func (h *NetworkQoSHandle) RefreshCfg(cfg *api.ColocationConfig) error {
	if err := h.BaseHandle.RefreshCfg(cfg); err != nil {
		return err
	}

	h.Lock.Lock()
	defer h.Lock.Unlock()
	if h.Active {
		err := h.networkqosMgr.EnableNetworkQoS(cfg.NetworkQosConfig)
		if err != nil {
			klog.ErrorS(err, "Failed to enable network qos")
			return err
		}
		klog.V(5).InfoS("Successfully enable/update network QoS")
		return nil
	}

	err := h.networkqosMgr.DisableNetworkQoS()
	if err != nil {
		klog.ErrorS(err, "Failed to disable network qos")
		return err
	}
	klog.V(5).InfoS("Successfully disable network QoS")
	return nil
}
