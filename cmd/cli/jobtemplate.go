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
	"volcano.sh/volcano/pkg/cli/jobtemplate"
)

func buildJobTemplateCmd() *cobra.Command {
	jobTemplateCmd := &cobra.Command{
		Use:   "jobtemplate",
		Short: "vcctl command line operation jobtemplate",
	}

	jobTemplateCommandMap := map[string]struct {
		Short       string
		RunFunction func(cmd *cobra.Command, args []string)
		InitFlags   func(cmd *cobra.Command)
	}{
		"create": {
			Short: "create a jobtemplate",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobtemplate.CreateJobTemplate(cmd.Context()))
			},
			InitFlags: jobtemplate.InitCreateFlags,
		},
		"list": {
			Short: "list jobtemplates",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobtemplate.ListJobTemplate(cmd.Context()))
			},
			InitFlags: jobtemplate.InitListFlags,
		},
		"get": {
			Short: "get a jobtemplate",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobtemplate.GetJobTemplate(cmd.Context()))
			},
			InitFlags: jobtemplate.InitGetFlags,
		},
		"delete": {
			Short: "delete a jobtemplate",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobtemplate.DeleteJobTemplate(cmd.Context()))
			},
			InitFlags: jobtemplate.InitDeleteFlags,
		},
		"describe": {
			Short: "describe a jobtemplate",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, jobtemplate.DescribeJobTemplate(cmd.Context()))
			},
			InitFlags: jobtemplate.InitDescribeFlags,
		},
	}

	for command, config := range jobTemplateCommandMap {
		cmd := &cobra.Command{
			Use:   command,
			Short: config.Short,
			Run:   config.RunFunction,
		}
		config.InitFlags(cmd)
		jobTemplateCmd.AddCommand(cmd)
	}

	return jobTemplateCmd
}
