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
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/exec"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

const (
	// defaultNetworkDevice is the default network interface managed by bwmcli in cgroup v2 mode.
	defaultNetworkDevice = "eth0"

	// nsenterPrefix is the command prefix to execute commands in the host's mount namespace.
	nsenterPrefix = "nsenter --target 1 --mount --"
)

type NetworkQoSManager interface {
	Init() error
	HealthCheck() error
	EnableNetworkQoS(qosConf *api.NetworkQos) error
	DisableNetworkQoS() error
}

var networkQoSManager NetworkQoSManager

type NetworkQoSManagerImp struct {
	config             *config.Configuration
	flavorQuotaMinRate int64
	// cgroupVersion indicates the cgroup version (v1 or v2) detected on the node.
	cgroupVersion string
}

func NewNetworkQoSManager(config *config.Configuration, cgroupVersion string) NetworkQoSManager {
	networkQoSManager = &NetworkQoSManagerImp{
		config:        config,
		cgroupVersion: cgroupVersion,
	}
	return networkQoSManager
}

func GetNetworkQoSManager(config *config.Configuration, cgroupVersion string) NetworkQoSManager {
	if networkQoSManager == nil {
		return NewNetworkQoSManager(config, cgroupVersion)
	}
	return networkQoSManager
}

// Init initializes the network QoS subsystem based on cgroup version.
// For cgroup v1: installs bwm_tc.o and network-qos CNI plugin.
// For cgroup v2: checks bwmcli availability on the host.
func (m *NetworkQoSManagerImp) Init() error {
	switch m.cgroupVersion {
	case cgroup.CgroupV2:
		return m.initV2()
	default:
		return InstallNetworkQoS()
	}
}

// initV2 checks that bwmcli (oncn-bwm) is available on the host via nsenter.
func (m *NetworkQoSManagerImp) initV2() error {
	klog.InfoS("Initializing network QoS for cgroup v2 mode (oncn-bwm)")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := nsenterPrefix + " which bwmcli"
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("bwmcli not found on host, network QoS v2 requires oncn-bwm package: %v, output: %s", err, output)
	}
	klog.InfoS("bwmcli is available on host", "path", strings.TrimSpace(output))
	return nil
}

// HealthCheck checks the health of network QoS subsystem based on cgroup version.
// For cgroup v1: checks bwm_tc.o and network-qos binary existence.
// For cgroup v2: queries bwmcli device status.
func (m *NetworkQoSManagerImp) HealthCheck() error {
	switch m.cgroupVersion {
	case cgroup.CgroupV2:
		return m.healthCheckV2()
	default:
		return CheckNetworkQoSStatus()
	}
}

// healthCheckV2 checks bwmcli status by querying enabled devices.
func (m *NetworkQoSManagerImp) healthCheckV2() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := nsenterPrefix + " bwmcli -p devs"
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("bwmcli health check failed: %v, output: %s", err, output)
	}
	klog.V(5).InfoS("bwmcli health check passed", "output", strings.TrimSpace(output))
	return nil
}

// EnableNetworkQoS enables network QoS based on cgroup version.
// For cgroup v1: calls network-qos prepare to write eBPF map and register CNI plugin.
// For cgroup v2: calls bwmcli to set bandwidth, waterline, and enable network device.
func (m *NetworkQoSManagerImp) EnableNetworkQoS(qosConf *api.NetworkQos) error {
	switch m.cgroupVersion {
	case cgroup.CgroupV2:
		return m.enableNetworkQoSV2(qosConf)
	default:
		return m.enableNetworkQoSV1(qosConf)
	}
}

// enableNetworkQoSV1 is the original cgroup v1 implementation that uses network-qos prepare command
// to write eBPF throttling config map and register CNI plugin.
func (m *NetworkQoSManagerImp) enableNetworkQoSV1(qosConf *api.NetworkQos) error {
	onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, err := m.GetBandwidthConfigs(qosConf)
	if err != nil {
		return fmt.Errorf("failed to get bandwidth configs: %v", err)
	}

	var checkInterval string
	if qosConf != nil && qosConf.QoSCheckInterval != nil {
		checkInterval = strconv.Itoa(*qosConf.QoSCheckInterval)
	}

	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := fmt.Sprintf(utils.NetWorkCmdFile+" prepare --%s=%s --%s=%s --%s=%s --%s=%s --%s=%s",
		utils.EnableNetworkQoS, "true",
		utils.OnlineBandwidthWatermarkKey, onlineBandwidthWatermark,
		utils.OfflineLowBandwidthKey, offlineLowBandwidth,
		utils.OfflineHighBandwidthKey, offlineHighBandwidth,
		utils.NetWorkQoSCheckInterval, checkInterval)
	output, err := exec.GetExecutor().CommandContext(cmdCtx, cmd)
	if err != nil {
		return fmt.Errorf("failed to set network qos:%v, output:%s", err, output)
	}
	return nil
}

// enableNetworkQoSV2 uses bwmcli (oncn-bwm) to configure network QoS on cgroup v2 systems.
// It sets offline bandwidth range, online bandwidth waterline, and enables the network device.
func (m *NetworkQoSManagerImp) enableNetworkQoSV2(qosConf *api.NetworkQos) error {
	onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, err := m.GetBandwidthConfigs(qosConf)
	if err != nil {
		return fmt.Errorf("failed to get bandwidth configs: %v", err)
	}

	// Convert bandwidth format from "50Mbps" to "50mb" for bwmcli
	watermark := convertBandwidthFormat(onlineBandwidthWatermark)
	lowBw := convertBandwidthFormat(offlineLowBandwidth)
	highBw := convertBandwidthFormat(offlineHighBandwidth)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Step 1: Set offline bandwidth range (lowBandwidth, highBandwidth)
	cmd := fmt.Sprintf("%s bwmcli -s bandwidth %s,%s", nsenterPrefix, lowBw, highBw)
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to set bandwidth via bwmcli: %v, output: %s", err, output)
	}
	klog.InfoS("Successfully set bandwidth via bwmcli", "low", lowBw, "high", highBw)

	// Step 2: Set online bandwidth waterline
	cmd = fmt.Sprintf("%s bwmcli -s waterline %s", nsenterPrefix, watermark)
	output, err = exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to set waterline via bwmcli: %v, output: %s", err, output)
	}
	klog.InfoS("Successfully set waterline via bwmcli", "waterline", watermark)

	// Step 3: Enable network device
	cmd = fmt.Sprintf("%s bwmcli -e %s", nsenterPrefix, defaultNetworkDevice)
	output, err = exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to enable network device via bwmcli: %v, output: %s", err, output)
	}
	klog.InfoS("Successfully enabled network device via bwmcli", "device", defaultNetworkDevice)

	return nil
}

// DisableNetworkQoS disables network QoS based on cgroup version.
// For cgroup v1: calls network-qos prepare --enable-network-qos=false.
// For cgroup v2: calls bwmcli -d to disable the network device.
func (m *NetworkQoSManagerImp) DisableNetworkQoS() error {
	switch m.cgroupVersion {
	case cgroup.CgroupV2:
		return m.disableNetworkQoSV2()
	default:
		return m.disableNetworkQoSV1()
	}
}

// disableNetworkQoSV1 is the original cgroup v1 implementation.
func (m *NetworkQoSManagerImp) disableNetworkQoSV1() error {
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := fmt.Sprintf(utils.NetWorkCmdFile+" prepare --%s=%s", utils.EnableNetworkQoS, "false")
	output, err := exec.GetExecutor().CommandContext(cmdCtx, cmd)
	if err != nil {
		return fmt.Errorf("failed to reset network qos:%v, output:%s", err, output)
	}
	return nil
}

// disableNetworkQoSV2 disables oncn-bwm network QoS by disabling the network device.
func (m *NetworkQoSManagerImp) disableNetworkQoSV2() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := fmt.Sprintf("%s bwmcli -d %s", nsenterPrefix, defaultNetworkDevice)
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to disable network qos via bwmcli: %v, output: %s", err, output)
	}
	klog.InfoS("Successfully disabled network QoS via bwmcli", "device", defaultNetworkDevice)
	return nil
}

// convertBandwidthFormat converts bandwidth string from Volcano format ("50Mbps") to bwmcli format ("50mb").
func convertBandwidthFormat(bandwidth string) string {
	return strings.TrimSuffix(bandwidth, "Mbps") + "mb"
}

func (m *NetworkQoSManagerImp) GetBandwidthConfigs(qosConf *api.NetworkQos) (onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth string, err error) {
	if m.flavorQuotaMinRate == 0 {
		nodeName := m.config.GenericConfiguration.KubeNodeName
		getCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		node, err := m.config.GenericConfiguration.KubeClient.CoreV1().Nodes().Get(getCtx, nodeName, metav1.GetOptions{})
		if err != nil {
			return "", "", "0", fmt.Errorf("failed to get k8s node(%s): %v", nodeName, err)
		}
		m.flavorQuotaMinRate, err = GetFlavorQuotaMinRate(node)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to get flavor quota min rate, err: %v", err)
		}
	}

	onlineBandwidthWatermark, err = GetOnlineBandwidthWatermark(m.flavorQuotaMinRate, qosConf)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get onlineBandwidthWatermark, err: %v", err)
	}

	offlineLowBandwidth, err = GetOfflineLowBandwidthPercent(m.flavorQuotaMinRate, qosConf)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get offlineLowBandwidth, err: %v", err)
	}

	offlineHighBandwidth, err = GetOfflineHighBandwidthPercent(m.flavorQuotaMinRate, qosConf)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get offlineHighBandwidth, err: %v", err)
	}

	return onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, nil
}

func GetFlavorQuotaMinRate(node *corev1.Node) (int64, error) {
	minRate, ok := node.Annotations[apis.NetworkBandwidthRateAnnotationKey]
	if !ok {
		return 0, fmt.Errorf("node %s network bandwidth rate not exists, annotation %s", node.Name, apis.NetworkBandwidthRateAnnotationKey)
	}

	minRateInt, err := strconv.ParseInt(minRate, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to cover net rate spec(%s) to int, err: %v", minRate, err)
	}
	return minRateInt, nil
}

func GetOnlineBandwidthWatermark(serverRateQuota int64, qosConf *api.NetworkQos) (string, error) {
	var onlineBandwidthWatermarkPercent int
	if qosConf != nil && qosConf.OnlineBandwidthWatermarkPercent != nil {
		onlineBandwidthWatermarkPercent = *qosConf.OnlineBandwidthWatermarkPercent
	} else {
		return "", fmt.Errorf("illegal network config, parameter OnlineBandwidthWatermarkPercent missing")
	}

	return strconv.FormatInt(serverRateQuota*int64(onlineBandwidthWatermarkPercent)/100, 10) + "Mbps", nil
}

func GetOfflineLowBandwidthPercent(serverRateQuota int64, qosConf *api.NetworkQos) (string, error) {
	var offlineLowBandwidthPercent int
	if qosConf != nil && qosConf.OfflineLowBandwidthPercent != nil {
		offlineLowBandwidthPercent = *qosConf.OfflineLowBandwidthPercent
	} else {
		return "", fmt.Errorf("illegal network config, parameter OfflineLowBandwidthPercent missing")
	}

	return strconv.FormatInt(serverRateQuota*int64(offlineLowBandwidthPercent)/100, 10) + "Mbps", nil
}

func GetOfflineHighBandwidthPercent(serverRateQuota int64, qosConf *api.NetworkQos) (string, error) {
	var offlineHighBandwidthPercent int
	if qosConf != nil && qosConf.OfflineHighBandwidthPercent != nil {
		offlineHighBandwidthPercent = *qosConf.OfflineHighBandwidthPercent
	} else {
		return "", fmt.Errorf("illegal network config, parameter OfflineHighBandwidthPercent missing")
	}

	return strconv.FormatInt(serverRateQuota*int64(offlineHighBandwidthPercent)/100, 10) + "Mbps", nil
}

// InstallNetworkQoS installs the network QoS components for cgroup v1 mode.
// It copies bwm_tc.o eBPF program and network-qos CNI plugin to their expected paths.
func InstallNetworkQoS() error {
	checkErr := features.CheckNodeSupportNetworkQoS()
	if checkErr != nil {
		if features.IsUnsupportedError(checkErr) {
			klog.InfoS("Skip installing network-qos, os/network-mode not supported")
			return nil
		}
		return checkErr
	}

	klog.InfoS("Start to install network-qos")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := "sudo /bin/cp -f /usr/local/bin/bwm_tc.o /usr/share/bwmcli"
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		klog.ErrorS(err, "Failed to install bwm_tc.o to /usr/share/bwmcli", "output", output)
		return err
	}

	cmd = "sudo /bin/cp -f /usr/local/bin/network-qos /opt/cni/bin"
	output, err = exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		klog.ErrorS(err, "Failed to install network-qos to /opt/cni/bin; %s", "output", output)
		return err
	}
	klog.InfoS("Successfully installed network-qos")
	return nil
}

// CheckNetworkQoSStatus checks if the cgroup v1 network QoS components are in place.
func CheckNetworkQoSStatus() error {
	_, err := os.Stat(utils.TCPROGPath)
	if err != nil {
		klog.ErrorS(err, "Failed to check the existence of bwm_tc.o", "path", utils.TCPROGPath)
		return err
	}

	_, err = os.Stat(utils.NetworkQoSPath)
	if err != nil {
		klog.ErrorS(err, "Failed to check the existence of network-qos", "path", utils.NetworkQoSPath)
		return err
	}
	return nil
}
