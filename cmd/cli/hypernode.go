/*
Copyright 2026 The Volcano Authors.

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

	"volcano.sh/volcano/pkg/cli/hypernode"
	"volcano.sh/volcano/pkg/cli/util"
)

func buildHyperNodeCmd() *cobra.Command {
	hyperNodeCmd := &cobra.Command{
		Use:   "hypernode",
		Short: "HyperNode operations",
	}
	options := &hypernode.TreeOptions{}
	treeCmd := &cobra.Command{
		Use:   "tree",
		Short: "Show HyperNode trees, member Nodes, status and resource totals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(cmd.Context(), cmd.OutOrStdout())
		},
	}
	util.InitFlags(treeCmd, &options.CommonFlags)
	treeCmd.Flags().StringVar(&options.Charset, "charset", "auto", "Tree characters: auto, ascii or unicode")
	treeCmd.Flags().BoolVar(&options.DRA, "dra", true, "Include node-local DRA device inventory from ResourceSlices")
	hyperNodeCmd.AddCommand(treeCmd)
	return hyperNodeCmd
}
