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
