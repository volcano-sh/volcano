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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/network-qos/tools/options"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

var opt options.Options

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := utils.InitLog(utils.ToolCmdLogFilePath)
	if err != nil {
		fmt.Printf("failed fo init log: %v\n", err)
	}

	err = NewRootCmd().Execute()
	if err != nil {
		klog.Flush()
		fmt.Fprintf(os.Stderr, "execute command failed, error:%v\n", err)
		os.Exit(1)
	}

	klog.Flush()
	os.Exit(0)
}

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network-qos",
		Short: "Network QoS",
		Long:  "Network QoS",
	}

	cmd.AddCommand(newSetCmd(os.Stdout, os.Stderr))
	cmd.AddCommand(newGetCmd(os.Stdout, os.Stderr))
	cmd.AddCommand(newResetCmd(os.Stdout, os.Stderr))
	cmd.AddCommand(newStatusCmd(os.Stdout, os.Stderr))
	cmd.AddCommand(newPrepareCmd(os.Stdout, os.Stderr))
	cmd.AddCommand(newVersionCmd(os.Stdout, os.Stderr))
	return cmd
}
