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
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	cilliumbpf "github.com/cilium/ebpf"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

type getCmd struct {
	out    io.Writer
	errOut io.Writer
}

func newGetCmd(out io.Writer, errOut io.Writer) *cobra.Command {
	get := &getCmd{
		out:    out,
		errOut: errOut,
	}

	return &cobra.Command{
		Use:   "show",
		Short: "Show online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth config values in ebpf map",
		Long:  `Show online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth config values in ebpf map`,
		Run: func(cmd *cobra.Command, args []string) {
			klog.InfoS("Network QoS command called", "command", "show")
			err := get.run()
			if err != nil {
				utils.Error(get.errOut, cmd, err)
			}
		},
	}
}

func (c *getCmd) run() (err error) {
	config, err := throttling.GetNetworkThrottlingConfig().GetThrottlingConfig()
	if err != nil {
		if errors.Is(err, cilliumbpf.ErrKeyNotExist) || os.IsNotExist(err) {
			return fmt.Errorf("failed to get throttling config: %v, network qos has not been initialized, please enable network qos first", err)
		}
		return fmt.Errorf("failed to get throttling config: %v", err)
	}

	configResult := &api.EbpfNetThrottlingConfGetResult{
		WaterLine: strconv.FormatUint(config.WaterLine*8, 10) + "bps",
		Interval:  config.Interval,
		LowRate:   strconv.FormatUint(config.LowRate*8, 10) + "bps",
		HighRate:  strconv.FormatUint(config.HighRate*8, 10) + "bps",
	}

	throttlingConfig, err := json.Marshal(configResult)
	if err != nil {
		return fmt.Errorf("failed to marshal throttling config %v to json: %v", config, err)
	}

	fmt.Fprintf(c.out, "%s: %s\n", "network-qos config", throttlingConfig)
	klog.InfoS("Network QoS command called successfully", "command", "show", "throttling-config", throttlingConfig)
	return nil
}
