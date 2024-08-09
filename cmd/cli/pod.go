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

package main

import (
	"github.com/spf13/cobra"

	"volcano.sh/volcano/cmd/cli/util"
	"volcano.sh/volcano/pkg/cli/pod"
)

func buildPodCmd() *cobra.Command {
	podCmd := &cobra.Command{
		Use:   "pod",
		Short: "vcctl command line operation pod",
	}

	podCommandMap := map[string]struct {
		Short       string
		RunFunction func(cmd *cobra.Command, args []string)
		InitFlags   func(cmd *cobra.Command)
	}{
		"list": {
			Short: "list pod information created by vcjob",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, pod.ListPods(cmd.Context()))
			},
			InitFlags: pod.InitListFlags,
		},
	}
	for command, config := range podCommandMap {
		cmd := &cobra.Command{
			Use:   command,
			Short: config.Short,
			Run:   config.RunFunction,
		}
		config.InitFlags(cmd)
		podCmd.AddCommand(cmd)
	}
	return podCmd
}
