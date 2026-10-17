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
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

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
	"volcano.sh/volcano/pkg/agent/utils/exec"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/networkqos"
)

const (
	// bwmcliCmdTimeout is the timeout for bwmcli command execution.
	bwmcliCmdTimeout = 5 * time.Second
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
		networkqosMgr: networkqos.GetNetworkQoSManager(config, cgroupMgr.GetCgroupVersion()),
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
		if errors.IsNotFound(err) {
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

	cgroupVersion := h.cgroupMgr.GetCgroupVersion()
	switch cgroupVersion {
	case cgroup.CgroupV2:
		klog.V(2).InfoS("Detected cgroup version v2 for network qos handling. Network QoS will use oncn-bwm mode.")
		return h.handleV2(podEvent)
	default:
		klog.V(2).InfoS("Detected cgroup version v1 for network qos handling. Falling back to legacy net_cls mode.")
		return h.handleV1(podEvent)
	}
}

// handleV1 sets the network QoS level for a pod using cgroup v1 net_cls.classid file.
func (h *NetworkQoSHandle) handleV1(podEvent framework.PodEvent) error {
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

// handleV2 sets the network QoS level for a pod using bwmcli command with cgroup v2 unified path.
// In cgroup v2, the net_cls controller is not available. Instead, we use bwmcli (oncn-bwm) to set
// the network priority directly via the cgroup path:
//
//	bwmcli -s <cgroup-path> <priority>
//
// where priority is 0 for online pods and -1 for offline pods.
//
// Since bwmcli is installed on the host (not in the agent container), we use chroot /proc/1/root
// to execute the command on the host filesystem. The cgroup path from CgroupManager may contain a
// "/host" prefix (because the agent container mounts the host filesystem at /host), which must
// be stripped when running in the host namespace.
func (h *NetworkQoSHandle) handleV2(podEvent framework.PodEvent) error {
	// In cgroup v2, all controllers share a unified hierarchy, so we can use any subsystem
	// (e.g., cpu) to get the correct unified cgroup path.
	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cgroup.CgroupCpuSubsystem, podEvent.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path(%s), error: %v", podEvent.UID, err)
	}

	// Strip the "/host" prefix from the cgroup path if present, because bwmcli runs on the host
	// via chroot /proc/1/root and sees the native host filesystem paths (e.g., /sys/fs/cgroup/...).
	hostCgroupPath := strings.TrimPrefix(cgroupPath, "/host")

	qosLevel := extension.NormalizeQosLevel(podEvent.QoSLevel)

	cmdCtx, cancel := context.WithTimeout(context.Background(), bwmcliCmdTimeout)
	defer cancel()
	// Use chroot /proc/1/root to execute bwmcli on the host filesystem.
	// bwmcli is a host-installed tool (oncn-bwm package) and is not available inside the agent container.
	// Running via chroot ensures bwmcli and all its shared library dependencies (e.g., libbpf.so.1)
	// are loaded from the host's native filesystem. Requires hostPID: true and privileged: true.
	cmd := fmt.Sprintf("chroot /proc/1/root bwmcli -s %s %d", hostCgroupPath, qosLevel)
	output, err := exec.GetExecutor().CommandContext(cmdCtx, cmd)
	if err != nil {
		return fmt.Errorf("failed to set network qos via bwmcli, path=%s, level=%d, error: %v, output: %s",
			hostCgroupPath, qosLevel, err, output)
	}

	klog.InfoS("Successfully set network qos level via bwmcli", "qosLevel", qosLevel, "cgroupPath", hostCgroupPath)
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
