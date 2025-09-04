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
	"volcano.sh/volcano/pkg/cli/simulator"
)

func buildSimulatorCmd() *cobra.Command {
	simulatorCmd := &cobra.Command{
		Use:   "simu",
		Short: "vcctl command line operation simulator",
	}

	commands := []struct {
		Use         string
		Short       string
		Args        cobra.PositionalArgs
		RunFunction func(cmd *cobra.Command, args []string)
		InitFlags   func(cmd *cobra.Command)
	}{
		{
			Use:   "create-cluster [cluster-name]",
			Short: "create cluster for the scheduler simulator",
			Args:  cobra.ExactArgs(1),
			RunFunction: func(cmd *cobra.Command, args []string) {
				clusterName := args[0]
				util.CheckError(cmd, simulator.CreateCluster(clusterName, cmd.Context()))
			},
			InitFlags: simulator.InitCreateClusterFlags,
		},
		{
			Use:   "delete-cluster [cluster-name] [--config node-config-file]",
			Short: "delete cluster for the scheduler simulator",
			Args:  cobra.ExactArgs(1),
			RunFunction: func(cmd *cobra.Command, args []string) {
				clusterName := args[0]
				util.CheckError(cmd, simulator.DeleteCluster(clusterName, cmd.Context()))
			},
			InitFlags: simulator.InitDeleteClusterFlags,
		},
		{
			Use:   "run-tests [--config test-case-file]",
			Short: "run scheduling tests by the scheduler simulator",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, simulator.RunTests(cmd.Context()))
			},
			InitFlags: simulator.InitRunTestsFlags,
		},
		{
			Use:   "clean-tests",
			Short: "clean scheduling tests by the scheduler simulator",
			RunFunction: func(cmd *cobra.Command, args []string) {
				util.CheckError(cmd, simulator.CleanTests(cmd.Context()))
			},
			InitFlags: simulator.InitCleanTestsFlags,
		},
	}

	for _, command := range commands {
		cmd := &cobra.Command{
			Use:   command.Use,
			Short: command.Short,
			Args:  command.Args,
			Run:   command.RunFunction,
		}
		command.InitFlags(cmd)
		simulatorCmd.AddCommand(cmd)
	}

	return simulatorCmd
}
