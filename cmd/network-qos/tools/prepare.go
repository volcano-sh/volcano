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

package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/utils/exec"
	"volcano.sh/volcano/pkg/networkqos/cni"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

type prepareCmd struct {
	out    io.Writer
	errOut io.Writer
}

func newPrepareCmd(out io.Writer, errOut io.Writer) *cobra.Command {
	prepare := &prepareCmd{
		out:    out,
		errOut: errOut,
	}

	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "prepare online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth values",
		Long:  "prepare online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth values",
		Run: func(cmd *cobra.Command, args []string) {
			klog.InfoS("Network QoS command called", "command", "prepare")
			err := prepare.run()
			if err != nil {
				utils.Error(prepare.errOut, cmd, err)
				return
			}
		},
	}
	(&opt).AddFlags(cmd)
	return cmd
}

func (c *prepareCmd) run() (err error) {
	enableNetworkQoS := opt.EnableNetworkQoS
	onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, interval := opt.OnlineBandwidthWatermark,
		opt.OfflineLowBandwidth, opt.OfflineHighBandwidth, opt.CheckoutInterval

	if !enableNetworkQoS {
		return c.unInstall()
	}

	return c.install(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, interval)
}

func (c *prepareCmd) unInstall() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := fmt.Sprintf(utils.NetWorkCmdFile + " reset")
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to reset network qos:%v, output:%s", err, output)
	}

	cniConfFile := strings.TrimSpace(os.Getenv(utils.CNIConfFilePathEnv))
	if cniConfFile == "" {
		cniConfFile = utils.DefaultCNIConfFile
	}
	err = cni.GetCNIPluginConfHandler().DeleteCniPluginFromConfList(cniConfFile, utils.CNIPluginName)
	if err != nil {
		return fmt.Errorf("failed to reset cni config: %v", err)
	}
	klog.InfoS("Network QoS command called successfully", "command", "prepare[uninstall]")
	return nil
}

func (c *prepareCmd) install(onlineBandwidthWatermark, offlineLowBandwidth, offlineHighBandwidth, interval string) error {
	if len(interval) == 0 {
		interval = utils.DefaultInterval
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := fmt.Sprintf(utils.NetWorkCmdFile+" set --%s=%s --%s=%s --%s=%s --%s=%s", utils.OnlineBandwidthWatermarkKey, onlineBandwidthWatermark,
		utils.OfflineLowBandwidthKey, offlineLowBandwidth,
		utils.OfflineHighBandwidthKey, offlineHighBandwidth,
		utils.NetWorkQoSCheckInterval, interval)
	output, err := exec.GetExecutor().CommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to set network qos:%v, output:%s", err, output)
	}

	cniConf := make(map[string]interface{})
	cniConf["name"] = utils.CNIPluginName
	cniConf["type"] = utils.CNIPluginName
	cniConf["args"] = map[string]string{
		utils.NodeColocationEnable:        "true",
		utils.NetWorkQoSCheckInterval:     interval,
		utils.OnlineBandwidthWatermarkKey: onlineBandwidthWatermark,
		utils.OfflineLowBandwidthKey:      offlineLowBandwidth,
		utils.OfflineHighBandwidthKey:     offlineHighBandwidth,
	}

	cniConfFile := strings.TrimSpace(os.Getenv(utils.CNIConfFilePathEnv))
	if cniConfFile == "" {
		cniConfFile = utils.DefaultCNIConfFile
	}
	err = cni.GetCNIPluginConfHandler().AddOrUpdateCniPluginToConfList(cniConfFile, utils.CNIPluginName, cniConf)
	if err != nil {
		return fmt.Errorf("failed to add/update cni plugin to configlist: %v", err)
	}
	klog.InfoS("Network QoS command called successfully", "command", "prepare[install]", "cni-conf", cniConf)
	return nil
}
