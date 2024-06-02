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
			Short: "list a jobtemplate",
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
