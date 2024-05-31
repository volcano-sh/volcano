/*
Copyright 2019 The Volcano Authors.

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
	"volcano.sh/volcano/pkg/cli/queue"
)

func buildQueueCmd() *cobra.Command {
	queueCmd := &cobra.Command{
		Use:   "queue",
		Short: "Queue Operations",
	}

	commands := []struct {
		Use         string
		Short       string
		RunFunction func(cmd *cobra.Command, args []string)
		InitFlags   func(cmd *cobra.Command)
	}{
		{
			Use:   "create",
			Short: "creates queue",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, queue.CreateQueue(cmd.Context()))
			},
			InitFlags: queue.InitCreateFlags,
		},
		{
			Use:   "delete",
			Short: "delete queue",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, queue.DeleteQueue(cmd.Context()))
			},
			InitFlags: queue.InitDeleteFlags,
		},
		{
			Use:   "operate",
			Short: "operate queue",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, queue.OperateQueue(cmd.Context()))
			},
			InitFlags: queue.InitOperateFlags,
		},
		{
			Use:   "list",
			Short: "lists all the queue",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, queue.ListQueue(cmd.Context()))
			},
			InitFlags: queue.InitListFlags,
		},
		{
			Use:   "get",
			Short: "get a queue",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, queue.GetQueue(cmd.Context()))
			},
			InitFlags: queue.InitGetFlags,
		},
	}

	for _, command := range commands {
		cmd := &cobra.Command{
			Use:   command.Use,
			Short: command.Short,
			Run:   command.RunFunction,
		}
		command.InitFlags(cmd)
		queueCmd.AddCommand(cmd)
	}

	return queueCmd
}
