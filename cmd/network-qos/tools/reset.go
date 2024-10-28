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

	"volcano.sh/volcano/pkg/networkqos/throttling"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

type resetCmd struct {
	out    io.Writer
	errOut io.Writer
}

func newResetCmd(out io.Writer, errOut io.Writer) *cobra.Command {
	reset := &resetCmd{
		out:    out,
		errOut: errOut,
	}

	return &cobra.Command{
		Use:   "reset",
		Short: "Reset online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth values",
		Long:  "Reset online-bandwidth-watermark/offline-low-bandwidth/offline-high-bandwidth values",
		Run: func(cmd *cobra.Command, args []string) {
			klog.InfoS("Network QoS command called", "command", "reset")
			err := reset.run()
			if err != nil {
				utils.Error(reset.errOut, cmd, err)
			}
		},
	}
}

func (c *resetCmd) run() (err error) {
	throttlingConf, err := throttling.GetNetworkThrottlingConfig().GetThrottlingConfig()
	if err != nil {
		if errors.Is(err, cilliumbpf.ErrKeyNotExist) || os.IsNotExist(err) {
			fmt.Fprintf(c.out, "throttling config does not exist, reset successfully")
			klog.InfoS("Network QoS command called successfully, throttling config does not exist")
			return nil
		}
		return fmt.Errorf("failed to get throttling config: %v", err)
	}

	throttlingConf, err = throttling.GetNetworkThrottlingConfig().CreateOrUpdateThrottlingConfig("", "1024Tibps", "1024Tibps",
		strconv.FormatUint(throttlingConf.Interval, 10))
	if err != nil {
		return fmt.Errorf("failed to update throttling config: %v", err)
	}

	throttlingConfigBytes, err := json.Marshal(throttlingConf)
	if err != nil {
		return fmt.Errorf("failed to marshal throttling conf: %v to json %v", throttlingConf, err)
	}

	fmt.Fprintf(c.out, "throttling config reset: %s\n", throttlingConfigBytes)
	klog.InfoS("Network QoS command called successfully", "command", "reset", "throttling-conf", throttlingConfigBytes)
	return nil
}
