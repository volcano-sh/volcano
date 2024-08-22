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
	"volcano.sh/volcano/pkg/cli/jobflow"
)

func buildJobFlowCmd() *cobra.Command {
	jobFlowCmd := &cobra.Command{
		Use:   "jobflow",
		Short: "vcctl command line operation jobflow",
	}

	jobFlowCommandMap := map[string]struct {
		Short       string
		RunFunction func(cmd *cobra.Command, args []string)
		InitFlags   func(cmd *cobra.Command)
	}{
		"create": {
			Short: "create a jobflow",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobflow.CreateJobFlow(cmd.Context()))
			},
			InitFlags: jobflow.InitCreateFlags,
		},
		"list": {
			Short: "list jobflows",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobflow.ListJobFlow(cmd.Context()))
			},
			InitFlags: jobflow.InitListFlags,
		},
		"get": {
			Short: "get a jobflow",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobflow.GetJobFlow(cmd.Context()))
			},
			InitFlags: jobflow.InitGetFlags,
		},
		"delete": {
			Short: "delete a jobflow",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobflow.DeleteJobFlow(cmd.Context()))
			},
			InitFlags: jobflow.InitDeleteFlags,
		},
		"describe": {
			Short: "describe a jobflow",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobflow.DescribeJobFlow(cmd.Context()))
			},
			InitFlags: jobflow.InitDescribeFlags,
		},
	}

	for command, config := range jobFlowCommandMap {
		cmd := &cobra.Command{
			Use:   command,
			Short: config.Short,
			Run:   config.RunFunction,
		}
		config.InitFlags(cmd)
		jobFlowCmd.AddCommand(cmd)
	}

	return jobFlowCmd
}
