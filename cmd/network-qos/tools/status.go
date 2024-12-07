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

	cilliumbpf "github.com/cilium/ebpf"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/networkqos/throttling"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

type statusCmd struct {
	out    io.Writer
	errOut io.Writer
}

func newStatusCmd(out io.Writer, errOut io.Writer) *cobra.Command {
	status := &statusCmd{
		out:    out,
		errOut: errOut,
	}

	return &cobra.Command{
		Use:   "status",
		Short: "Print network QoS status",
		Long:  "Print network QoS status",
		Run: func(cmd *cobra.Command, args []string) {
			klog.InfoS("Network QoS command called", "command", "status")
			err := status.run()
			if err != nil {
				utils.Error(status.errOut, cmd, err)
			}
		},
	}
}

func (c *statusCmd) run() (err error) {
	throttlingStatus, err := throttling.GetNetworkThrottlingConfig().GetThrottlingStatus()
	if err != nil {
		if errors.Is(err, cilliumbpf.ErrKeyNotExist) || os.IsNotExist(err) {
			return fmt.Errorf("failed to get throttling status: %v, network qos has not been initialized, please enable network qos first", err)
		}
		return fmt.Errorf("failed to get throttling status: %v", err)
	}

	throttlingStatusBytes, err := json.Marshal(throttlingStatus)
	if err != nil {
		return fmt.Errorf("failed to marshal throttling status %v to json: %v", throttlingStatus, err)
	}

	fmt.Fprintf(c.out, "%s: %s\n", "throttling status", throttlingStatusBytes)
	klog.InfoS("Network QoS command called successfully", "command", "status", "status", throttlingStatusBytes)
	return nil
}
