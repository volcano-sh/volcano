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
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/network-qos/tools/options"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

type setCmd struct {
	out    io.Writer
	errOut io.Writer
}

func newSetCmd(out io.Writer, errOut io.Writer) *cobra.Command {
	set := &setCmd{
		out:    out,
		errOut: errOut,
	}

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Add/Update online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth/check-interval values",
		Long:  "Add/Update online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth/check-interval values",
		Run: func(cmd *cobra.Command, args []string) {
			klog.InfoS("Network QoS command called", "command", "set")
			err := set.run(opt)
			if err != nil {
				utils.Error(set.errOut, cmd, err)
			}
		},
	}
	(&opt).AddFlags(cmd)
	return cmd
}

func (c *setCmd) run(opt options.Options) (err error) {
	throttlingConf, err := throttling.GetNetworkThrottlingConfig().CreateOrUpdateThrottlingConfig(opt.OnlineBandwidthWatermark,
		opt.OfflineLowBandwidth, opt.OfflineHighBandwidth, opt.CheckoutInterval)
	if err != nil {
		return fmt.Errorf("failed to create/update throttling config: %v", err)
	}

	throttlingConfigBytes, err := json.Marshal(throttlingConf)
	if err != nil {
		return fmt.Errorf("failed to create/update throttling config: %v", err)
	}

	fmt.Fprintf(c.out, "throttling config set: %s\n", throttlingConfigBytes)
	klog.InfoS("Network QoS command called successfully", "command", "set", "throttling-conf", throttlingConfigBytes)
	return nil
}
