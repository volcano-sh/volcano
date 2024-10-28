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

package utils

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

const (
	Kbps = 1000
	Mbps = 1000 * 1000
	Gbps = 1000 * 1000 * 1000
	Tbps = 1000 * 1000 * 1000 * 1000

	Kibps = 1024
	Mibps = 1024 * 1024
	Gibps = 1024 * 1024 * 1024
	Tibps = 1024 * 1024 * 1024 * 1024
)

const (
	TCPROGPath         = "/usr/share/bwmcli/bwm_tc.o"
	CNILogFilePath     = "/var/log/volcano/agent/network-qos.log"
	ToolCmdLogFilePath = "/var/log/volcano/agent/network-qos-tools.log"
	NetWorkCmdFile     = "/usr/local/bin/network-qos"
	DefaultCNIConfFile = "/etc/cni/net.d/cni.conflist"
)

const (
	OnlineBandwidthWatermarkKey = "online-bandwidth-watermark"
	OfflineLowBandwidthKey      = "offline-low-bandwidth"
	OfflineHighBandwidthKey     = "offline-high-bandwidth"
	NetWorkQoSCheckInterval     = "check-interval"
	NodeColocationEnable        = "colocation"
	EnableNetworkQoS            = "enable-network-qos"
	CNIPluginName               = "network-qos"
)

const (
	OpenEulerOSReleaseName    = "openEuler"
	OpenEulerOSReleaseVersion = "22.03 (LTS-SP2)"

	// HostOSReleasePathEnv presents the key for env of host os release file
	HostOSReleasePathEnv = "HOST_OS_RELEASE"

	DefaultNodeOSReleasePath = "/host/etc/os-release"
	NetworkQoSPath           = "/opt/cni/bin/network-qos"
)

const (
	DefaultInterval = "10000000" // 1000000 纳秒 = 10 毫秒
)

const (
	// CNIConfFilePathEnv presents the key for env of cni con file
	CNIConfFilePathEnv = "CNI_CONF_FILE_PATH"
)

func InitLog(logPath string) error {
	klog.InitFlags(nil)
	if err := flag.Set("log_file", logPath); err != nil {
		return fmt.Errorf("failed fo set flag(log_file): %v\n", err)
	}
	if err := flag.Set("logtostderr", "false"); err != nil {
		return fmt.Errorf("failed fo set flag(logtostderr): %v\n", err)
	}
	if err := flag.Set("alsologtostderr", "false"); err != nil {
		return fmt.Errorf("failed fo set flag(alsologtostderr): %v\n", err)
	}
	if err := flag.Set("skip_log_headers", "true"); err != nil {
		return fmt.Errorf("failed fo set flag(skip_log_headers): %v\n", err)
	}
	if err := flag.Set("stderrthreshold", "3"); err != nil {
		return fmt.Errorf("failed fo set flag(stderrthreshold): %v\n", err)
	}
	if err := flag.Set("v", "4"); err != nil {
		return fmt.Errorf("failed fo set flag(v): %v\n", err)
	}
	flag.Parse()
	return nil
}

func SizeStrConvertToByteSize(sizeStr string) (uint64, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	index := strings.IndexFunc(sizeStr, unicode.IsLetter)
	if index == -1 {
		index = len(sizeStr)
	}

	unit := sizeStr[index:]
	num := sizeStr[:index]
	numInt, err := strconv.ParseUint(num, 10, 0)
	if err != nil {
		return 0, fmt.Errorf("size string %s is illegal, only supprt positive integer with unit like Kbps, Mbps, Gbps, Tbps: %v", sizeStr, err)
	}

	switch unit {
	case "Kbps", "":
		return numInt * Kbps / 8, nil
	case "Mbps":
		return numInt * Mbps / 8, nil
	case "Gbps":
		return numInt * Gbps / 8, nil
	case "Tbps":
		return numInt * Tbps / 8, nil
	case "Kibps":
		return numInt * Kibps / 8, nil
	case "Mibps":
		return numInt * Mibps / 8, nil
	case "Gibps":
		return numInt * Gibps / 8, nil
	case "Tibps":
		return numInt * Tibps / 8, nil
	default:
		return 0, fmt.Errorf("size %s is illegal, only supprt positive integer with unit like Kbps, Mbps, Gbps, Tbps", sizeStr)
	}
}

func Error(errOut io.Writer, cmd *cobra.Command, err error) {
	fmt.Fprintf(errOut, "execute command[%s] failed, error:%v\n", cmd.Name(), err)
	klog.ErrorS(err, "Network QoS command called failed", "command", cmd.Name())
	klog.Flush()
	os.Exit(1)
}
